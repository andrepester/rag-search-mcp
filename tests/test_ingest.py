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


def _settings(tmp_path, **overrides) -> ingest.IngestSettings:
    data = {
        "docs_dir": tmp_path / "docs",
        "chroma_dir": tmp_path / "index",
        "ollama_host": "http://127.0.0.1:11434",
        "embed_model": "nomic-embed-text",
        "collection_name": "docs",
        "chunk_size": 20,
        "chunk_overlap": 5,
    }
    data.update(overrides)
    return ingest.IngestSettings(**data)


def test_chunk_text_validates_arguments() -> None:
    with pytest.raises(ValueError, match="chunk_size"):
        ingest.chunk_text("hello", 0, 0)

    with pytest.raises(ValueError, match="overlap"):
        ingest.chunk_text("hello", 10, -1)

    with pytest.raises(ValueError, match="smaller"):
        ingest.chunk_text("hello", 10, 10)


def test_embed_texts_validates_payload(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    settings = _settings(tmp_path)

    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient({"embeddings": [[1.0, 2.0], [3, 4.5]]}),
    )

    embeddings = ingest.embed_texts(["a", "b"], settings)
    assert embeddings == [[1.0, 2.0], [3.0, 4.5]]

    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient({"embeddings": [[1.0, "bad"]]}),
    )

    with pytest.raises(ingest.IngestError, match="non-numeric"):
        ingest.embed_texts(["a"], settings)

    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient([1, 2, 3]),
    )

    with pytest.raises(ingest.IngestError, match="invalid embeddings payload"):
        ingest.embed_texts(["a"], settings)


def test_embed_texts_rejects_count_mismatch(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    settings = _settings(tmp_path)
    monkeypatch.setattr(
        ingest.httpx,
        "Client",
        lambda timeout: _FakeHttpClient({"embeddings": [[1.0, 2.0]]}),
    )

    with pytest.raises(ingest.IngestError, match="unexpected embedding count"):
        ingest.embed_texts(["a", "b"], settings)


def test_build_index_writes_metadata_and_chunks(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    fake_client = _FakeClient()
    settings = _settings(tmp_path, embed_model="nomic-embed-text:latest")

    monkeypatch.setattr(ingest.chromadb, "PersistentClient", lambda path: fake_client)
    monkeypatch.setattr(
        ingest,
        "embed_texts",
        lambda texts, _settings: [[0.1, 0.2] for _ in texts],
    )

    count = ingest.build_index(
        [("sample.md", "first paragraph\n\nsecond paragraph\n\nthird paragraph")],
        settings,
    )

    assert count > 0
    assert fake_client.collection.add_calls

    metadata_path = settings.chroma_dir / ingest.INDEX_METADATA_FILENAME
    assert metadata_path.exists()

    metadata = json.loads(metadata_path.read_text(encoding="utf-8"))
    assert metadata["embed_model"] == "nomic-embed-text:latest"
    assert metadata["collection_name"] == "docs"


def test_build_index_rolls_back_on_swap_failure(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    fake_client = _FakeClient()
    settings = _settings(tmp_path)

    settings.chroma_dir.mkdir(parents=True, exist_ok=True)
    marker = settings.chroma_dir / "marker.txt"
    marker.write_text("existing", encoding="utf-8")

    temp_dir = tmp_path / "index-tmp-fixed"

    def fake_mkdtemp(*_args, **_kwargs) -> str:
        temp_dir.mkdir(parents=True, exist_ok=True)
        return str(temp_dir)

    original_replace = ingest.Path.replace

    def flaky_replace(self: ingest.Path, target: ingest.Path) -> ingest.Path:
        if self == temp_dir and target == settings.chroma_dir:
            raise OSError("swap failed")
        return original_replace(self, target)

    monkeypatch.setattr(ingest.chromadb, "PersistentClient", lambda path: fake_client)
    monkeypatch.setattr(ingest.tempfile, "mkdtemp", fake_mkdtemp)
    monkeypatch.setattr(
        ingest,
        "embed_texts",
        lambda texts, _settings: [[0.1, 0.2] for _ in texts],
    )
    monkeypatch.setattr(ingest.Path, "replace", flaky_replace)

    with pytest.raises(OSError, match="swap failed"):
        ingest.build_index([("sample.md", "alpha\n\nbeta")], settings)

    assert marker.exists()
    assert marker.read_text(encoding="utf-8") == "existing"
