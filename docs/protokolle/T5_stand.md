# T5 Stand (Fernzugriff), Phase 1: Spike P-AUTH und ADR-010-Entwurf

Für die Prüfsitzung: Diese Datei ersetzt bei einem parallel laufenden Teil die Einträge in `docs/roadmap.md` und `CHANGELOG.md` (A03_teile.md, Rahmen 10). Beim Zusammenführen übertragen.

## Stand 27.10.2026

- **Ausgangsstand:** `main` `1c9847a`, Zweig `claude/charming-wozniak-qy1x1c`, eine Cloud-Sitzung.
- **Erledigt:** S3.0 Punkte 2 und 3 (A03 Teil C). Spike P-AUTH im Container: Prototyp `mcp --http` (zustandslos, `RequireBearerToken`, JWT über JWKS, Audience-Bindung, Principal-Zuordnung) mit Tests gegen einen Fake-AS; Messung von **Authelia 4.39.28** und **Dex v2.46.1** gegen die Prüfliste, jeweils Ende zu Ende mit dem OAuth-Client des MCP-SDK. Ergebnis: [S30_pauth.md](S30_pauth.md), Rohdaten [S30_pauth_rohdaten.txt](S30_pauth_rohdaten.txt). **Vorschlag: Authelia.** Entwurf [ADR-010_entwurf.md](ADR-010_entwurf.md).
- **Haltepunkt erreicht:** ADR-010 braucht das Ja des Betreibers. Phase 2 (S3.5, S3.6) erst danach und nach dem Merge von T2 (A03_teile.md, T5).
- **Kein Produktcode geändert**, keine Migration, kein Build (Phase 1 hat keinen; A03_teile.md, T5). `make check` und `make cross` grün; `make race`/`make integration` ohne Änderung am Hauptmodul nicht nötig (nur Doku und ein eigenes Spike-Modul). `make vuln` lief nicht: `vuln.go.dev` antwortet im Container mit 403 (Netzregel der Umgebung); bitte in der Prüfsitzung nachholen, auch für `spike/pauth` falls er übernommen wird.
- **Spike-Code:** `spike/pauth` (eigenes Go-Modul) im Commit `chore: add P-AUTH spike code (branch only)`. **Beim Zusammenführen weglassen** (Auftrag: kein Spike-Code nach `main`); die drei Doku-Commits sind davon unabhängig. Verweise aus `S30_pauth.md` auf `spike/pauth` zeigen dann auf diesen Zweig.

## Festlegungen (Einspruch möglich)

1. **Zwei Kandidaten statt vier:** gemessen Authelia und Dex (Reihenfolge laut A03). PocketID und Zitadel liefern Images nur über `ghcr.io`, das im Container keine Blobs ausliefert (403); Keycloak liegt auf `quay.io` (gesperrt) und ist laut Auftrag nur Rückfall. Da Authelia alle Pflichtpunkte erfüllt, nicht weiter verfolgt.
2. **Dex aus dem Quelltext** gebaut, als Docker Hub zeitweise 429 meldete; danach mit dem offiziellen Image `dexidp/dex:v2.46.1` wiederholt. Die Prüfliste nennt den Release-Stand.
3. **Principal = `sub`** (bei festem `iss`). E9 nennt „`sub` bzw. `client_id`“; `client_id` steht nur im Audit. Grund: Ein Mensch meldet sich an; der Client allein soll keinen Schlüssel bekommen. Für reine Maschinen-Clients (Client Credentials) wäre `sub` = Client; nicht Teil von S3.
4. **JOSE-Typ `at+jwt` ist Pflicht** (RFC 9068), weil sonst ein ID-Token mit passender `aud` als Access-Token durchginge. Authelia setzt ihn; Dex nicht.
5. **Host-Allowlist statt SDK-Schutz gegen DNS-Rebinding** (Befund B1). In A03 steht „Origin-Prüfung (SDK)“; das ist ein Bauhinweis, kein Entscheid, deshalb keine Stopp-Regel. Die Origin-Prüfung übernimmt `http.NewCrossOriginProtection`.
6. **Authelia-Client mit `requested_audience_mode: implicit`** (Befund B4), damit `aud` auch ohne `resource` stimmt.
7. **Spike-Code in einem eigenen Commit auf dem Zweig** statt ganz weg (Nachvollziehbarkeit; Muster `spike/s0`).

## Offen und neue Fragen (für Phase 2 und D8)

- `tailscale serve` und der `Host`-Kopf (B1): D8 Schritt 2.
- Claude Code mit Authelia Ende zu Ende, mit `--client-id` und `--callback-port`, Umleitung `http://localhost:<port>/callback` (B7): D8 Schritt 4. Ob Claude Code `resource` mitschickt, ist ungeprüft (durch Festlegung 6 unerheblich).
- OAuth aus einer Cloud-Sitzung (Risiko 3): laut Claude-Code-Doku nur mit interaktiver Sitzung; Weg bleibt `auth = "key"`.
- Dex-Refresh hängt (B6): Fehlerbericht an das Projekt möglich, für PostKauz ohne Folgen.
- D8 braucht Authelia im Tailnet: Benutzerdatei, Signaturschlüssel und Geheimnisse liegen beim Betreiber (Anleitung kommt mit S3.6 in `docs/betrieb.md`).

## Für die Roadmap (beim Merge übertragen)

- Stand-Zeile: „T5 Phase 1 erledigt (27.10.2026): Spike P-AUTH, Vorschlag Authelia, ADR-010 als Entwurf; wartet auf das Ja des Betreibers.“
- Offene Frage mit Default: „ADR-010 annehmen (Authelia als empfohlener AS, zuerst Modus `key`)? Default: ja.“

## Für das CHANGELOG („Unreleased“, beim Merge übertragen)

- `docs`: Spike P-AUTH (S3.0): Protokoll `docs/protokolle/S30_pauth.md` mit Prüfliste für Authelia 4.39.28 und Dex v2.46.1, Rohdaten, Entwurf ADR-010 (HTTP-MCP zustandslos, PostKauz nur Resource Server, externer AS, Host-Allowlist hinter Reverse-Proxy).

## Commits auf dem Zweig

1. `chore: add P-AUTH spike code (branch only)` — beim Merge weglassen.
2. `docs: record P-AUTH spike results`
3. `docs: draft ADR-010 for stateless HTTP-MCP`
4. `docs: add T5 phase 1 status`
