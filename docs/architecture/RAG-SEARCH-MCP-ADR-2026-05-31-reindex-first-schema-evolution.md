# ADR: Reindex-first Index Schema Evolution

| Field | Content |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-05-31 |
| Name | Use reindex-first as the approved path for incompatible index and schema changes |
| Status | Accepted |
| Decision Question | How should `rag-search-mcp` handle incompatible changes to the index, ingestion, or query assumptions: explicit migration of old index artifacts, or a full reindex? |
| Context / Constraints | `rag-search-mcp` is a small, privately operated Docker-first project. The index is reproducibly built from mounted documentation and code sources, configuration, chunking rules, the embedding model, and metadata. Persisted index artifacts are operational state, not the primary source of truth. The product decision recorded in Vikunja `#12` on 2026-05-31 is that the project will not implement a dedicated index schema migration path. `#25` records the resulting architecture decision for ingestion and query behavior. |
| Decision & Rationale | Incompatible changes to chunking, metadata, the embedding model, collection/source layout, or query assumptions are handled by a full reindex. In this project, reindexing is legitimate and is the intended operational path. This reduces migration complexity, avoids a long-lived compatibility burden for old index artifacts, and fits the existing Make and runtime interface (`make index`, `rag_reindex`, `make install`, `make doctor`). |
| Alternatives | 1) Explicit schema versions and migrations for existing index artifacts: rejected because the effort and failure risk are higher than the value for the current private operating model. 2) Guarantee backward compatibility for old indexes without reindexing: rejected because chunking, embeddings, and metadata are tightly coupled to the code and fixture state. 3) Fully reset external model state for every incompatible change: rejected because model lifecycle is owned by the shared Ollama host and is independent from local index rebuilds. |
| Date / Documentation | 2026-05-31; README troubleshooting; Vikunja backlog items: `#12`, `#25` |
| Actors | User (product decision that reindexing is legitimate), OpenCode Assistant (documentation) |

## Assumptions

- Mounted documentation and code sources remain the primary source of truth.
- Index artifacts can be rebuilt from sources, configuration, and the embedding model.
- The current operating model does not require stable upgrade guarantees for old index formats.
- Shared Ollama model lifecycle is separate from the local index rebuild lifecycle.

## Consequences / Operational Implications

- New incompatible index or query changes do not need to include migration code for old index artifacts.
- Documentation, tests, and CI may assume a freshly generated index for incompatible changes.
- Golden-query and fixture-based tests should build their index reproducibly in the test path instead of migrating older index versions.
- Release notes or PR descriptions for incompatible changes must clearly call out the required reindex.
- Backup and restore concerns for non-regenerable operational data are not addressed by this decision.
- If the project later needs stable upgrade guarantees, multi-user operation, or external releases with data-retention promises, this decision must be revisited.

## Validation / Evidence

- After incompatible changes, a successful `make index FRESH_INDEX=1` or `rag_reindex` is the expected evidence of a valid index.
- `make doctor` may use reindexing and index verification as operational checks without migrating old index artifacts.
- Retrieval regression tests generate their fixture index from scratch and document the model, scope, top-k, chunking, and fixture state.
- Documentation and backlog items must no longer assume that v1 has an explicit index migration path.
