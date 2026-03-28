from __future__ import annotations

import chromadb.errors
import pytest

from lib import server


@pytest.fixture(autouse=True)
def _reset_server_caches(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(server, "_collection_cache_key", None)
    monkeypatch.setattr(server, "_collection_cache", None)
    monkeypatch.setattr(server, "_source_cache", {})


def _settings(tmp_path, **overrides) -> server.ServerSettings:
    data = {
        "chroma_dir": tmp_path,
        "ollama_host": "http://127.0.0.1:11434",
        "embed_model": "nomic-embed-text",
        "collection_name": "docs",
        "max_top_k": 50,
    }
    data.update(overrides)
    return server.ServerSettings(**data)


class _FakeCollection:
    def __init__(self) -> None:
        self.query_calls: list[dict[str, object]] = []

    def get(self, **kwargs):
        ids = kwargs.get("ids")
        if ids == ["missing"]:
            return {"ids": []}
        if ids:
            return {
                "ids": ["chunk-1"],
                "documents": ["chunk body"],
                "metadatas": [{"source_path": "alpha.md", "chunk_index": 0}],
            }
        return {
            "metadatas": [
                {"source_path": "b.md"},
                {"source_path": "a.md"},
                {"source_path": "a.md"},
            ]
        }

    def query(self, **kwargs):
        self.query_calls.append(kwargs)
        return {
            "ids": [["chunk-1"]],
            "documents": [["chunk body"]],
            "metadatas": [[{"source_path": "alpha.md", "chunk_index": 1}]],
            "distances": [[0.12]],
        }


class _FakeClient:
    def __init__(self, should_raise_not_found: bool = False) -> None:
        self.should_raise_not_found = should_raise_not_found

    def get_collection(self, name: str):
        if self.should_raise_not_found:
            raise chromadb.errors.NotFoundError(f"collection {name} not found")
        return _FakeCollection()


def test_load_index_metadata_invalid_json(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    metadata_path = tmp_path / server.INDEX_METADATA_FILENAME
    metadata_path.write_text("{bad json", encoding="utf-8")
    settings = _settings(tmp_path)

    assert server.load_index_metadata(settings) == {}


def test_get_collection_returns_none_for_missing_collection(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    monkeypatch.setattr(
        server.chromadb,
        "PersistentClient",
        lambda path: _FakeClient(should_raise_not_found=True),
    )
    monkeypatch.setattr(server, "get_settings", lambda: _settings(tmp_path))

    assert server.get_collection() is None


def test_list_sources_and_get_chunk(monkeypatch: pytest.MonkeyPatch) -> None:
    collection = _FakeCollection()
    monkeypatch.setattr(server, "get_collection", lambda: collection)

    assert server.list_sources() == {"sources": ["a.md", "b.md"]}
    assert server.get_chunk("missing") == {"found": False, "chunk_id": "missing"}

    result = server.get_chunk("chunk-1")
    assert result["found"] is True
    assert result["chunk_id"] == "chunk-1"


def test_search_docs_returns_structured_embedding_error(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    settings = _settings(tmp_path, embed_model="configured-model")
    monkeypatch.setattr(server, "get_collection", lambda: _FakeCollection())
    monkeypatch.setattr(server, "get_settings", lambda: settings)
    monkeypatch.setattr(server, "get_index_embed_model", lambda _s: "index-model")
    monkeypatch.setattr(
        server,
        "embed_query",
        lambda query, model, _settings: (_ for _ in ()).throw(
            server.QueryEmbeddingError("boom")
        ),
    )

    response = server.search_docs("hello", source_filter="alpha")

    assert response["matches"] == []
    assert response["reindex_recommended"] is True
    assert response["source_filter"] == "alpha"
    assert response["matched_sources"] == []
    assert "warnings" in response


def test_search_docs_applies_source_filter(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    collection = _FakeCollection()
    monkeypatch.setattr(server, "get_collection", lambda: collection)
    monkeypatch.setattr(server, "get_settings", lambda: _settings(tmp_path))
    monkeypatch.setattr(server, "get_index_embed_model", lambda _s: "nomic-embed-text")
    monkeypatch.setattr(
        server, "embed_query", lambda query, model, _settings: [0.1, 0.2]
    )
    monkeypatch.setattr(
        server,
        "resolve_source_filter",
        lambda c, f, _settings: ["alpha.md"],
    )

    response = server.search_docs("hello", source_filter="alpha")

    assert response["matched_sources"] == ["alpha.md"]
    assert response["matches"]
    assert collection.query_calls
    assert collection.query_calls[0]["where"] == {"source_path": {"$in": ["alpha.md"]}}


def test_search_docs_clamps_top_k(monkeypatch: pytest.MonkeyPatch, tmp_path) -> None:
    collection = _FakeCollection()
    monkeypatch.setattr(server, "get_collection", lambda: collection)
    monkeypatch.setattr(
        server, "get_settings", lambda: _settings(tmp_path, max_top_k=3)
    )
    monkeypatch.setattr(server, "get_index_embed_model", lambda _s: "nomic-embed-text")
    monkeypatch.setattr(
        server, "embed_query", lambda query, model, _settings: [0.1, 0.2]
    )
    monkeypatch.setattr(server, "resolve_source_filter", lambda c, f, _settings: [])

    server.search_docs("hello", top_k=200)

    assert collection.query_calls
    assert collection.query_calls[0]["n_results"] == 3


def test_parse_embedding_payload_validates_payload() -> None:
    assert server.parse_embedding_payload({"embeddings": [[1, 2.5]]}) == [1.0, 2.5]

    with pytest.raises(server.QueryEmbeddingError, match="invalid embeddings payload"):
        server.parse_embedding_payload([1, 2, 3])

    with pytest.raises(server.QueryEmbeddingError, match="missing"):
        server.parse_embedding_payload({})

    with pytest.raises(server.QueryEmbeddingError, match="non-numeric"):
        server.parse_embedding_payload({"embeddings": [["x"]]})
