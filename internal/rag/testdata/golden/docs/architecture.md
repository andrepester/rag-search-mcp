# Architecture Golden Fixture

rag-search-mcp exposes an MCP service for semantic retrieval across local
documentation and code. The architecture uses Ollama embeddings and a Chroma
vector collection named rag.

The rag-mcp service accepts search requests, embeds the query, asks Chroma for
nearest chunks, and returns source paths, scopes, chunk indexes, distances, and
text.
