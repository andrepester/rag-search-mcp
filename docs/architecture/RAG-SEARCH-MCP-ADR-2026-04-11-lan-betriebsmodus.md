# ADR: Betriebsmodus v1 (localhost-default, LAN-opt-in)

| Feld | Inhalt |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-04-11 |
| Name | Betriebsmodus v1 auf localhost-default mit optionalem LAN-only Opt-in festlegen |
| Status | Angenommen |
| Fragestellung | Welcher Betriebsmodus ist fuer v1 von `rag-search-mcp` verbindlich, sodass lokale Nutzung und optionaler LAN-Betrieb moeglich sind, ohne Clients ausserhalb des lokalen Netzwerks zuzulassen? |
| Kontext / Randbedingungen | Das Repo ist aktuell localhost-orientiert: `RAG_HTTP_HOST` defaultet auf `127.0.0.1` und Docker published Ports auf Loopback. Gleichzeitig besteht Anforderung, den Dienst im lokalen Netzwerk betreiben zu koennen. Der MCP-Endpunkt ist HTTP-basiert (`/mcp`), daher steigt bei LAN-Exposition das Risiko unautorisierter Zugriffe. Sicherheitsgrenzen muessen testbar und operational umsetzbar sein. |
| Entscheidung & Begrundung | v1 nutzt zwei Expositionsprofile: (1) Default `localhost-only`; (2) optional `LAN-only` als expliziter Opt-in. WAN/Internet-Exposition und VPN/Overlay-Zugriff sind in v1 nicht unterstuetzt. LAN-only ist nur zulaessig, wenn Mindestschutz fuer Nicht-Loopback aktiv ist (Token-basierte Authentisierung) und Netzgrenzen auf Docker/Host/Firewall-Schicht gesetzt sind. Bis zur technischen Umsetzung in `[[backlog/P1-004-api-security-token-baseline|P1-004]]` bleibt der effektiv freigegebene Betriebsmodus `localhost-only`. Diese Entscheidung erhaelt sichere Defaults, erlaubt den gewuenschten LAN-Betrieb kontrolliert und verhindert implizite Oeffnung nach aussen. |
| Alternativen | 1) Strikt `localhost-only`: verworfen, da Nutzeranforderung LAN-Betrieb explizit fordert. 2) Standardmaessig `0.0.0.0`/LAN offen: verworfen wegen hoher Fehlkonfigurations- und Expositionsgefahr. 3) LAN inkl. VPN als gleichwertig in v1: verworfen, da Trust-Boundary unscharf wird und zusaetzliche Controls/Operationalisierung noetig waeren. |
| Datum / Dokumentation | 2026-04-11; Threat Model: `docs/architecture/THREAT_MODEL.md`; Backlog-Item: `[[backlog/P0-009-threat-model-entscheidung|P0-009]]`; Folgearbeit: `[[backlog/P1-004-api-security-token-baseline|P1-004]]` |
| Akteure | Nutzer (Anforderung LAN-Betrieb), OpenCode Assistant (Ausarbeitung), Architektur-Review (`architect` Subagent) |

## Annahmen

- Betreiber kontrolliert Host-Netzwerk und Firewall-Regeln.
- LAN-Segmente sind nicht automatisch vertrauenswuerdig.
- MCP-Zugriffe erfolgen ueber HTTP und koennen bei Fehlkonfiguration ueber den Host exponiert werden.

## Konsequenzen / Betriebsimplikationen

- Default-Installationen bleiben auf Loopback begrenzt.
- LAN-Betrieb ist ein bewusster Opt-in und erfordert zusaetzliche Betriebsaufgaben (Token, Netzgrenze, Dokumentation).
- WAN/VPN-Szenarien benoetigen ein separates, explizites Threat Model und sind fuer v1 ausgeschlossen.

## Validierung / Nachweis

- `localhost-only`: Service ist lokal erreichbar, nicht aus externem Netzsegment.
- `LAN-only` (nach Umsetzung in `P1-004`): Nicht-Loopback ohne gueltiges Token wird abgewiesen.
- LAN-Opt-in: nur freigegebene Quellnetze koennen den Dienst erreichen; WAN/oeffentliche Exposition bleibt ausgeschlossen.
