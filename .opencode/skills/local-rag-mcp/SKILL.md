# RAG MCP

Use this skill when the user asks about indexed docs or mounted code and you need
retrieval evidence before answering.

## When to use it

- Questions about content stored in mounted `data/docs/` and `data/code/`
- Requests to search or summarize indexed repository material
- Requests to cite or inspect retrieved chunks before answering

## Available MCP tools

- `rag_list_sources` to list indexed source files by scope
- `rag_search` to run semantic search against the shared index
- `rag_get_chunk` to fetch a chunk by its `chunk_id`
- `rag_reindex` to rebuild the index from mounted sources

## Recommended workflow

1. Decide `scope` first (`docs`, `code`, or `all`; default is `all`).
2. Call `rag_list_sources` when you need to discover what is indexed.
3. Call `rag_search` with the user's topic and selected scope.
4. Use `source_filter` with part of a path to narrow retrieval to matching files when useful.
5. Call `rag_get_chunk` for the strongest matches when you need more context.
6. Answer using the retrieved evidence and mention the source paths you used.

## Retrieval guidelines

- Prefer MCP retrieval over guessing when the answer should come from indexed data.
- If the first search is noisy, retry with a shorter or more specific query.
- If the user names a document, pass part of that path with `source_filter`.
- If `scope=code` returns no hits, verify whether code is mounted/indexed before retrying.
- If no good match appears, say so clearly instead of inventing details.

## Example prompts

- "List indexed docs and code sources with `rag_list_sources` using `scope=all`."
- "Search docs only for installation with `rag_search` and `scope=docs`."
- "Search code only for chunking logic with `rag_search` and `scope=code`."
- "Search both docs and code with `rag_search` and `scope=all`."
