# ADR: Operating Mode v1 (localhost by default, LAN opt-in)

| Field | Content |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-04-11 |
| Name | Define the v1 operating mode as localhost by default with optional LAN-only opt-in |
| Status | Accepted, updated on 2026-05-28 |
| Decision Question | What operating mode should v1 of `rag-search-mcp` enforce so local use and optional LAN operation are possible without allowing clients outside the local network? |
| Context / Constraints | The repository is currently localhost-oriented: `RAG_HTTP_HOST` defaults to `127.0.0.1`, and Docker publishes ports on loopback. At the same time, the service must support operation on a local network. The MCP endpoint is HTTP-based (`/mcp`), so exposing it on a LAN increases the risk of unauthorized access. Security boundaries must be testable and operationally enforceable. |
| Decision & Rationale | v1 supports two exposure profiles: (1) default `localhost-only`; (2) optional `LAN-only` as an explicit opt-in for controlled local networks. WAN/Internet exposure and VPN/overlay access are not supported in v1. LAN-only operation must be deliberately constrained at the Docker, host, and firewall layers. Based on the 2026-05-28 product decision, token-based API protection for non-loopback access is no longer part of the v1 standard flow and is not a release gate. This keeps the default safe, allows the requested LAN operation without extra authentication complexity, and makes the LAN risk acceptance explicit. |
| Alternatives | 1) Strict `localhost-only`: rejected because LAN operation is an explicit user requirement. 2) Default `0.0.0.0` / open LAN exposure: rejected because it creates too much misconfiguration and exposure risk. 3) Treat LAN and VPN access as equivalent in v1: rejected because the trust boundary becomes ambiguous and would require additional controls and operational guidance. |
| Date / Documentation | 2026-04-11; updated 2026-05-28; Threat Model: `docs/architecture/THREAT_MODEL.md`; Vikunja backlog items: `P0-009`, `#16` |
| Actors | User (LAN operation requirement), OpenCode Assistant (drafting), architecture review (`architect` subagent) |

## Assumptions

- The operator controls the host network and firewall rules.
- LAN segments are not trusted by default; the current v1 LAN opt-in deliberately accepts this residual risk for controlled local networks.
- MCP access uses HTTP and can be exposed through the host if the host or Docker configuration is wrong.

## Consequences / Operational Implications

- Default installations remain limited to loopback.
- LAN operation is a deliberate opt-in and requires additional operational care around network boundaries, documentation, and risk acceptance, but it does not require token authentication in the current v1 standard flow.
- WAN and VPN scenarios require a separate, explicit threat model and are out of scope for v1.
- A future web UI, public exposure model, or broader team/overlay deployment must revisit authentication, CORS, and deployment boundaries.

## Validation / Evidence

- `localhost-only`: the service is reachable locally and not from an external network segment.
- `LAN-only`: access from the local network is opened only through explicit non-loopback bind/publish configuration.
- LAN opt-in: only approved source networks can reach the service; WAN and public exposure remain excluded.
- Token authentication: not a v1 requirement for controlled LAN opt-in; documentation and tests must not assume that this protection exists.
