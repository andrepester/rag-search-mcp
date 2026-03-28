from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, cast

import chromadb
import httpx
from pypdf import PdfReader

from lib.embedding_payload import parse_batch_embedding_payload

DEFAULT_DOCS_DIR = "docs"
DEFAULT_CHROMA_DIR = "index"
DEFAULT_OLLAMA_HOST = "http://127.0.0.1:11434"
DEFAULT_EMBED_MODEL = "nomic-embed-text"
DEFAULT_COLLECTION_NAME = "docs"
DEFAULT_CHUNK_SIZE = 1200
DEFAULT_CHUNK_OVERLAP = 200
SUPPORTED_SUFFIXES = {".md", ".txt", ".pdf"}
INDEX_METADATA_FILENAME = ".rag_index_meta.json"


@dataclass(frozen=True)
class IngestSettings:
    docs_dir: Path
    chroma_dir: Path
    ollama_host: str
    embed_model: str
    collection_name: str
    chunk_size: int
    chunk_overlap: int


def get_settings() -> IngestSettings:
    return IngestSettings(
        docs_dir=Path(os.environ.get("RAG_DOCS_DIR", DEFAULT_DOCS_DIR))
        .expanduser()
        .resolve(),
        chroma_dir=Path(os.environ.get("RAG_CHROMA_DIR", DEFAULT_CHROMA_DIR))
        .expanduser()
        .resolve(),
        ollama_host=os.environ.get("OLLAMA_HOST", DEFAULT_OLLAMA_HOST).rstrip("/"),
        embed_model=os.environ.get("EMBED_MODEL", DEFAULT_EMBED_MODEL),
        collection_name=os.environ.get("COLLECTION_NAME", DEFAULT_COLLECTION_NAME),
        chunk_size=int(os.environ.get("RAG_CHUNK_SIZE", str(DEFAULT_CHUNK_SIZE))),
        chunk_overlap=int(
            os.environ.get("RAG_CHUNK_OVERLAP", str(DEFAULT_CHUNK_OVERLAP))
        ),
    )


class IngestError(RuntimeError):
    """Raised when ingestion or embedding responses are invalid."""


def load_text(path: Path) -> str:
    if path.suffix.lower() == ".pdf":
        reader = PdfReader(str(path))
        return "\n\n".join(page.extract_text() or "" for page in reader.pages)
    return path.read_text(encoding="utf-8", errors="ignore")


def clean_text(text: str) -> str:
    text = text.replace("\r\n", "\n")
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()


def chunk_text(text: str, chunk_size: int, overlap: int) -> list[str]:
    if chunk_size <= 0:
        raise ValueError("chunk_size must be greater than zero")
    if overlap < 0:
        raise ValueError("overlap must be zero or positive")
    if overlap >= chunk_size:
        raise ValueError("overlap must be smaller than chunk_size")

    text = clean_text(text)
    if not text:
        return []

    paragraphs = [p.strip() for p in text.split("\n\n") if p.strip()]
    chunks: list[str] = []
    current = ""

    for para in paragraphs:
        candidate = f"{current}\n\n{para}".strip() if current else para
        if len(candidate) <= chunk_size:
            current = candidate
            continue

        if current:
            chunks.append(current)
            tail = current[-overlap:] if overlap else ""
            current = f"{tail}\n\n{para}".strip()
        else:
            current = para

        while len(current) > chunk_size:
            chunks.append(current[:chunk_size])
            current = current[max(1, chunk_size - overlap) :]

    if current:
        chunks.append(current)

    return [c.strip() for c in chunks if c.strip()]


def embed_texts(texts: list[str], settings: IngestSettings) -> list[list[float]]:
    try:
        with httpx.Client(timeout=120.0) as client:
            response = client.post(
                f"{settings.ollama_host}/api/embed",
                json={"model": settings.embed_model, "input": texts},
            )
            response.raise_for_status()
            data = response.json()
    except httpx.TimeoutException as exc:
        raise IngestError("Timed out while requesting Ollama embeddings.") from exc
    except httpx.HTTPStatusError as exc:
        raise IngestError(
            f"Ollama returned HTTP {exc.response.status_code} for embeddings."
        ) from exc
    except httpx.RequestError as exc:
        raise IngestError(
            f"Failed to reach Ollama at '{settings.ollama_host}'."
        ) from exc
    except ValueError as exc:
        raise IngestError("Ollama returned invalid JSON for embeddings.") from exc

    try:
        return parse_batch_embedding_payload(data, expected_count=len(texts))
    except ValueError as exc:
        raise IngestError(str(exc)) from exc


