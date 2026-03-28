from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path

import chromadb
import httpx
from chromadb.errors import ChromaError, NotFoundError
from mcp.server.fastmcp import FastMCP

from lib.embedding_payload import parse_query_embedding_payload

DEFAULT_CHROMA_DIR = "index"
DEFAULT_OLLAMA_HOST = "http://127.0.0.1:11434"
DEFAULT_EMBED_MODEL = "nomic-embed-text"
DEFAULT_COLLECTION_NAME = "docs"
DEFAULT_MAX_TOP_K = 50
INDEX_METADATA_FILENAME = ".rag_index_meta.json"

mcp = FastMCP("local_rag", json_response=True)

_collection_cache_key: tuple[str, str] | None = None
_collection_cache: object | None = None
_source_cache: dict[tuple[str, str, float | None], list[str]] = {}


@dataclass(frozen=True)
class ServerSettings:
    chroma_dir: Path
    ollama_host: str
    embed_model: str
    collection_name: str
    max_top_k: int


def get_settings() -> ServerSettings:
    return ServerSettings(
        chroma_dir=Path(os.environ.get("RAG_CHROMA_DIR", DEFAULT_CHROMA_DIR))
        .expanduser()
        .resolve(),
        ollama_host=os.environ.get("OLLAMA_HOST", DEFAULT_OLLAMA_HOST).rstrip("/"),
        embed_model=os.environ.get("EMBED_MODEL", DEFAULT_EMBED_MODEL),
        collection_name=os.environ.get("COLLECTION_NAME", DEFAULT_COLLECTION_NAME),
        max_top_k=max(
            1,
            int(os.environ.get("RAG_MAX_TOP_K", str(DEFAULT_MAX_TOP_K))),
        ),
    )


class QueryEmbeddingError(RuntimeError):
    """Raised when query embeddings cannot be created or parsed."""


