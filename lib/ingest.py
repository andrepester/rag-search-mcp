from __future__ import annotations

import hashlib
import os
from pathlib import Path
import re
import shutil
import tempfile

import chromadb
import httpx
from pypdf import PdfReader

DOCS_DIR = Path(os.environ.get("RAG_DOCS_DIR", "docs")).expanduser().resolve()
CHROMA_DIR = Path(os.environ.get("RAG_CHROMA_DIR", "index")).expanduser().resolve()
OLLAMA_HOST = os.environ.get("OLLAMA_HOST", "http://127.0.0.1:11434").rstrip("/")
EMBED_MODEL = os.environ.get("EMBED_MODEL", "nomic-embed-text")
COLLECTION_NAME = os.environ.get("COLLECTION_NAME", "docs")
CHUNK_SIZE = int(os.environ.get("RAG_CHUNK_SIZE", "1200"))
CHUNK_OVERLAP = int(os.environ.get("RAG_CHUNK_OVERLAP", "200"))
SUPPORTED_SUFFIXES = {".md", ".txt", ".pdf"}


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


def embed_texts(texts: list[str]) -> list[list[float]]:
    with httpx.Client(timeout=120.0) as client:
        response = client.post(
            f"{OLLAMA_HOST}/api/embed",
            json={"model": EMBED_MODEL, "input": texts},
        )
        response.raise_for_status()
        data = response.json()
    return data["embeddings"]


def iter_documents() -> list[Path]:
    return sorted(
        path
        for path in DOCS_DIR.rglob("*")
        if path.is_file() and path.suffix.lower() in SUPPORTED_SUFFIXES
    )


def build_index(documents: list[tuple[str, str]]) -> int:
    CHROMA_DIR.parent.mkdir(parents=True, exist_ok=True)
    temp_dir = Path(
        tempfile.mkdtemp(prefix=f"{CHROMA_DIR.name}-tmp-", dir=str(CHROMA_DIR.parent))
    )

    try:
        client = chromadb.PersistentClient(path=str(temp_dir))
        collection = client.get_or_create_collection(
            name=COLLECTION_NAME,
            metadata={"hnsw:space": "cosine"},
        )

        ids: list[str] = []
        texts: list[str] = []
        metadatas: list[dict[str, str | int]] = []

        for rel_path, text in documents:
            chunks = chunk_text(text, CHUNK_SIZE, CHUNK_OVERLAP)

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
            embeddings = embed_texts(batch_docs)
            collection.add(
                ids=ids[start:end],
                documents=batch_docs,
                metadatas=metadatas[start:end],
                embeddings=embeddings,
            )

        backup_dir = CHROMA_DIR.with_name(f"{CHROMA_DIR.name}.bak")
        if backup_dir.exists():
            shutil.rmtree(backup_dir)

        if CHROMA_DIR.exists():
            CHROMA_DIR.replace(backup_dir)

        temp_dir.replace(CHROMA_DIR)

        if backup_dir.exists():
            shutil.rmtree(backup_dir)

        return len(texts)
    except Exception:
        shutil.rmtree(temp_dir, ignore_errors=True)
        raise


def main() -> None:
    if not DOCS_DIR.exists():
        raise SystemExit(f"Docs directory is missing: {DOCS_DIR}")

    files = iter_documents()
    if not files:
        raise SystemExit(f"No files found in {DOCS_DIR}")

    documents: list[tuple[str, str]] = []

    for path in files:
        rel_path = str(path.relative_to(DOCS_DIR))
        text = load_text(path)
        documents.append((rel_path, text))

    chunk_count = build_index(documents)
    print(f"Index built: {len(files)} files, {chunk_count} chunks")


if __name__ == "__main__":
    main()
