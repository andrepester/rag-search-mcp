from __future__ import annotations

import json
import os
from pathlib import Path

import chromadb
import httpx
from chromadb.errors import ChromaError, NotFoundError
from mcp.server.fastmcp import FastMCP

CHROMA_DIR = Path(os.environ.get("RAG_CHROMA_DIR", "index")).expanduser().resolve()
OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://127.0.0.1:11434").rstrip("/")
EMBED_MODEL = os.environ.get("EMBED_MODEL", "nomic-embed-text")
COLLECTION_NAME = os.environ.get("COLLECTION_NAME", "docs")
INDEX_METADATA_FILENAME = ".rag_index_meta.json"

mcp = FastMCP("local_rag", json_response=True)


class QueryEmbeddingError(RuntimeError):
    """Raised when query embeddings cannot be created or parsed."""


def load_index_metadata() -> dict[str, object]:
    metadata_path = CHROMA_DIR / INDEX_METADATA_FILENAME
    if not metadata_path.exists():
        return {}

    try:
        content = metadata_path.read_text(encoding="utf-8")
        data = json.loads(content)
        return data if isinstance(data, dict) else {}
    except (OSError, json.JSONDecodeError):
        return {}


def get_index_embed_model() -> str:
    metadata = load_index_metadata()
    model = metadata.get("embed_model")
    if isinstance(model, str) and model.strip():
        return model.strip()
    return EMBED_MODEL


def get_collection():
    client = chromadb.PersistentClient(path=str(CHROMA_DIR))
    try:
        return client.get_collection(name=COLLECTION_NAME)
    except (NotFoundError, ChromaError, OSError, ValueError):
        return None


def parse_embedding_payload(payload: object) -> list[float]:
    if not isinstance(payload, dict):
        raise QueryEmbeddingError("Ollama returned an invalid embeddings payload.")

    embeddings = payload.get("embeddings")
    if not isinstance(embeddings, list) or not embeddings:
        raise QueryEmbeddingError("Ollama response is missing 'embeddings'.")

    first_embedding = embeddings[0]
    if not isinstance(first_embedding, list) or not first_embedding:
        raise QueryEmbeddingError(
            "Ollama response contains an invalid embedding vector."
        )

    values: list[float] = []
    for value in first_embedding:
        if not isinstance(value, (int, float)):
            raise QueryEmbeddingError("Embedding vector contains non-numeric values.")
        values.append(float(value))
    return values


def embed_query(text: str, embed_model: str) -> list[float]:
    try:
        with httpx.Client(timeout=120.0) as client:
            response = client.post(
                f"{OLLAMA_HOST}/api/embed",
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
            f"Failed to reach Ollama at '{OLLAMA_HOST}'."
        ) from exc
    except ValueError as exc:
        raise QueryEmbeddingError(
            "Ollama returned invalid JSON for embeddings."
        ) from exc

    return parse_embedding_payload(data)


def resolve_source_filter(collection, source_filter: str) -> list[str]:
    if not source_filter:
        return []

    result = collection.get(include=["metadatas"])
    filter_text = source_filter.lower()
    return sorted(
        {
            str(meta["source_path"])
            for meta in result.get("metadatas", [])
            if meta and filter_text in str(meta.get("source_path", "")).lower()
        }
    )


@mcp.tool()
def list_sources() -> dict[str, list[str]]:
    collection = get_collection()
    if collection is None:
        return {"sources": []}

    result = collection.get(include=["metadatas"])
    sources = sorted(
        {
            meta["source_path"]
            for meta in result.get("metadatas", [])
            if meta and meta.get("source_path")
        }
    )
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
    collection = get_collection()
    if collection is None:
        response: dict[str, object] = {"query": query, "matches": []}
        if source_filter:
            response["source_filter"] = source_filter
            response["matched_sources"] = []
        return response

    top_k = max(1, top_k)

    configured_embed_model = EMBED_MODEL
    index_embed_model = get_index_embed_model()
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
        query_embedding = embed_query(query, query_embed_model)
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

    matched_sources = resolve_source_filter(collection, source_filter)
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
