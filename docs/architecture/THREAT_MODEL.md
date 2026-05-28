# Threat Model v1 (kompakt)

Dieses Dokument beschreibt die Security-Ableitung zur ADR-Entscheidung aus `docs/architecture/RAG-SEARCH-MCP-ADR-2026-04-11-lan-betriebsmodus.md`.

## Scope

- Endpunkte: `/mcp`, `/healthz`
- Betriebsmodus gemaess ADR: `localhost-only` (Default), `LAN-only` (Opt-in)

## Umsetzungsstatus

- Default-Installationen bleiben operational `localhost-only`.
- `LAN-only` ist ein expliziter Opt-in fuer kontrollierte lokale Netze.
- Token-basierte API-Absicherung fuer Nicht-Loopback-Zugriffe wird in v1 nicht als Standardfluss oder Freigabe-Gate umgesetzt. Diese Produktentscheidung wurde am 2026-05-28 in Vikunja `#16` dokumentiert.

## Bedrohungen (v1)

- Unautorisierte Zugriffe von Clients im LAN
- Fehlkonfiguration durch zu breite Exposition (z. B. WAN/oeffentliche Erreichbarkeit)
- Kompromittierte LAN-Clients mit legitimer Netznaehe

## Verbindliche Controls

- Netzgrenzen: primaer Docker/Host/Firewall, nur freigegebene Netze im LAN-Opt-in
- Authentisierung: keine Token-Pflicht fuer den aktuellen v1-LAN-Opt-in; Betreiber akzeptieren die lokale LAN-Trust-Boundary bewusst
- CORS: kein permissiver Default
- Discovery: keine automatische Service Discovery in v1

## Test-/Compliance-Checks

- `localhost-default`: mit Default-Config ist Zugriff auf `/mcp` nur lokal erfolgreich.
- `LAN-opt-in`: nur mit expliziter Non-Loopback-Bind/Publish-Konfiguration und dokumentierter Source-Netzgrenze.
- `Auth`: Doku und Konfiguration duerfen keine aktive Token-Absicherung fuer v1-LAN-Opt-in voraussetzen.
- `Out-of-scope`: keine Exposition ueber WAN/oeffentliche Interfaces, kein VPN/Overlay-Zugriff ohne neues Threat Model.

## Out-of-Scope (v1)

- WAN/Internet-Exposition
- VPN/Overlay-Access ohne separates Threat Model
- Offener Reverse-Proxy/Ingress ins Internet

## Restrisiken

- Fehlkonfiguration auf Host-/Firewall-Ebene
- Kompromittierte Clients im erlaubten LAN-Segment
- Unautorisierte Nutzung durch erreichbare LAN-Clients, weil keine Applikations-Token-Pflicht besteht
- Spaetere Web-, WAN-, VPN- oder Overlay-Szenarien koennen nicht auf diese v1-Risikoakzeptanz gestuetzt werden

## Folgearbeiten in Vikunja

- `#16 API Security Baseline (Token-first)` ist zurueckgestellt und kein v1-Freigabe-Gate
- `P1-003 MacOS/Linux Harmonisierung fuer Docker-Workflows`
- `P1-009 Observability-Baseline (Metriken, Logs, Health)`