def load_index_metadata(settings: ServerSettings) -> dict[str, object]:
    metadata_path = settings.chroma_dir / INDEX_METADATA_FILENAME
    if not metadata_path.exists():
        return {}

    try:
        content = metadata_path.read_text(encoding="utf-8")
        data = json.loads(content)
        return data if isinstance(data, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def get_index_embed_model(settings: ServerSettings) -> str:
    metadata = load_index_metadata(settings)
    model = metadata.get("embed_model")
    if isinstance(model, str) and model.strip():
        return model.strip()
    return settings.embed_model


def get_collection():
    global _collection_cache, _collection_cache_key

    settings = get_settings()
    cache_key = (str(settings.chroma_dir), settings.collection_name)
    if _collection_cache_key == cache_key:
        return _collection_cache

    client = chromadb.PersistentClient(path=str(settings.chroma_dir))
    try:
        collection = client.get_collection(name=settings.collection_name)
        _collection_cache_key = cache_key
        _collection_cache = collection
        return collection
    except (NotFoundError, ChromaError, OSError, ValueError):
        _collection_cache_key = cache_key
        _collection_cache = None
        return None


def parse_embedding_payload(payload: object) -> list[float]:
    try:
        return parse_query_embedding_payload(payload)
    except ValueError as exc:
        raise QueryEmbeddingError(str(exc)) from exc


def embed_query(text: str, embed_model: str, settings: ServerSettings) -> list[float]:
    try:
        with httpx.Client(timeout=120.0) as client:
            response = client.post(
                f"{settings.ollama_host}/api/embed",
                json={"model": embed_model, "input": text},
            )
            response.raise_for_status()
            data = response.json()
    except httpx.TimeoutException as exc:
        raise QueryEmbeddingError(
            "Timed out while requesting Ollama embeddings."
        ) from exc
    except httpx.HTTPStatusError as exc:
        raise QueryEmbeddingError(
            f"Ollama returned HTTP {exc.response.status_code} for embeddings."
        ) from exc
    except httpx.RequestError as exc:
        raise QueryEmbeddingError(
            f"Failed to reach Ollama at '{settings.ollama_host}'."
        ) from exc
    except ValueError as exc:
        raise QueryEmbeddingError(
            "Ollama returned invalid JSON for embeddings."
        ) from exc

    return parse_embedding_payload(data)


def _get_source_paths(collection, settings: ServerSettings) -> list[str]:
    metadata_path = settings.chroma_dir / INDEX_METADATA_FILENAME
    metadata_mtime = metadata_path.stat().st_mtime if metadata_path.exists() else None
    cache_key = (str(settings.chroma_dir), settings.collection_name, metadata_mtime)

    cached_sources = _source_cache.get(cache_key)
    if cached_sources is not None:
        return cached_sources

    result = collection.get(include=["metadatas"])
    sources = sorted(
        {
            str(meta["source_path"])
            for meta in result.get("metadatas", [])
            if meta and meta.get("source_path")
        }
    )

    for key in list(_source_cache):
        if key[0] == str(settings.chroma_dir) and key[1] == settings.collection_name:
            _source_cache.pop(key, None)

    _source_cache[cache_key] = sources
    return sources


def resolve_source_filter(
    collection, source_filter: str, settings: ServerSettings
) -> list[str]:
    if not source_filter:
        return []

    filter_text = source_filter.lower()
    return sorted(
        {
            source_path
            for source_path in _get_source_paths(collection, settings)
            if filter_text in source_path.lower()
        }
    )


@mcp.tool()
def list_sources() -> dict[str, list[str]]:
    collection = get_collection()
    if collection is None:
        return {"sources": []}

    settings = get_settings()
    sources = _get_source_paths(collection, settings)
    return {"sources": sources}


@mcp.tool()
def get_chunk(chunk_id: str) -> dict[str, object]:
    collection = get_collection()
    if collection is None:
        return {"found": False, "chunk_id": chunk_id}

    result = collection.get(ids=[chunk_id], include=["documents", "metadatas"])

    ids = result.get("ids", [])
    if not ids:
        return {"found": False, "chunk_id": chunk_id}

    return {
        "found": True,
        "chunk_id": ids[0],
        "document": result["documents"][0],
        "metadata": result["metadatas"][0],
    }


@mcp.tool()
def search_docs(
    query: str, top_k: int = 5, source_filter: str = ""
) -> dict[str, object]:
    settings = get_settings()
    collection = get_collection()
    if collection is None:
        response: dict[str, object] = {"query": query, "matches": []}
        if source_filter:
            response["source_filter"] = source_filter
            response["matched_sources"] = []
        return response

    top_k = max(1, min(top_k, settings.max_top_k))

    configured_embed_model = settings.embed_model
    index_embed_model = get_index_embed_model(settings)
    query_embed_model = index_embed_model

    warnings: list[str] = []
    reindex_recommended = False
    if configured_embed_model != index_embed_model:
        reindex_recommended = True
        warnings.append(
            "Configured EMBED_MODEL does not match index model; "
            "using index model for query embeddings."
        )

    try:
        query_embedding = embed_query(query, query_embed_model, settings)
    except QueryEmbeddingError as exc:
        error_response: dict[str, object] = {
            "query": query,
            "matches": [],
            "configured_embed_model": configured_embed_model,
            "index_embed_model": index_embed_model,
            "query_embed_model": query_embed_model,
            "error": (
                "Failed to create query embedding with model "
                f"'{query_embed_model}': {exc}"
            ),
        }
        if source_filter:
            error_response["source_filter"] = source_filter
            error_response["matched_sources"] = []
        if warnings:
            error_response["warnings"] = warnings
        error_response["reindex_recommended"] = True
        error_response["reindex_reason"] = (
            "Index embeddings are not compatible with current runtime configuration "
            "or the index model is unavailable. Run 'make reindex' after ensuring "
            "the target model is available in Ollama."
        )
        return error_response

    query_kwargs: dict[str, object] = {
        "query_embeddings": [query_embedding],
        "n_results": top_k,
        "include": ["documents", "metadatas", "distances"],
    }

    matched_sources = resolve_source_filter(collection, source_filter, settings)
    if source_filter:
        if not matched_sources:
            return {"query": query, "source_filter": source_filter, "matches": []}
        query_kwargs["where"] = {"source_path": {"$in": matched_sources}}

    try:
        result = collection.query(
            **query_kwargs,
        )
    except (ChromaError, ValueError, TypeError) as exc:
        error_response = {
            "query": query,
            "matches": [],
            "error": f"Failed to query local index: {exc}",
        }
        if source_filter:
            error_response["source_filter"] = source_filter
            error_response["matched_sources"] = matched_sources
        return error_response

    matches: list[dict[str, object]] = []
    ids = result.get("ids", [[]])[0]
    documents = result.get("documents", [[]])[0]
    metadatas = result.get("metadatas", [[]])[0]
    distances = result.get("distances", [[]])[0]

    for chunk_id, document, metadata, distance in zip(
        ids, documents, metadatas, distances, strict=False
    ):
        source_path = str(metadata.get("source_path", ""))
        matches.append(
            {
                "chunk_id": chunk_id,
                "source_path": source_path,
                "chunk_index": metadata.get("chunk_index"),
                "distance": distance,
                "text": document,
            }
        )

        if len(matches) >= top_k:
            break

    result_response: dict[str, object] = {
        "query": query,
        "matches": matches,
        "configured_embed_model": configured_embed_model,
        "index_embed_model": index_embed_model,
        "query_embed_model": query_embed_model,
    }
    if source_filter:
        result_response["source_filter"] = source_filter
        result_response["matched_sources"] = matched_sources
    if warnings:
        result_response["warnings"] = warnings
    if reindex_recommended:
        result_response["reindex_recommended"] = True
        result_response["reindex_reason"] = (
            "Current index was built with a different embedding model than "
            "EMBED_MODEL. The server used the index model for compatibility. "
            "Run 'make reindex' to align."
        )
    return result_response


if __name__ == "__main__":
    mcp.run(transport="stdio")
