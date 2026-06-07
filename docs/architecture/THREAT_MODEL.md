# Threat Model v1 (compact)

This document captures the security implications of the ADR decision in `docs/architecture/RAG-SEARCH-MCP-ADR-2026-04-11-lan-operating-mode.md`.

## Scope

- Endpoints: `/mcp`, `/`, `/ui/`, `/api/search`, `/api/chunk`, `/api/sources`,
  `/healthz`, `/readyz`, `/metrics`
- Operating mode defined by the ADR: `localhost-only` (default), `LAN-only` (opt-in)

## Implementation Status

- Default installations operate in `localhost-only` mode.
- `LAN-only` is an explicit opt-in for controlled local networks.
- Token-based API protection for non-loopback access is not implemented as a standard flow or release gate in v1. This product decision was documented in Vikunja `#16` on 2026-05-28.
- The web interface is served by the existing `rag-mcp` process and Docker port.
  Its internal JSON routes are same-origin browser routes, not a separate public
  REST API product.

## Threats (v1)

- Unauthorized access from clients on the LAN
- Misconfiguration that exposes the service too broadly, such as WAN or public reachability
- Compromised LAN clients with legitimate network proximity
- XSS or content injection through indexed chunk text rendered in the browser UI
- Accidental disclosure of queries, chunk text, source snippets, embeddings, or
  request bodies through UI/API logs, metrics, or readiness responses

## Required Controls

- Network boundaries: primarily enforced through Docker, the host, and firewall rules; LAN opt-in allows only approved networks.
- Authentication: no token requirement for the current v1 LAN opt-in; operators deliberately accept the local LAN trust boundary.
- CORS: no permissive default. Browser UI calls remain same-origin.
- Web content security: the UI uses a restrictive same-origin Content Security
  Policy, and indexed result/chunk content is rendered as text rather than HTML.
- Discovery: no automatic service discovery in v1.
- Readiness: `/readyz` exposes only dependency names, status, errors, and remediation hints; it must not expose queries, chunk text, embeddings, source snippets, or request bodies.
- Runtime logs: CLI reindex logs may include configured docs/code source roots as operational context; logs must not expose queries, chunk text, embeddings, source snippets, or request bodies.
- Metrics: `/metrics` exposes bounded operational counters and gauges only; metric labels must not include queries, paths, chunk text, source snippets, embeddings, or request bodies.

## Test / Compliance Checks

- `localhost-default`: with the default configuration, `/mcp` is reachable only locally.
- `LAN-opt-in`: requires explicit non-loopback bind/publish configuration and a documented source-network boundary.
- `Auth`: documentation and configuration must not assume active token protection for v1 LAN opt-in.
- `Web UI`: `/` and `/ui/` serve the browser UI on the existing port and do not
  require a separate frontend server.
- `CORS`: UI API responses do not set `Access-Control-Allow-Origin: *`.
- `XSS`: search results and chunk details are inserted as text, not interpreted as
  HTML or Markdown.
- `Out-of-scope`: no exposure through WAN/public interfaces, and no VPN/overlay access without a new threat model.

## Out-of-Scope (v1)

- WAN/Internet exposure
- VPN/overlay access without a separate threat model
- Open reverse proxy or ingress to the Internet

## Residual Risks

- Misconfiguration at the host or firewall layer
- Compromised clients inside the allowed LAN segment
- Unauthorized use of `/mcp`, `/`, `/ui/`, or internal UI API routes by reachable
  LAN clients because there is no application-level token requirement
- Future web, WAN, VPN, or overlay scenarios cannot rely on this v1 risk acceptance

## Follow-up Work in Vikunja

- `#16 API Security Baseline (Token-first)` is deferred and is not a v1 release gate.
- `#17 Webinterface fuer RAG-Suche`
- `P1-003 macOS/Linux harmonization for Docker workflows`
- `P1-009 Observability baseline (metrics, logs, health)`
