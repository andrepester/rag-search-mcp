# ADR: Reindex-first fuer Index-Schema-Evolution

| Feld | Inhalt |
|---|---|
| ID | RAG-SEARCH-MCP-ADR-2026-05-31 |
| Name | Reindex-first als legitimen Pfad fuer inkompatible Index- und Schemaaenderungen festlegen |
| Status | Angenommen |
| Fragestellung | Wie geht `rag-search-mcp` mit inkompatiblen Aenderungen an Index, Ingestion oder Query-Annahmen um: explizite Migration alter Index-Artefakte oder kompletter Reindex? |
| Kontext / Randbedingungen | `rag-search-mcp` ist ein kleines, privat betriebenes Docker-first-Projekt. Der Index wird aus gemounteten docs- und code-Quellen, Konfiguration, Chunking-Regeln, Embedding-Modell und Metadaten reproduzierbar aufgebaut. Persistierte Index-Artefakte sind Betriebsdaten, aber keine primaere Quelle der Wahrheit. Vikunja `#12` hat am 2026-05-31 entschieden, dass kein eigener Index-Schema-Migrationspfad umgesetzt wird. `#25` dokumentiert die daraus folgende Architekturentscheidung fuer Ingestion und Query. |
| Entscheidung & Begrundung | Inkompatible Aenderungen an Chunking, Metadaten, Embedding-Modell, Collection-/Source-Struktur oder Query-Annahmen werden durch einen vollstaendigen Reindex behandelt. Ein Reindex ist in diesem Projekt legitim und der gewuenschte Betriebsweg. Das reduziert Migrationskomplexitaet, vermeidet langlebige Kompatibilitaetslast fuer alte Index-Artefakte und passt zur vorhandenen Make-/Runtime-Oberflaeche (`make reindex`, `rag_reindex`, `make install`, `make doctor`). |
| Alternativen | 1) Explizite Schema-Versionen und Migrationen fuer bestehende Index-Artefakte: verworfen, weil Aufwand und Fehlerrisiko fuer den aktuellen privaten Betriebsumfang hoeher sind als der Nutzen. 2) Rueckwaertskompatibilitaet alter Indexe ohne Reindex garantieren: verworfen, weil Chunking, Embeddings und Metadaten eng an Code und Fixture-Stand gekoppelt sind. 3) Vollstaendiger Reset von Index und Modellen bei jeder Aenderung: verworfen, weil Modelle unabhaengig vom Index persistieren duerfen und nicht jede Aenderung Modell-Reset erfordert. |
| Datum / Dokumentation | 2026-05-31; README Troubleshooting; Vikunja-Backlog-Items: `#12`, `#25` |
| Akteure | Nutzer (Produktentscheidung Reindex legitim), OpenCode Assistant (Dokumentation) |

## Annahmen

- Die gemounteten docs- und code-Quellen bleiben die primaere Quelle der Wahrheit.
- Index-Artefakte koennen aus Quellen, Konfiguration und Embedding-Modell neu aufgebaut werden.
- Der aktuelle Betriebsumfang braucht keine stabilen Upgrade-Garantien fuer alte Index-Formate.
- `HOST_MODELS_DIR` und Modellpersistenz sind vom Index-Rebuild getrennt zu betrachten.

## Konsequenzen / Betriebsimplikationen

- Neue inkompatible Index- oder Query-Aenderungen muessen keinen Migrationscode fuer alte Index-Artefakte mitliefern.
- Doku, Tests und CI duerfen fuer inkompatible Aenderungen einen frisch erzeugten Index voraussetzen.
- Golden-Query- und Fixture-basierte Tests sollen den Index im Testpfad reproduzierbar neu erzeugen, statt alte Index-Versionen zu migrieren.
- Release- oder PR-Notizen muessen bei inkompatiblen Aenderungen klar auf den noetigen Reindex hinweisen.
- Backup-/Restore-Fragen fuer nicht regenerierbare Betriebsdaten sind nicht durch diese Entscheidung geloest.
- Wenn das Projekt spaeter stabile Upgrade-Garantien, Mehrnutzerbetrieb oder externe Releases mit Datenhaltungsversprechen braucht, muss diese Entscheidung neu bewertet werden.

## Validierung / Nachweis

- Nach inkompatiblen Aenderungen ist ein erfolgreicher `make reindex` bzw. `rag_reindex` der erwartete Nachweis fuer einen gueltigen Index.
- `make doctor` darf Reindex/Index-Verifikation als Betriebscheck nutzen, ohne alte Index-Artefakte migrieren zu muessen.
- Tests fuer Retrieval-Regressionen erzeugen ihren Fixture-Index frisch und dokumentieren Modell, Scope, Top-K, Chunking und Fixture-Stand.
- Doku und Backlog duerfen nicht mehr voraussetzen, dass ein expliziter Index-Migrationspfad fuer v1 existiert.
