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

# Phase 2: HTTP-MCP (S3.5) und OAuth 2.1 (S3.6)

## Stand 29.10.2026

- **Ausgangsstand:** Zweig `claude/determined-fermi-1g8w63`, Commit `9d2fc69` (T3 samt Nachbesserung, wird nach der Gegenprüfung zu `main`). Eine Cloud-Sitzung, Go 1.27.3 nach der Ausnahme in `CLAUDE.md` (der Container brachte 1.24.7 mit).
- **Merge während der Sitzung:** Auf denselben Zweig hat eine andere Sitzung die zweite Nachbesserung T3 gepusht (19 Commits bis `04fb1c1`). Zusammengeführt per Merge (kein Rebase, kein Force-Push); Konflikte nur in `CHANGELOG.md`, `docs/datenmodell.md`, `docs/roadmap.md` (beide Stände behalten; die Festlegungen von T5 heißen dort jetzt Nummer 64). Die Prüfläufe unten liefen nach dem Merge erneut.
- **T5 läuft nicht mehr parallel** (Merge-Reihenfolge T3 → T5): Diese Sitzung pflegt `docs/roadmap.md` und `CHANGELOG.md` selbst (Rahmen 10); diese Datei hält Einzelheiten, Festlegungen und Prüfläufe.
- **Erledigt:** S3.5 und S3.6 nach ADR-010, Punkt für Punkt:
  1. Transport zustandslos (`Stateless`, `JSONResponse`), nur `POST /mcp`, GET/DELETE 405, PRM unter `/.well-known/oauth-protected-resource[/<pfad>]`, `/healthz` 204 ohne Inhalt; Härtung: 1 MiB (413), Fristen 60 s (Lesen, Schreiben; Kopf 10 s), keine `Server`-Kopfzeile, `Cache-Control: no-store`, Rate-Begrenzung (429), `Content-Type` geprüft (415).
  2. Bindung: Standard `127.0.0.1:8787`; Host-Allowlist (421) statt SDK-Schutz (`DisableLocalhostProtection`); Origin-Prüfung `http.NewCrossOriginProtection` vor dem SDK-Handler.
  3. Nur Resource Server.
  4. Modus `key` (S3.5) und `oauth` (S3.6), in dieser Reihenfolge gebaut und committet.
  5. OAuth-Prüfung in der Reihenfolge des ADR; 401 mit `WWW-Authenticate: Bearer resource_metadata="…"` (bei einem falschen Token zusätzlich `error="invalid_token"`), fester Text; 403 ohne Hinweis bei unbekanntem `sub`.
  6. Authelia-Beispiel (`deploy/docker/auth/`) mit den Client-Einstellungen aus dem Spike; Test-AS daraus (`deploy/testauth/`).
  7. Vorregistrierter öffentlicher Client (`claude-code`, `--callback-port`).
  8. Privates Netz: `tailscale serve`, kein Funnel; Anleitung `docs/betrieb.md` 4.5.
  9. Executor und Audit: Akteur `agent`, `params.via` mit `transport`, `principal`, bei OAuth `iss`/`sub`; nie das Token; kein Passwort im HTTP-Prozess (`a.noCredentials`, Kindprozess-Test ohne Speicher der Zugangsdaten).
- **Dazu:** `doctor` zeigt `[http]` (S3.8-Punkt vorgezogen), systemd-Units, Docker-Image und Compose, `docs/betrieb.md` Abschnitt 4, README „Remote access (HTTP-MCP)“, Abnahme D8.
- **Keine Migration** (Schema 22). **Neue Abhängigkeit:** `github.com/coreos/go-oidc/v3` v3.21.0 (01.09.2026, Apache-2.0, per `go get`; neueste Version laut Modul-Proxy), bringt `github.com/go-jose/go-jose/v4` v4.1.4 (Apache-2.0) und hebt `golang.org/x/oauth2` auf v0.36.0. `golang.org/x/time` v0.15.0 (BSD-3-Clause, schon indirekt über das SDK) ist jetzt direkt (Rate-Begrenzung).

## Festlegungen (Einspruch möglich)