def iter_documents(settings: IngestSettings) -> list[Path]:
    return sorted(
        path
        for path in settings.docs_dir.rglob("*")
        if path.is_file() and path.suffix.lower() in SUPPORTED_SUFFIXES
    )


def build_index(documents: list[tuple[str, str]], settings: IngestSettings) -> int:
    chunk_text("validation", settings.chunk_size, settings.chunk_overlap)

    settings.chroma_dir.parent.mkdir(parents=True, exist_ok=True)
    temp_dir = Path(
        tempfile.mkdtemp(
            prefix=f"{settings.chroma_dir.name}-tmp-",
            dir=str(settings.chroma_dir.parent),
        )
    )

    try:
        client = chromadb.PersistentClient(path=str(temp_dir))
        collection = client.get_or_create_collection(
            name=settings.collection_name,
            metadata={"hnsw:space": "cosine"},
        )

        ids: list[str] = []
        texts: list[str] = []
        metadatas: list[dict[str, str | int]] = []

        for rel_path, text in documents:
            chunks = chunk_text(text, settings.chunk_size, settings.chunk_overlap)

            for idx, chunk in enumerate(chunks):
                raw_id = f"{rel_path}:{idx}"
                chunk_id = hashlib.sha1(raw_id.encode("utf-8")).hexdigest()[:16]
                ids.append(chunk_id)
                texts.append(chunk)
                metadatas.append(
                    {
                        "source_path": rel_path,
                        "chunk_index": idx,
                    }
                )

        batch_size = 32
        for start in range(0, len(texts), batch_size):
            end = start + batch_size
            batch_docs = texts[start:end]
            embeddings = embed_texts(batch_docs, settings)
            collection.add(
                ids=ids[start:end],
                documents=batch_docs,
                metadatas=cast(Any, metadatas[start:end]),
                embeddings=cast(Any, embeddings),
            )

        metadata_path = temp_dir / INDEX_METADATA_FILENAME
        metadata_path.write_text(
            json.dumps(
                {
                    "schema_version": 1,
                    "embed_model": settings.embed_model,
                    "collection_name": settings.collection_name,
                    "chunk_size": settings.chunk_size,
                    "chunk_overlap": settings.chunk_overlap,
                },
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )

        backup_dir = settings.chroma_dir.with_name(f"{settings.chroma_dir.name}.bak")
        backup_created = False
        if backup_dir.exists():
            shutil.rmtree(backup_dir)

        if settings.chroma_dir.exists():
            settings.chroma_dir.replace(backup_dir)
            backup_created = True

        try:
            temp_dir.replace(settings.chroma_dir)
        except Exception:
            if (
                backup_created
                and backup_dir.exists()
                and not settings.chroma_dir.exists()
            ):
                backup_dir.replace(settings.chroma_dir)
            raise

        if backup_dir.exists():
            shutil.rmtree(backup_dir)

        return len(texts)
    except Exception:
        shutil.rmtree(temp_dir, ignore_errors=True)
        raise


def main() -> None:
    settings = get_settings()

    if not settings.docs_dir.exists():
        raise SystemExit(f"Docs directory is missing: {settings.docs_dir}")

    files = iter_documents(settings)
    if not files:
        raise SystemExit(f"No files found in {settings.docs_dir}")

    documents: list[tuple[str, str]] = []

    for path in files:
        rel_path = str(path.relative_to(settings.docs_dir))
        text = load_text(path)
        documents.append((rel_path, text))

    chunk_count = build_index(documents, settings)
    print(f"Index built: {len(files)} files, {chunk_count} chunks")


if __name__ == "__main__":
    main()
