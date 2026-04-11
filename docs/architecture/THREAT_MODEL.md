# Threat Model v1 (kompakt)

Dieses Dokument beschreibt die Security-Ableitung zur ADR-Entscheidung aus `docs/architecture/RAG-SEARCH-MCP-ADR-2026-04-11-lan-betriebsmodus.md`.

## Scope

- Endpunkte: `/mcp`, `/healthz`
- Betriebsmodus gemaess ADR: `localhost-only` (Default), `LAN-only` (Opt-in)

## Umsetzungsstatus

- Aktuell gilt operational `localhost-only` als freigegebener Modus.
- `LAN-only` wird erst mit aktiver Token-Absicherung fuer Nicht-Loopback-Zugriffe freigegeben (Umsetzung ueber `[[backlog/P1-004-api-security-token-baseline|P1-004]]`).

## Bedrohungen (v1)

- Unautorisierte Zugriffe von Clients im LAN
- Fehlkonfiguration durch zu breite Exposition (z. B. WAN/oeffentliche Erreichbarkeit)
- Kompromittierte LAN-Clients mit legitimer Netznaehe

## Verbindliche Controls

- Netzgrenzen: primaer Docker/Host/Firewall, nur freigegebene Netze im LAN-Opt-in
- Authentisierung: Token-Pflicht fuer Nicht-Loopback-Zugriffe
- CORS: kein permissiver Default
- Discovery: keine automatische Service Discovery in v1

## Test-/Compliance-Checks

- `localhost-default`: mit Default-Config ist Zugriff auf `/mcp` nur lokal erfolgreich.
- `LAN-opt-in`: nur mit expliziter Non-Loopback-Bind/Publish-Konfiguration und dokumentierter Source-Netzgrenze.
- `Auth`: Nicht-Loopback-Requests ohne gueltiges Token werden abgewiesen.
- `Out-of-scope`: keine Exposition ueber WAN/oeffentliche Interfaces, kein VPN/Overlay-Zugriff ohne neues Threat Model.

## Out-of-Scope (v1)

- WAN/Internet-Exposition
- VPN/Overlay-Access ohne separates Threat Model
- Offener Reverse-Proxy/Ingress ins Internet

## Restrisiken

- Fehlkonfiguration auf Host-/Firewall-Ebene
- Kompromittierte Clients im erlaubten LAN-Segment
- Token-Leakage ohne saubere Rotation/Operational Hygiene

## Folgearbeiten

- `[[backlog/P1-004-api-security-token-baseline|P1-004 API Security Baseline (Token-first)]]`
- `[[backlog/P1-003-macos-linux-harmonization|P1-003 MacOS/Linux Harmonisierung fuer Docker-Workflows]]`
- `[[backlog/P1-009-observability-baseline|P1-009 Observability-Baseline (Metriken, Logs, Health)]]`