1. **`[http] auth` ohne Angabe = `key`.** Grund: läuft ohne Autorisierungsserver, nur auf Loopback/Tailnet; `oauth` verlangt ohnehin `resource`, `issuer` und Principals.
2. **Weg des Aufrufs in `params.via`** (`transport`, `principal`, `iss`, `sub`) statt neuer Spalte. Grund: keine Migration, `mail_audit_log` und Export zeigen es ohne Schemaänderung; ein Vertragstest sichert, dass keine Operation ein Eingabefeld `via` hat.
3. **Widerrufener oder abgelaufener Schlüssel bei der Anmeldung → 403** `{"status":"denied","code":"key_revoked"}` (bzw. `key_expired`), nicht 401. Grund: 401 löst bei OAuth-Clients eine neue Anmeldung aus, die am gesperrten Schlüssel nichts ändert; „denied“ wie über stdio. Innerhalb des Zwischenspeichers liefert ein Tool dieselbe Ablehnung über den Executor.
4. **Anmeldung mit Schlüssel 30 s zwischengespeichert** (Hash → Schlüssel). Grund: zustandslos heißt Anmeldung je Anfrage; ohne Zwischenspeicher schriebe jede Anfrage `last_used_at`. Widerruf und Ablauf prüft der Executor weiter vor jedem Aufruf (2 s).
5. **Rate-Begrenzung für alle Anfragen zusammen** (auch `/healthz` und abgewiesene), Vorrat = Wert je Minute. Grund: schützt Datenbank und Audit auch gegen Raten von Schlüsseln; ein Wert für den Betreiber.
6. **`/healthz` ohne Host-Prüfung** (Docker-Healthcheck, Dienstüberwachung auf `localhost`); liefert nichts außer 204.
7. **Erlaubte Hosts ohne Angabe:** Host aus `resource`; ohne eigene `resource` (Standard `http://<listen>/mcp`) zusätzlich `localhost`, `127.0.0.1`, `[::1]` mit dem Port. Vergleich ohne Groß-/Kleinschreibung und ohne `:443`/`:80`.
8. **`listen` nur als IP-Adresse oder `localhost`**, kein Hostname (Auflösung könnte eine andere Adresse liefern als geprüft). Modus `key` außerhalb von Loopback nur 100.64.0.0/10 und fd7a:115c:a1e0::/48; `0.0.0.0` nur im Modus `oauth` mit `allow_non_loopback`.
9. **Ungültige Token nicht ins Audit**, nur ins Log (fester Grund `invalid_token`); unbekanntes Subjekt (`principal_unknown`, mit `iss`/`sub`) und gesperrter Schlüssel (`key_revoked`/`key_expired`) schon. Grund: abgelaufene Token sind Alltag (5 min Lebensdauer); das Subjekt braucht der Betreiber für die Zuordnung. Im Modus `key` schreibt eine gescheiterte Anmeldung wie über stdio (`transport: http`).
10. **Discovery beim ersten Bedarf** (RFC 8414 mit Pfad des Ausstellers, sonst OpenID-Discovery; Aussteller muss genau passen), nicht beim Start. Grund: PostKauz startet auch, wenn der AS noch nicht läuft (Dienstreihenfolge); bis dahin 503 mit `Retry-After`, höchstens eine neue Discovery je 10 s.
11. **JOSE-Typ:** `at+jwt` und `application/at+jwt` (RFC 9068 erlaubt beide). **Verfahren:** RS256, PS256, ES256, EdDSA (nie `none`, nie HMAC).
12. **`scopes_supported`** = `[http] scopes`, Standard `openid offline_access` (passt zum Authelia-Client; `offline_access` für das Refresh-Token).
13. **Metadaten im Modus `key`** ohne `authorization_servers` (es gibt keinen AS); `resource`, `bearer_methods_supported`, `resource_name`.
14. **Ein MCP-Server je Anfrage**, kein Zwischenspeicher. Grund: einfach und ohne Zustand; `internal/mcp.New` ist billig (Tests unauffällig).
15. **Keine Windows-Aufgabe für `mcp --http`** vor dem Betriebsentscheid; D8 startet den Server im Skript, `docs/betrieb.md` 4.9 im Fenster.
16. **Docker:** Basis-Compose mit `network_mode: host` für `mcp-http` (Linux), damit `listen = "127.0.0.1:8787"` gilt und `tailscale serve` auf dem Host weiterreicht; Sidecar-Variante teilt das Netz des Tailscale-Containers. Image aus der Release-Programmdatei; die Alpine-Stufe liefert nur die CA-Zertifikate (kein Paket aus dem Netz), Wurzel schreibgeschützt mit `tmpfs /tmp` (SQLite braucht Temp-Dateien; gemessen: ohne `tmpfs` scheitert die Migration mit `disk I/O error`).
17. **D8 in vier Teilen** (A lokal, B PC im Tailnet, C Cloud-Sitzung, D OAuth am PC); `reader` hat seit T2 **12** Tools, A03 nennt 9. Teil D braucht Authelia auf Linux (VPS oder WSL2).
18. **Authelia-Beispiel:** Platzhalter `<auth-host>` (Cookie-Domäne ohne Port) und `<auth-url>` (= `[http] issuer`) getrennt; Signaturschlüssel über den Template-Filter (`X_AUTHELIA_CONFIG_FILTERS=template`) aus `jwks.pem`; `sub` ist bei Authelia eine zufällige Kennung je Benutzer, die Zuordnung geht über das Audit (`principal_unknown`).

