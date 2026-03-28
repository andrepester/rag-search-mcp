from __future__ import annotations

import json

import pytest

from lib import ingest


class _FakeResponse:
    def __init__(self, payload: object) -> None:
        self._payload = payload

    def raise_for_status(self) -> None:
        return None

    def json(self) -> object:
        return self._payload


class _FakeHttpClient:
    def __init__(self, payload: object) -> None:
        self._payload = payload

    def __enter__(self) -> _FakeHttpClient:
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        return None

    def post(self, *_args, **_kwargs) -> _FakeResponse:
        return _FakeResponse(self._payload)


class _FakeCollection:
    def __init__(self) -> None:
        self.add_calls: list[dict[str, object]] = []

    def add(self, **kwargs) -> None:
        self.add_calls.append(kwargs)


class _FakeClient:
    def __init__(self) -> None:
        self.collection = _FakeCollection()

    def get_or_create_collection(self, **_kwargs) -> _FakeCollection:
        return self.collection


def test_chunk_text_validates_arguments() -> None:
    with pytest.raises(ValueError, match="chunk_size"):
        ingest.chunk_text("hello", 0, 0)

    with pytest.raises(ValueError, match="overlap"):
        ingest.chunk_text("hello", 10, -1)


def test_embed_texts_validates_payload(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient({"embeddings": [[1.0, 2.0], [3, 4.5]]}),
    )

    embeddings = ingest.embed_texts(["a", "b"])
    assert embeddings == [[1.0, 2.0], [3.0, 4.5]]

    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient({"embeddings": [[1.0, "bad"]]}),
    )

    with pytest.raises(ingest.IngestError, match="non-numeric"):
        ingest.embed_texts(["a"])

    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient([1, 2, 3]),
    )

    with pytest.raises(ingest.IngestError, match="invalid embeddings payload"):
        ingest.embed_texts(["a"])


def test_build_index_writes_metadata_and_chunks(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    fake_client = _FakeClient()

    monkeypatch.setattr(ingest.chromadb, "PersistentClient", lambda path: fake_client)
    monkeypatch.setattr(
        ingest, "embed_texts", lambda texts: [[0.1, 0.2] for _ in texts]
    )
    monkeypatch.setattr(ingest, "CHROMA_DIR", tmp_path / "index")
    monkeypatch.setattr(ingest, "EMBED_MODEL", "nomic-embed-text:latest")
    monkeypatch.setattr(ingest, "COLLECTION_NAME", "docs")
    monkeypatch.setattr(ingest, "CHUNK_SIZE", 20)
    monkeypatch.setattr(ingest, "CHUNK_OVERLAP", 5)

    count = ingest.build_index(
        [("sample.md", "first paragraph\n\nsecond paragraph\n\nthird paragraph")]
    )

    assert count > 0
    assert fake_client.collection.add_calls

    metadata_path = tmp_path / "index" / ingest.INDEX_METADATA_FILENAME
    assert metadata_path.exists()

    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    assert metadata["embed_model"] == "nomic-embed-text:latest"
    assert metadata["collection_name"] == "docs"
