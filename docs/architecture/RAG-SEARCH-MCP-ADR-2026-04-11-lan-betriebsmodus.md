# ADR: Betriebsmodus v1 (localhost-default, LAN-opt-in)

| Feld | Inhalt |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-04-11 |
| Name | Betriebsmodus v1 auf localhost-default mit optionalem LAN-only Opt-in festlegen |
| Status | Angenommen, aktualisiert am 2026-05-28 |
| Fragestellung | Welcher Betriebsmodus ist fuer v1 von `rag-search-mcp` verbindlich, sodass lokale Nutzung und optionaler LAN-Betrieb moeglich sind, ohne Clients ausserhalb des lokalen Netzwerks zuzulassen? |
| Kontext / Randbedingungen | Das Repo ist aktuell localhost-orientiert: `RAG_HTTP_HOST` defaultet auf `127.0.0.1` und Docker published Ports auf Loopback. Gleichzeitig besteht Anforderung, den Dienst im lokalen Netzwerk betreiben zu koennen. Der MCP-Endpunkt ist HTTP-basiert (`/mcp`), daher steigt bei LAN-Exposition das Risiko unautorisierter Zugriffe. Sicherheitsgrenzen muessen testbar und operational umsetzbar sein. |
| Entscheidung & Begrundung | v1 nutzt zwei Expositionsprofile: (1) Default `localhost-only`; (2) optional `LAN-only` als expliziter Opt-in fuer kontrollierte lokale Netze. WAN/Internet-Exposition und VPN/Overlay-Zugriff sind in v1 nicht unterstuetzt. LAN-only ist ueber Docker/Host/Firewall-Schicht bewusst zu begrenzen; eine Token-basierte API-Absicherung fuer Nicht-Loopback-Zugriffe ist nach Produktentscheidung vom 2026-05-28 kein v1-Standardfluss und kein Freigabe-Gate mehr. Diese Entscheidung erhaelt sichere Defaults, erlaubt den gewuenschten LAN-Betrieb ohne zusaetzliche Auth-Komplexitaet und macht die LAN-Risikoakzeptanz explizit. |
| Alternativen | 1) Strikt `localhost-only`: verworfen, da Nutzeranforderung LAN-Betrieb explizit fordert. 2) Standardmaessig `0.0.0.0`/LAN offen: verworfen wegen hoher Fehlkonfigurations- und Expositionsgefahr. 3) LAN inkl. VPN als gleichwertig in v1: verworfen, da Trust-Boundary unscharf wird und zusaetzliche Controls/Operationalisierung noetig waeren. |
| Datum / Dokumentation | 2026-04-11; aktualisiert 2026-05-28; Threat Model: `docs/architecture/THREAT_MODEL.md`; Vikunja-Backlog-Items: `P0-009`, `#16` |
| Akteure | Nutzer (Anforderung LAN-Betrieb), OpenCode Assistant (Ausarbeitung), Architektur-Review (`architect` Subagent) |

## Annahmen

- Betreiber kontrolliert Host-Netzwerk und Firewall-Regeln.
- LAN-Segmente sind nicht automatisch vertrauenswuerdig; der aktuelle v1-LAN-Opt-in akzeptiert dieses Restrisiko fuer kontrollierte lokale Netze.
- MCP-Zugriffe erfolgen ueber HTTP und koennen bei Fehlkonfiguration ueber den Host exponiert werden.

## Konsequenzen / Betriebsimplikationen

- Default-Installationen bleiben auf Loopback begrenzt.
- LAN-Betrieb ist ein bewusster Opt-in und erfordert zusaetzliche Betriebsaufgaben (Netzgrenze, Dokumentation, Risikoakzeptanz), aber keine Token-Auth im aktuellen v1-Standardfluss.
- WAN/VPN-Szenarien benoetigen ein separates, explizites Threat Model und sind fuer v1 ausgeschlossen.
- Ein spaeteres Webinterface, Public-Exposure oder breiteres Team-/Overlay-Szenario muss Auth-, CORS- und Deployment-Grenzen neu entscheiden.

## Validierung / Nachweis

- `localhost-only`: Service ist lokal erreichbar, nicht aus externem Netzsegment.
- `LAN-only`: nur explizite Non-Loopback-Bind/Publish-Konfiguration oeffnet Zugriff aus dem lokalen Netz.
- LAN-Opt-in: nur freigegebene Quellnetze koennen den Dienst erreichen; WAN/oeffentliche Exposition bleibt ausgeschlossen.
- Token-Auth: keine v1-Anforderung fuer kontrollierten LAN-Opt-in; Doku und Tests duerfen diese Absicherung nicht als vorhanden voraussetzen.
