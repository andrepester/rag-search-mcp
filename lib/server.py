from __future__ import annotations

import os
from pathlib import Path

import chromadb
import httpx
from mcp.server.fastmcp import FastMCP

CHROMA_DIR = Path(os.environ.get("RAG_CHROMA_DIR", "index")).expanduser().resolve()
OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://127.0.0.1:11434").rstrip("/")
EMBED_MODEL = os.environ.get("EMBED_MODEL", "nomic-embed-text")
COLLECTION_NAME = os.environ.get("COLLECTION_NAME", "docs")

mcp = FastMCP("local_rag", json_response=True)


def get_collection():
    client = chromadb.PersistentClient(path=str(CHROMA_DIR))
    try:
        return client.get_collection(name=COLLECTION_NAME)
    except Exception:
        return None


def embed_query(text: str) -> list[float]:
    with httpx.Client(timeout=120.0) as client:
        response = client.post(
            f"{OLLAMA_HOST}/api/embed",
            json={"model": EMBED_MODEL, "input": text},
        )
        response.raise_for_status()
        data = response.json()
    return data["embeddings"][0]


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

    query_kwargs: dict[str, object] = {
        "query_embeddings": [embed_query(query)],
        "n_results": top_k,
        "include": ["documents", "metadatas", "distances"],
    }

    matched_sources = resolve_source_filter(collection, source_filter)
    if source_filter:
        if not matched_sources:
            return {"query": query, "source_filter": source_filter, "matches": []}
        query_kwargs["where"] = {"source_path": {"$in": matched_sources}}

    result = collection.query(
        **query_kwargs,
    )

    matches: list[dict[str, object]] = []
    ids = result.get("ids", [[]])[0]
    documents = result.get("documents", [[]])[0]
    metadatas = result.get("metadatas", [[]])[0]
    distances = result.get("distances", [[]])[0]

    for chunk_id, document, metadata, distance in zip(
        ids, documents, metadatas, distances
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

    response: dict[str, object] = {"query": query, "matches": matches}
    if source_filter:
        response["source_filter"] = source_filter
        response["matched_sources"] = matched_sources
    return response


if __name__ == "__main__":
    mcp.run(transport="stdio")
