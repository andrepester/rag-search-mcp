# Configuration And Security Golden Fixture

Configuration is controlled through environment variables such as
RAG_CHUNK_SIZE, RAG_CHUNK_OVERLAP, RAG_SCOPE_DEFAULT, RAG_MAX_TOP_K,
RAG_MAX_SEARCH_DISTANCE, and source_filter. Scope values are all, docs, and code.

The default security boundary is localhost-only loopback access. LAN-only access
is explicit opt-in, WAN exposure is out of scope, and token protection is not a
default v1 release gate.