## Prüfläufe

| Lauf | Ergebnis |
|---|---|
| `make check` | grün vor jedem Commit (gofmt, `go vet`, `go test ./...`, Guards, PII-Gate; ein Treffer des PII-Gates beim Instanznamen der systemd-Vorlage mit einem Kontonamen (sieht aus wie eine Adresse), mit dem Platzhalter `<konto>` behoben) |
| `make race` | grün, 37 Pakete ohne Datenrennen; `internal/cli` 2.061 s (34 min, lief zum Teil parallel zu `make integration`; Grenze 45 min, Hinweis für die Prüfsitzung), `internal/ops` 959 s, `internal/httpmcp` 53 s |
| `make integration` | `IT=dovecot,stalwart,authelia`: 12 Tests, 32 PASS (Dovecot 2.4.5, Stalwart 0.11.8, Authelia 4.39.28 mit `TestIntegrationAuthelia`), kein FAIL; **GreenMail übersprungen**: Docker Hub antwortete beim Laden von `greenmail/standalone:2.1.14` sechsmal mit 429 (Rahmen 7: Dovecot ist Pflicht und lief) |
| `make cross` | grün (linux/amd64 30,5 MB, windows/amd64 30,4 MB, darwin/arm64 28,7 MB ohne `-s -w`) |
| `make vuln` | nicht möglich: `vuln.go.dev` antwortet in der Cloud mit 403 (wie in T1a bis T3); bitte in der Prüfsitzung, neue Abhängigkeiten `go-oidc/v3` v3.21.0, `go-jose/v4` v4.1.4, `x/oauth2` v0.36.0 |
| `systemd-analyze verify` | beide Units ohne Meldung (mit einer Programmdatei unter `/usr/local/bin/postkauz`) |
| Docker | Image gebaut (49,7 MB, Nutzer 65532), Compose `mcp-http`: `/healthz` 204, `/mcp` ohne Bearer 401; ohne `tmpfs /tmp` scheiterte die Migration (behoben) |
| Probe D8 (Container) | Modus `key` gegen `mcp --http`: alle Prüfungen OK nach Korrektur der Toolzahl (12); Widerruf → 403. Modus `oauth` gegen Authelia 4.39.28: 20 OK (Anmeldung mit PKCE, ID-Token 401, Schlüssel 401, unbekanntes Subjekt 403 mit Hinweis, Sitzung, Audit mit `iss`/`sub`, Ablauf nach 340 s → 401, Refresh → 200), danach Widerruf → 403 |
| PowerShell | `abnahme_D8.ps1` nicht ausgeführt und nicht syntaxgeprüft (kein `pwsh`); Klammern gezählt (ausgeglichen), BOM und ASCII per `make guard` |
| PSScriptAnalyzer | nicht verfügbar (kein `pwsh`) |
| Mutationen | Ohne `at+jwt`-Prüfung bzw. ohne `aud`-Prüfung scheitern 5 Fälle von `TestOAuthTokenChecks` (geprüft, danach zurückgesetzt) |

## Offen und Fragen

- Abnahme D8 (Prüfsitzung): Tailscale (`tailscale serve` und der `Host`-Kopf, B1: durch die Allowlist abgedeckt), Cloud-Sitzung im Userspace-Modus (ob die Netzregel der Sitzung `tailscaled` erlaubt, Risiko 1), Claude Code mit Authelia (B4, B7).
- PowerShell: `abnahme_D8.ps1` ist nicht gelaufen und nicht syntaxgeprüft (kein `pwsh` im Container; die Installation aus dem Microsoft-Paketquelle hat der Rechte-Filter der Sitzung abgelehnt). Vor dem Lauf: `[System.Management.Automation.Language.Parser]::ParseFile(...)` und PSScriptAnalyzer.
- Fragen an den Betreiber: roadmap.md, Offene Fragen 6 und 7.

## Commits (Phase 2)

1. `feat: add the [http] configuration and the call origin in the audit`
2. `feat: serve MCP over stateless HTTP with API keys (S3.5)`
3. `feat: accept access tokens of an external authorization server (S3.6)`
4. `feat: add systemd units, a Docker setup and the VPS guide (S3.5)`
5. `test: give the test CA key usage so Python clients trust it`
6. `docs: add acceptance D8 for HTTP-MCP with key and OAuth`
7. `docs: record T5 phase 2 in roadmap, changelog, ADR-010 and protocol`
8. `docs: record the build 0.3.0-alpha.8` (Prüfsummen unten)
