# ADR: Atomic Index Generation Switch

| Field | Content |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-06-04-ATOMIC-GENERATION-SWITCH |
| Name | Use a global Chroma collection with an atomic active generation pointer |
| Status | Accepted |
| Decision Question | How should `rag-search-mcp` keep search results consistent during reindexing after the index topology decision rejected separate namespaces or per-source collections? |
| Context / Constraints | The accepted topology is a single global Chroma collection with scope and source metadata filters. Vikunja `#14` originally described an atomic collection switch, but the 2026-05-31 task comment clarified that a real collection switch is not appropriate without separate namespaces. Index artifacts remain reproducible operational state under the host persistence boundary, and `#12`/`#25` keep reindex-first as the migration path for incompatible index changes. |
| Decision & Rationale | Reindexing builds a new `index_generation` inside the configured global Chroma collection. Queries, chunk lookup, and source listing read only the active generation. The active generation is stored in an atomically written pointer file under the host index persistence path, so the running MCP service and the CLI reindex process share the same active state. Unchanged sources may be copied into the new generation without recomputing embeddings. The user-visible switch happens when the pointer file is renamed into place, not when build chunks are written. |
| Alternatives | 1) Rotate between separate Chroma collections: rejected because the topology decision is a global index, not namespace or collection separation. 2) Delete and recreate the active collection: rejected because it makes the active query state unavailable or inconsistent during rebuild. 3) Mark individual records active/inactive in place: rejected because it requires many non-atomic metadata updates and can expose mixed generations. |
| Date / Documentation | 2026-06-04; README operations/troubleshooting; Vikunja backlog item `#14` |
| Actors | User (clarified that a real collection switch conflicts with the global-index decision), OpenCode Assistant (implementation and documentation) |

## Assumptions

- `RAG_COLLECTION_NAME` remains the single global query collection.
- `scope`, `source_path`, `chunk_id`, `source_hash`, and `index_generation` metadata are sufficient to isolate active results.
- The active pointer file lives under the existing `HOST_INDEX_DIR` persistence boundary and is reset by `make clean-install FULL_RESET=1`.
- Source directories remain the source of truth; index generations and pointer files are derived operational state.
- The later single-writer/job-status model can share the same host-persistent state directory without changing active-generation query semantics.

## Consequences / Operational Implications

- During reindexing, existing searches continue to read the previous active generation until the new generation is complete.
- If embedding, Chroma writes, source loading, or state-file activation fails before the pointer switch, the previous active generation remains visible.
- Deleted sources are no longer visible after a successful pointer switch because queries filter by the new active generation.
- Old generations remain available after activation so in-flight queries that already read the previous pointer can finish; a future cleanup path may remove stale generations after a safe grace boundary.
- Standard `make index` runs and `rag_reindex` share the same activation model because both write the same pointer file.
- `make index FRESH_INDEX=1` is an explicit destructive reset path: it recreates the configured collection before rebuilding and therefore does not preserve active-query continuity during the fresh run.
- Existing unversioned indexes require a reindex after this incompatible index-layout change, consistent with the reindex-first ADR.
- The follow-up single-writer/job-status implementation stores `reindex.lock` and `reindex-status.json` beside the active pointer so CLI and MCP reindex paths coordinate around the same build state.

## Validation / Evidence

- `internal/ingest` builds a fresh generation and writes the active pointer only after all selected source records are available.
- `internal/rag` loads the active generation before search, chunk lookup, and source listing, so CLI-triggered reindex runs are visible to the running service.
- Tests cover unchanged-source reuse, changed-source embedding, deleted-source activation, generation-filtered source listing, generation-filtered chunk lookup, and deterministic golden queries.
- `docker/docker-compose.yml` mounts `HOST_INDEX_DIR/rag-state` into `rag-mcp` as the index state directory.
