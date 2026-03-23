# Local RAG MCP

Use this skill when the user asks about documents indexed by this repository's local
RAG setup or when you need repo-local retrieval before answering.

## When to use it

- Questions about content stored in `docs/`
- Requests to search or summarize indexed local documents
- Requests to cite or inspect retrieved chunks before answering

## Available MCP tools

- `local_rag_list_sources` to list indexed source files
- `local_rag_search_docs` to run semantic search against the local index
- `local_rag_get_chunk` to fetch a chunk by its `chunk_id`

## Recommended workflow

1. Call `local_rag_list_sources` when you need to discover what is indexed.
2. Call `local_rag_search_docs` with the user's topic.
3. Use `source_filter` with part of a path to narrow retrieval to matching files when useful.
4. Call `local_rag_get_chunk` for the strongest matches when you need more context.
5. Answer using the retrieved local evidence and mention the source paths you used.

## Retrieval guidelines

- Prefer local MCP retrieval over guessing when the answer should come from indexed docs.
- If the first search is noisy, retry with a shorter or more specific query.
- If the user names a document, pass part of that path with `source_filter`.
- If no good match appears, say so clearly instead of inventing details.

## Example prompts

- "List the local RAG sources in this project."
- "Search the local docs for how OpenCode is wired to MCP."
- "Find the chunk about installation steps and summarize it."
- "Use local docs to explain this repo's architecture."
