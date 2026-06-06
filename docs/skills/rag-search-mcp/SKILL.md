# RAG Search MCP Skill Template

Use this skill when the user asks for retrieval-backed answers from documentation
or code indexed by a `rag-search-mcp` server.

## When To Use It

- Questions about mounted documentation, runbooks, source code, or examples
- Requests to search, summarize, compare, or cite indexed project material
- Requests to inspect a specific indexed file, source path, or chunk
- Debugging or architecture questions where local repository evidence matters
- Follow-up questions that need fresh retrieval rather than remembered context

Typical user phrasing includes:

- "Search the indexed docs for ..."
- "Find where the code handles ..."
- "Use RAG to answer ..."
- "Summarize the mounted documentation about ..."
- "Which source mentions ..."

## Available MCP Tools

- `rag_list_sources` lists indexed source files by `scope`
- `rag_search` runs semantic search against the shared index
- `rag_get_chunk` fetches a chunk by `chunk_id`
- `rag_reindex` rebuilds the index from mounted sources
- `rag_reindex_status` reports active, completed, and blocked reindex runs

## Recommended Workflow

1. Choose `scope` first: `docs`, `code`, or `all`.
2. Use `rag_list_sources` when the indexed source set is unclear.
3. Use `rag_search` with a focused query and the selected scope.
4. Use `source_filter` with part of a path when the user names a document or file.
5. Fetch the strongest match with `rag_get_chunk` when more context is needed.
6. Answer from retrieved evidence and mention the source paths used.

## Retrieval Guidelines

- Prefer retrieval over guessing whenever the answer should come from indexed data.
- Retry with shorter or more specific queries when results are noisy.
- If `scope=code` returns no useful hits, check whether code is mounted and indexed.
- If no good match appears, say that clearly and describe what was searched.
- Do not assume client-specific configuration; this skill only depends on the
  `rag-search-mcp` MCP tools being available in the current client.

## Endpoint Note

The default local remote-MCP endpoint is:

```text
http://127.0.0.1:${RAG_HTTP_PORT}/mcp
```

With the default port, that is:

```text
http://127.0.0.1:8765/mcp
```
