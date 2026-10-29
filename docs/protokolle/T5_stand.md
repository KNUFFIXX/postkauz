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
- **Merge während der Sitzung:** Auf denselben Zweig hat eine andere Sitzung die zweite Nachbesserung T3 gepusht (19 Commits bis `04fb1c1`). Zusammengeführt per Merge (kein Rebase, kein Force-Push); Konflikte nur in `CHANGELOG.md`, `docs/datenmodell.md`, `docs/roadmap.md` (beide Stände behalten; die Festlegungen von T5 heißen dort jetzt Nummer 64; seit dem Merge der dritten Nachbesserung T3 Nummer 65). Die Prüfläufe unten liefen nach dem Merge erneut.
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

### Nach dem Merge mit der zweiten Nachbesserung T3 (`2210e76`)

| Lauf | Ergebnis |
|---|---|
| `make check` | grün (an `3bb7df5`; der zweite Merge `2210e76` brachte nur Doku und das Löschen von Wegwerf-Testdateien) |
| `make integration` | `IT=dovecot,stalwart,authelia`: 32 PASS, kein FAIL; GreenMail weiter übersprungen (429) |
| `make race` | grün, 37 Pakete ohne Datenrennen; `internal/cli` 2.065 s (34 min) auch allein, also nicht durch den parallelen Lauf: nur 11 min unter der Grenze von 45 min (`Makefile`, Ziel `race`); Hinweis an die Prüfsitzung, die Grenze im Auge zu behalten |
| `make cross` | grün (die drei Programme des Release-Baus) |

### Build 0.3.0-alpha.8

Gebaut an `2210e76` in einem frischen Klon ohne Tags (`git clone --no-tags --branch claude/determined-fermi-1g8w63`, sauberer Arbeitsbaum), Nachbau in einem zweiten Klon ohne Tags: `SHA256SUMS` gleich (ohne die Zeile `VERSION`), `go version -m` in beiden `mod github.com/KNUFFIXX/postkauz v0.0.0-20261029073539-2210e7603fa2`, `vcs.modified=false`. Go 1.27.3, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`.

```
f51c6d5649cfb9608d542847269e4949f0b3409e5e766d4c19a2fde2c42bc706  postkauz-linux-amd64
cbbe93d21a5bfa15fc7fa92f59197a8c07ff97e277e2c7a9a68976526752b23f  postkauz-windows-amd64.exe
196a0a959ec14b61021364b5e9cb6ee2d9261106c19928073d1dcd03a9becc7b  postkauz-darwin-arm64
26043f606d050fd1095f31019e1a1e3d3ce68bdde402ab4b2b5ce1fe17abcdef  Register-AuditExport.ps1
8a50ee74c064cc731446ed6f224d1d3ba8247d7abcea6cb9da834f2e40ec91c1  Register-SyncWatch.ps1
6973c8473a8a244172530c9e926d4d2b854b2bbee05bc013bde1c700bcdf3de3  Run-SyncWatch.ps1
9d40eb8aaef6b4c2cb68a8eea02651823ead6ac87c0a4917b29d9d3fc6a785e9  check-example.ps1
4bf639c3a3a5e3dd66c759ac21d8b1f0b1be6e7174bf67072bfcad01ecececc5  check-example.sh
c6b85f6389690ce64c0a68a370f494d4fe16ea417af5180a29faa65cdc3bee47  postkauz-sync@.service
233db530992511b2056540cac427fb73e0af5d17466bee9f36f00ec53eea093c  postkauz-mcp-http.service
```

Nur für die Abnahme D8, nicht für den Dauerbetrieb (Rahmen 5: Builds, die nicht von `main` stammen, nur in eigenen, frischen Datenverzeichnissen).

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
8. `chore: merge the second T3 follow-up into the T5 branch`, `chore: merge the alpha.7b record of the second T3 follow-up`
9. `docs: record the build 0.3.0-alpha.8` (Prüfsummen oben)

# Nachbesserung T5 Phase 2 (Gegenprüfung an `b6ff829`)

## Stand 29.10.2026

- **Auftrag:** [auftraege/A03_T5_nachbesserung.md](../../auftraege/A03_T5_nachbesserung.md) (Prüfsitzung DRACO-PK10: 2 BLOCKER, 6 WICHTIG, sonst KLEIN). Ausgangsstand Zweig `claude/determined-fermi-1g8w63`, Commit `7d9202c`. T3 (`claude/t3-final`) nicht angefasst, nicht gemergt. Keine Migration (Schema 22). Build **0.3.0-alpha.8b**.
- **Arbeitsweise:** Hauptsitzung plus drei Subagenten mit eigenen Worktrees (A: `internal/httpmcp`; B: `internal/ops`, `internal/config`, `internal/cli`, `internal/secrets`; C: Abnahme-Skripte, `deploy/`, Betriebsdoku). Übernommen per Fast-Forward bzw. Cherry-Pick (keine Merge-Commits); zwei Doku-Konflikte (`docs/sicherheit.md`, `docs/mcp.md`) von Hand zusammengeführt. Jeder Commit mit `make check` grün in seinem Worktree; am zusammengeführten Stand erneut.
- **Wegwerf-Tests** aus `auftraege/A03_T5nb_tests/` als Regressionstests mit Sollwert nach dem Fix übernommen (Dateien `t5n_*_test.go`, Tests `TestT5N…`); der Ordner ist im letzten Commit gelöscht.

## Erledigt, Punkt für Punkt

1. **OAuth-Discovery im Tailnet (BLOCKER).** Fehlt `Deps.HTTPClient`, baut `httpmcp` einen eigenen Client (`newASClient`: Klon von `http.DefaultTransport` mit eigenem `DialContext`, Verbindung 10 s, `Timeout` 10 s, keine Umleitungen) für Discovery **und** JWKS. **Begründung:** Die SSRF-Sperre des SDK ist hier aus, weil der Aussteller fest in der Konfiguration steht und nie aus einer Anfrage kommt; ein Proxy aus der Umgebung gilt weiter. Ein Aussteller als IP-Adresse scheitert mit eigener Meldung (schon beim Laden der Konfiguration, `doctor` zeigt es). Tests: `TestT5NDefaultASClient` (eigener `DialContext`, keine Umleitung), `TestT5NDiscoveryNoRedirect`, `TestT5NDiscoveryToTailnetIP`, `TestT5NDiscoveryToTailnetName` (nur mit `T5N_TAILNET_HOST`, hier übersprungen), `TestT5NIssuerNeedsDNSName`.
2. **D8 unter Windows (BLOCKER).** `Invoke-Http`: `$Body` ungetypt, Inhalt nur bei Methoden außer GET/HEAD und nur mit Inhalt; POST ohne Inhalt mit `ContentLength = 0`. Alle Aufrufer geprüft. Kein `pwsh` im Container: Parsen und Lauf auf dem Büro-PC (Prüfsitzung).
3. **JWKS ohne Frist, `http`-`jwks_uri` (WICHTIG).** `jwks_uri` muss `https` sein (außer Loopback). Eine Anmeldung hat höchstens 15 s. Ein eigener RoundTripper macht jeden Fehlschlag des JWKS-Abrufs (Frist, Status ≠ 200, Umleitung, kein JSON, über 1 MiB) zu `errJWKSFetch`; eine Hülle um den Schlüsselsatz erkennt ihn, die Antwort ist dann **503 mit `Retry-After`**, nicht 401 (go-oidc reicht den Fehler der Signatur nur als Text weiter, deshalb über den Kontext). Hängende JWKS → 503 nach etwa 10 s. Tests: `TestT5NDiscoverRejectsHTTPJWKS`, `TestT5NCheckJWKSURI`, `TestT5NJWKSHangAnswers503` (`defer close(block)` vor den Cleanups), `TestT5NJWKSErrorIs503`.
4. **Knotenname und Aussteller im D8-Protokoll (WICHTIG).** `Add-HostMask` maskiert Hostnamen und URLs wörtlich (ohne `GetFullPath`), für den Aussteller auch Host und Host:Port; `Hide-Private` zusätzlich alle `*.ts.net`, `fd7a:115c:a1e0:…` und 100.64.0.0/10.
5. **`tailscale serve` (WICHTIG).** Vorher `tailscale serve status --json`; belegtes oder unlesbares HTTPS 443 → Abbruch mit Hinweis, nichts geändert. Einschalten nur nach Zustimmung, am Ende von B und D Frage vor `tailscale serve --https=443 off`, bei Abbruch ohne zweite Frage aus. Nie `serve reset` (löschte auch andere Ports, etwa Authelia auf 8443). Syntax geprüft am Tailscale-Quelltext auf GitHub (`cmd/tailscale/cli/serve_v2.go`, `serve_legacy.go`, `ipn/serve.go`; tailscale.com war aus der Sitzung nicht erreichbar).
6. **HTTP-Dienst und IMAP-Passwörter (WICHTIG).** Umgesetzt mit Abweichung vom Vorschlag „zwei Systemnutzer“ (Festlegung 26): PostKauz setzt Datenbank, `-wal`, `-shm`, Blobs und Log bei jedem Start auf „nur der Eigentümer“ (`store.Open`, `blob`, `config.RestrictPermissions`); zwei Nutzer mit gemeinsamer Gruppe bräuchten ein neues Rechtemodell. Stattdessen:
   - MCP-Prozesse (stdio und `--http`) bekommen den Secrets-Speicher `secrets.Denied` (jeder Zugriff `ErrDenied`; Schlüsselbund, `insecure-secrets` und `POSTKAUZ_PASSWORD_*` werden nie angefasst; kein MCP-Tool braucht Zugangsdaten, geprüft an `internal/mcp/tools.go`). Tests: `TestT5NDeniedRefusesEverything`, `TestT5NNoCredentialsGetsDeniedSecrets`.
   - Jeder Prozess mit Zugangsdaten setzt unter Linux `PR_SET_DUMPABLE=0` (neues Paket `internal/procguard`, nur `syscall`): `/proc/<pid>/environ`, `/proc/<pid>/mem` und ptrace nur noch für root, kein Core-Dump; wirkt mit jeder systemd-Version. Test: `TestT5NDenyInspection` (Kindprozess).
   - Units: Passwortdateien unter `/etc/postkauz/credentials/` (Ordner 0700 root, Dateien `0600 root:root`; systemd liest `EnvironmentFile=` als root), MCP-Unit mit `InaccessiblePaths=-/etc/postkauz/credentials`, `PrivatePIDs=yes` (ab systemd 257), `ProtectProc=invisible` in beiden. `systemd-analyze verify` (systemd 255): beide ohne Fehler, nur „Unknown key name 'PrivatePIDs'“ (erwartet).
   - Windows offen genannt (`betrieb.md` 4.9): Sync und HTTP unter demselben Benutzer, Schutz nur im Code.
   - `TestMCPHTTPChildProcess` läuft unter Windows mit `Kill`; nur die Prüfung des geordneten Endes entfällt dort (hier nur übersetzt, `GOOS=windows go vet` sauber).
7. **`betrieb.md` und die Live-Datenbank (WICHTIG).** Warnung am Anfang von 4.2 und 4.9 (nur mit einem Release von `main`; Testbauten nur mit eigenem `--data-dir`); 4.2 nutzt einen eigenen Testordner und räumt am Ende auf; „9 Tools“ → 12.
8. **Cloud-Teil D8 und der Repo-Klon (WICHTIG).** `D8_WORK` = `mktemp -d` (im Klon → Abbruch), `/d8-cloud-*/` in `.gitignore`, Rohausgaben am Ende gelöscht; vom Status nur Knotenzahl und „PostKauz-Knoten gesehen“; Auth-Key in einer 0600-Datei, `tailscale up --auth-key=file:<pfad>` (im Quelltext `cmd/tailscale/cli/up.go` ab v1.30.0 vorhanden), Variable vor jedem Kindprozess gelöscht; Anmelde-URL nur nach `/dev/tty` und in eine 0600-Datei (`--login-url-file`); Weiter-Signal über Dateien (`$D8_WORK/go`, `--paste-file`), Wartezeit 1800 s. Trockenlauf unter `dash` mit eigenem Stub-`tailscale`.
9. **Kleineres:**
   - (a) Rate-Begrenzung neu (Festlegung 19 ersetzt Festlegung 5). Tests: `TestT5NRateNoLockoutByUnauthenticated`, `TestT5NRateWrongKeyAuditRows` (höchstens 32 `key_invalid`-Zeilen bei 130 falschen Schlüsseln, danach 429, der gültige Schlüssel 200), `TestT5NRateOtherUnauthenticated`, `TestT5NRatePerPrincipal`, `TestT5NRateOAuthPrincipalUnknown`.
   - (b) Discovery läuft im Hintergrund mit `context.WithTimeout(context.WithoutCancel(ctx), 10 s)`; nur ein echter Fehlschlag setzt `failedAt`; Wartende nutzen ihre eigene Frist. Tests: `TestT5NDiscoveryCancelledByClient`, `TestT5NDiscoveryCancelledEndToEnd`, `TestT5NDiscoveryWaiterKeepsItsDeadline`.
   - (c) JWKS höchstens einmal je 10 s; `iss`/`exp` vorab aus dem ungeprüften Inhalt. Tests: `TestT5NJWKSRefetchPerBadToken` (5 schlechte Token: 0 Abrufe), `TestT5NJWKSRefetchThrottled`, `TestT5NJWKSRotationWithin10s`, `TestT5NPrecheckClaims`.
   - (d) `nbf` und `iat` selbst mit 30 s; Tests für `alg: none`, HS256 mit dem öffentlichen Schlüssel, fremden Aussteller in den Metadaten, `http`-`jwks_uri` (`TestT5NTokenGaps`, `TestT5NDiscoverIssuerMismatch`).
   - (e) `withVia`/`withCounts`: `null` ergibt ein leeres Objekt (`paramsObject`), Kommentare an der richtigen Funktion. Tests: `TestT5NWithViaEdge`, `TestT5NWithCountsEdge`, `TestT5NRedactAlwaysObject`.
   - (f) `abnahme_D8.ps1`: `-LivePath` Pflicht (Datei-Eintrag sperrt auch den Ordner), Teile A, B, D in `try/catch/finally` mit `Clear-Part` (serve aus, `mcp --http` beenden, Schlüssel widerrufen, Schlüssel- und Refresh-Dateien löschen), D7-Reste entfernt, `-ExpectVersion` `0.3.0-alpha.8b` (Vergleich so, dass `alpha.8` nicht auf `alpha.8b` passt).
   - (g) Gescheiterte Anmeldung mit Schlüssel schreibt `{"via":{"transport":"stdio|http"}}`. Tests: `TestT5NAuthenticateKeyAuditedWritesVia`, `TestMCPRequiresValidKey`.
   - (h) Fehler in `[http]` stehen in `Config.HTTPErr`; nur `mcp --http` (Exit 1) und `doctor` (Zeile `INVALID`, Exit 1) melden sie. Tests: `TestT5NHTTPErrorDoesNotStopLoad`, `TestT5NHTTPConfigErrorOnlyStopsMCPHTTP`.
   - (i) `logPath` nennt nur `/mcp`, `/healthz` und die beiden Pfade der Metadaten wörtlich, sonst `other`. Test: `TestT5NLogPath`.
   - (j) Doku: Kopfzeile `'Authorization: Bearer ${POSTKAUZ_KEY}'` in einfachen Anführungszeichen (Claude Code setzt `${VAR}` in `headers` selbst ein, geprüft an code.claude.com/docs/en/mcp, „Environment variable expansion“); Schlüssel ohne Befehlsverlauf (`Read-Host`); `env $(cat …)` → `systemd-run --pipe --wait --uid=postkauz -p EnvironmentFile=…`; Compose-Verweis auf `betrieb.md` 4.3; `docs/mcp.md` „Abschnitt 9“ → 4; Kommentar zu B3 in `configuration.example.yml`; Authelia `two_factor` empfohlen; Images `alpine:3.22.7@sha256:2e0b81d6…` und `tailscale/tailscale:v1.104.4@sha256:ed6480b3…` (Digests per `docker pull` geprüft); Satz zu 100.64.0.0/10 (CGNAT).
   - (k) `deploy/testauth/setup.sh` (Codex: BLOCKER): `POSTKAUZ_TESTAUTH_DIR` statt `WORK`; nur absolute Pfade, mindestens zwei Ebenen tief (nie `/`, `/tmp`, `/root`), ohne `.`/`..`/`//`, nicht `$HOME`, nicht im Repository, kein Symlink; ein vorhandener Ordner wird nur gelöscht, wenn er die eigene Marke `.postkauz-testauth` trägt und Ordner und Marke dem eigenen Nutzer gehören (Marke keine Verknüpfung); ein fremder, nicht leerer Ordner bricht ab. Geprüft mit dem herausgezogenen Prüfblock (ohne Löschbefehl) für 12 Pfade und mit `make testauth-up` beim Integrationslauf.
   - (l) **Log-Flut** (Nachtrag der Prüfsitzung aus dem Codex-Lauf): `outer` schrieb je Anfrage eine Zeile, auch für abgewiesene; `postkauz.log` hat keine Größengrenze. Jetzt: angemeldete Anfragen einzeln (`http request`), alles andere (401, 403, 404, 405, 413, 415, 421, 429, 503 und `/healthz`) höchstens eine Zeile je 10 s (`http requests rejected` mit Anzahl und Zählern je Status und festem Grund, `internal/httpmcp/rejectlog.go`); die bisherige Warnung je gescheiterter Anmeldung geht in dieselbe Zeile; ausstehende Zähler schreibt die nächste Anfrage nach 10 s oder das Ende von `Serve`. Tests: `TestT5NRejectedRequestsDoNotFloodTheLog` (1.000 abgewiesene Anfragen gemischter Art: keine Einzelzeile, höchstens 2 Sammelzeilen; nach 10 s zählen alle Sammelzeilen zusammen 1.000, die angenommene Anfrage steht einzeln da), `TestT5NRejectLogFlush`; `TestT5NLogPath` angepasst (unangemeldete Anfragen ohne Pfad im Log).
10. **Kein Handlungsbedarf** (laut Auftrag, als Test festgehalten: `TestT5NListenEdgeCases`): `localhost` in `listen` erlaubt (Festlegung 8; `LOCALHOST` lehnt `CheckHTTP` ab), `[::ffff:127.0.0.1]` lauscht auf 127.0.0.1, `[::ffff:100.64.1.1]` gilt als Tailnet-Adresse, Zonen (`[fe80::1%eth0]`) lehnt `CheckHTTP` ab, `0.0.0.0`/`::` nur im Modus `oauth`. `consent_mode: implicit` gegen „Zustimmen klicken“ (Befund B3: bei `offline_access` greift implicit nicht). Audit-ID im Log der Tool-Aufrufe vorhanden (`internal/mcp/server.go`).

## Festlegungen (Einspruch möglich)

19. **Ersetzt Festlegung 5 (Rate-Begrenzung):** `/healthz` außerhalb jeder Grenze. Alle Anfragen ohne gültige Anmeldung (fehlendes oder falsches Token, Metadaten, unbekannte Pfade, 405/415 an `/mcp`, fremder Host) teilen sich einen festen Vorrat von 30 je Minute (ebenso viele auf einmal); leer → 429. Nach der Anmeldung hat jeder Schlüssel einen eigenen Vorrat von `[http] max_requests_per_minute`. Eine gescheiterte Anmeldung schreibt ihre Audit-Zeile nur, solange der Vorrat Platz hat. Ein gültiger Schlüssel kommt immer durch. Restrisiko: Bei leerem Vorrat prüft der Modus `key` falsche Schlüssel weiter gegen die Datenbank (nur lesen). *Berichtigt in der zweiten Nachbesserung (Punkt 1):* Vor der Anmeldung wird der Vorrat nur angesehen; gezogen wird erst nach einer gescheiterten Anmeldung. Ein gültiger Schlüssel (oder ein gültiges Token) zieht **nie** aus dem Vorrat; vorher tat er es doch (die Reservierung wurde nicht zurückgegeben), und gültiger Verkehr sperrte Unangemeldete aus. Ein 503 vor geglückter Prüfung des Tokens (unbekannte `kid`, JWKS oder Discovery nicht erreichbar, im Modus `key` auch eine nicht lesbare Datenbank) zählt wie ein Fehlschlag; nur ein 503 nach geprüftem Token zieht nicht.
20. **Aussteller nur als DNS-Name** (IP nur auf Loopback): `tailscale serve` stellt Zertifikate nur für MagicDNS-Namen aus, das SDK lehnt private Adressen in der URL ohnehin ab. Geprüft beim Laden (`doctor`) und bei der Discovery.
21. **Eigener Client für den AS:** Verbindung und Antwort je 10 s, keine Umleitungen, ohne SSRF-Sperre des SDK (Begründung Punkt 1). *Ergänzt in der zweiten Nachbesserung (Punkt 5d):* `jwks_uri` darf jeder `https`-Host sein, nicht nur der des Ausstellers, weil sie nur der fest konfigurierte Aussteller in seinen Metadaten nennt; wer diese Metadaten fälschen kann, kann ohnehin selbst Token signieren.
22. **Hängende oder kaputte JWKS → 503 mit `Retry-After: 10`** statt 401 (ein 401 löste beim Client eine neue Anmeldung aus, am Token liegt es nicht). Anmeldung höchstens 15 s.
23. **JWKS höchstens alle 10 s neu laden.** Eine neue `kid` gilt spätestens 10 s nach dem letzten Abruf, bis dahin 503; eine falsche Signatur mit bekannter `kid` bleibt 401.
24. **Vorprüfung von `iss`, `exp`, `nbf`, `iat`** vor der Signatur, nur als Ablehnung; `nbf` und `iat` je 30 s, beide freiwillig. Ergänzt Festlegung 10: Die Discovery läuft weiter, auch wenn der auslösende Client trennt.
25. **Gescheiterte Anmeldung mit Schlüssel** schreibt `{"via":{"transport":"stdio|http"}}` (auch für stdio, einheitlich); alte Zeilen bleiben in der alten Form. Ergänzt Festlegung 9.
26. **VPS: beide Dienste als Nutzer `postkauz`** statt zweier Systemnutzer (Auftrag Punkt 6). Grund: Rechtemodell von Datenbank und Blobs (nur Eigentümer). Schutz statt dessen über `secrets.Denied` im MCP-Prozess, `PR_SET_DUMPABLE=0` in Prozessen mit Zugangsdaten, Passwortdateien `0600 root:root` außer Sicht der MCP-Unit, `PrivatePIDs=` ab systemd 257. Zwei Nutzer erst mit einem Rechtemodell für eine gemeinsame Gruppe (Roadmap). *Ergänzt in der zweiten Nachbesserung (Punkt 4):* Das Passwort kommt als systemd-Credential statt über `EnvironmentFile=` (Festlegung 33), der Schutz `PR_SET_DUMPABLE=0` als erste Anweisung von `main` für jedes Kommando; die Sätze „gleich beim Start“ und „`/proc` nur mit eigenen Prozessen“ in `betrieb.md` 4.4 sind berichtigt. Restrisiko offen genannt: Ein übernommener HTTP-Dienst kann über die gemeinsame Datenbank `accounts.host` ändern.
27. **Fehler in `[http]`** (ungültiger Wert oder unbekannter Schlüssel unter `http.`) halten nur `mcp --http` auf, auch wenn `--listen` den Wert ersetzen würde; `doctor` zeigt `INVALID` und endet mit Exit 1. Syntaxfehler der Datei und falsche Typen halten weiter jedes Kommando auf (Datei nicht lesbar). *Geändert in der zweiten Nachbesserung (Punkt 5a):* Auch ein falscher Typ unter `[http]` (etwa `max_requests_per_minute = "60"`) hält nur `mcp --http` auf; global bleiben Syntaxfehler der Datei und falsche Typen außerhalb von `[http]`.
28. **D8:** Bei Abbruch schaltet das Skript `tailscale serve` ohne zweite Frage aus (die Frage vor dem Einschalten sagt es an); belegtes oder unlesbares 443 bricht ab (Exit 2); sagt der Betreiber am Ende von B „nein“, nutzt D die Einstellung weiter und fragt am Ende von D erneut. Teil C wartet auf Dateien (`go`, `callback`) statt auf eine Eingabe; die Anmelde-URL steht zusätzlich in `login-url.txt` (0600), weil eine Cloud-Sitzung kein `/dev/tty` hat.
29. **Images:** Tailscale fest `v1.104.4`, Alpine `3.22.7` (Linie 3.22 beibehalten), je mit Digest.
30. **Log abgewiesener Anfragen:** Sammelzeile höchstens alle 10 s, die erste nach einer ruhigen Zeit sofort; `/healthz` zählt mit (sonst ohne jede Grenze); ein angemeldeter Schlüssel mit 429 gilt als abgewiesen. Eine Größengrenze oder Rotation für `postkauz.log` selbst ist nicht Teil dieser Nachbesserung (Roadmap).

## In die Roadmap (nicht gebaut)

- Prüfen, dass die `listen`-Adresse auf einem Tailscale-Interface liegt.
- Unter Windows Sync und HTTP-MCP unter getrennten Benutzern.
- Auf dem VPS zwei Systemnutzer mit gemeinsamer Gruppe (braucht ein Rechtemodell für Datenbank, WAL und Blobs, Festlegung 26).
- Größengrenze oder Rotation für `postkauz.log` (auch der Dauer-Sync schreibt dorthin).

## Prüfläufe (Nachbesserung)

| Lauf | Ergebnis |
|---|---|
| `make check` | grün vor jedem Commit (in den Worktrees der Subagenten und am zusammengeführten Stand) |
| `make race` | grün an `269fe18`, 38 Pakete ohne Datenrennen, 25:59 min gesamt; `internal/cli` **1.547 s (25:47 min, Grenze 45 min)**, `internal/ops` 761 s, `internal/httpmcp` 160 s, `internal/procguard` 2 s |
| `make integration` | `IT=dovecot,stalwart,greenmail,authelia` an `269fe18`: 42 PASS, 0 FAIL, 0 SKIP (Images über `mirror.gcr.io` geholt und umgetaggt; Authelia frisch über das gehärtete `make testauth-up`). Ein Lauf vor 9 (l) an `ed44dcc`: 41 PASS, 1 SKIP (`TestIntegrationMailboxActions/greenmail`: „no mailbox with role trash“ im damaligen Zustand des GreenMail-Containers) |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make vuln` | nicht möglich: `vuln.go.dev` antwortet in der Cloud mit 403 (wie bisher); bitte in der Prüfsitzung. Keine neue Abhängigkeit (`internal/procguard` nutzt nur `syscall`) |
| `systemd-analyze verify` | beide Units ohne Fehler (systemd 255, Programmdatei als Attrappe unter `/usr/local/bin/postkauz`, danach entfernt); einziger Hinweis „Unknown key name 'PrivatePIDs'“ (erst ab systemd 257, erwartet) |
| `GOOS=windows go vet ./...` | sauber (u. a. `TestMCPHTTPChildProcess` mit `Kill` unter Windows) |
| PowerShell | `abnahme_D8.ps1` nicht geparst und nicht gelaufen (kein `pwsh` im Container); geprüft: ASCII/BOM-Guard, Klammern, Diff Zeile für Zeile. Parsen in PS 5.1 und 7.6 und Lauf: Prüfsitzung auf dem Büro-PC |
| Cloud-Skript D8 | Trockenlauf unter `dash` mit Stub-`tailscale`/`tailscaled` (Subagent C): Auth-Key nur über die 0600-Datei, nicht in argv und nicht in der Umgebung von `tailscaled`; Protokoll ohne Namen und Login |

### Build 0.3.0-alpha.8b

Gebaut an `269fe18` (enthält den ganzen Code; danach nur Protokoll und das Löschen der Wegwerf-Tests) in einem frischen Klon ohne Tags (`git clone --no-tags --branch claude/determined-fermi-1g8w63`, sauberer Arbeitsbaum), Nachbau in einem zweiten Klon ohne Tags: `SHA256SUMS` gleich (ohne die Zeile `VERSION`), `go version -m` in beiden `mod github.com/KNUFFIXX/postkauz v0.0.0-20261029103511-269fe1854e83`, `vcs.revision=269fe1854e83ee97cd705ce87ab4fc3fbd548a10`, `vcs.modified=false`. Go 1.27.3, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`. `postkauz version` → `postkauz 0.3.0-alpha.8b (go1.27.3 linux/amd64)`.

```
7750a612604ddbeec3738cb679f55b0cb6fa2e0b6e486e3964e89d1835495c23  postkauz-linux-amd64
c1c4eba1bb1223467fd1d4e69db4d7fbcb0aa75892bf4ee30e3cff3a2773d856  postkauz-windows-amd64.exe
eb537353e6d881dd1b1c922b27f6b2eb59a0f44511e12be159bedb01c2a8235f  postkauz-darwin-arm64
26043f606d050fd1095f31019e1a1e3d3ce68bdde402ab4b2b5ce1fe17abcdef  Register-AuditExport.ps1
8a50ee74c064cc731446ed6f224d1d3ba8247d7abcea6cb9da834f2e40ec91c1  Register-SyncWatch.ps1
6973c8473a8a244172530c9e926d4d2b854b2bbee05bc013bde1c700bcdf3de3  Run-SyncWatch.ps1
9d40eb8aaef6b4c2cb68a8eea02651823ead6ac87c0a4917b29d9d3fc6a785e9  check-example.ps1
4bf639c3a3a5e3dd66c759ac21d8b1f0b1be6e7174bf67072bfcad01ecececc5  check-example.sh
72e69e32dd6f24f3e696d31aa0d4c932777a5aef2e6b9c3dbde00ad7fec41920  postkauz-sync@.service
989ba16168765dac1a3f86f140e50d2b245e701ca8ff100e300d7ec41746368b  postkauz-mcp-http.service
```

Nur für die Abnahme D8, nicht für den Dauerbetrieb (Rahmen 5: Builds, die nicht von `main` stammen, nur in eigenen, frischen Datenverzeichnissen).

## Offen und Fragen (Nachbesserung)

- Prüfsitzung: `abnahme_D8.ps1` in PS 5.1/7.6 parsen und Teil A/B laufen lassen; `make vuln`; Teil C aus einer Cloud-Sitzung (neuer Ablauf mit Datei `$D8_WORK/go`).
- Nach dem ersten Lauf mit dem gehärteten `setup.sh`: Ein alter Ordner `/tmp/postkauz-testauth` ohne Marke bricht den Lauf ab; dann einmal von Hand löschen.
- Fragen an den Betreiber: roadmap.md, Offene Fragen 6 bis 8.

## Commits (Nachbesserung)

`e8b9039` … `2dcaffc` (Subagent B: `withVia`, Secrets-Speicher `denied`, Kindprozess-Test unter Windows, `params.via`, `[http]`-Fehler), `a5eda7e` … `9288367` (Subagent A: Discovery im Tailnet, JWKS, Discovery-Abbruch, Neuladen, `nbf`/`iat`, Rate-Begrenzung, `logPath`, `listen`-Grenzfälle), `1472200` (Aussteller als IP), `242ab05` … `ef486c9` (Subagent C: D8-Skripte, Units, Betriebsdoku, Images, `setup.sh`), `1800590` (`procguard`), `ed44dcc` (ADR-010 Nachtrag, Toolzahlen), `1e60c9e` (Protokoll), `fc2d484` (9 l Log-Flut), `8b5dd1a` (9 k Härtung), `269fe18` (Protokoll 9 l/9 k); danach dieser Eintrag und das Löschen von `auftraege/A03_T5nb_tests/`.

# Zweite Nachbesserung T5 Phase 2 (Gegenprüfung an `8b4566e`)

## Stand 29.10.2026

- **Auftrag:** [auftraege/A03_T5_nachbesserung2.md](../../auftraege/A03_T5_nachbesserung2.md) (Prüfsitzung DRACO-PK10: kein BLOCKER, vier WICHTIG, sonst KLEIN; letzte Nachbesserungsrunde für T5). Ausgangsstand Zweig `claude/determined-fermi-1g8w63`, Commit `ca77f4b`. Keine Migration (Schema 22). Build **0.3.0-alpha.8c**.
- **Schritt 0:** `origin/main` (`1e3ae3c`, T3 mit drei Nachbesserungen, 0.3.0-alpha.7c) per Merge hereingeholt (`e5c24d1`, kein Rebase, kein Force-Push). Konflikte nur in `CHANGELOG.md` und `docs/roadmap.md`, beide Stände behalten; die Festlegungen der dritten Nachbesserung T3 sind Entschiedene Frage 64, die von T5 jetzt 65 und 66 (vorher 64 und 65). `make check` danach grün, gepusht vor den Punkten.
- **Arbeitsweise:** Hauptsitzung (Punkte 1, 4, 5a, 5b, 5d) plus ein Subagent mit eigenem Worktree für die Abnahme-Skripte (Punkte 2, 3, 5c, 5e); übernommen per Cherry-Pick ohne Konflikt. `make check` je Commit in einem eigenen Prüf-Worktree, damit die Arbeit weiterlaufen konnte.
- **Wegwerf-Tests** aus `auftraege/A03_T5nb2_tests/` als Regressionstests mit Sollwert nach dem Fix übernommen (`t5n2_*_test.go`, Tests `TestT5N2…`; Python `docs/abnahme_D8_probe_t5n2_test.py`); der Ordner ist im letzten Commit gelöscht. Jeder neue Test ist am alten Stand rot und am neuen grün, außer `TestT5N2RefetchWithClock` (schärft einen Test, die Vorprüfung war schon da).

## Erledigt, Punkt für Punkt

1. **Gültige Aufrufe leerten den Vorrat für Unangemeldete (WICHTIG).** `mcpGate` sieht den Vorrat vor der Anmeldung nur an (`Tokens() >= 1`, davon hängt nur ab, ob eine gescheiterte Anmeldung ins Audit schreibt) und zieht erst nach einem Fehlschlag (`Allow()`, sonst 429). Ein 503 vor geglückter Prüfung des Tokens zählt wie ein Fehlschlag; nur ein 503 nach geprüftem Token (Datenbank nicht lesbar, neuer Fehler `errUnavailableVerified`) zieht nicht. Kommentar an `failPerMinute` und Festlegung 19 berichtigt, `docs/mcp.md` ergänzt. Tests (`t5n2_rate_test.go`): `TestT5N2ValidCallsDoNotDrainFailurePool` (35 gültige Aufrufe, danach Metadaten 200, ohne Token 401 mit `resource_metadata`, falscher Schlüssel 401 mit einer `key_invalid`-Zeile), `TestT5N2SustainedValidTraffic` (ein gültiger Aufruf je Sekunde, Metadaten 8× 200; mit `-short` übersprungen), `TestT5N2OAuthExpiredAfterValidTraffic` (abgelaufen → 401 `invalid_token`, unbekanntes Subjekt → 403 mit Audit-Zeile), `TestT5N2UnknownKidThrottled` (60 Token mit unbekannter `kid`: höchstens 30× 503, dann 429; danach gültiges Token 200). Am alten Stand: 429 statt 200/401/403, 0 Audit-Zeilen, 60× 503. Den Fall „GET mit gültigem Bearer“ aus der Prüfung nicht übernommen (Roadmap laut Auftrag).
2. **Token-Präfix unmaskiert im D8-Protokoll (WICHTIG).** Probe: `Resp.show()` maskiert vor dem Kürzen; ebenso die Details von `mail_search` und der AS-Metadaten. `abnahme_D8.ps1`: stderr der Probe und A5.1 kürzten vor `Hide-Private`, berichtigt; `Limit-Text` mit Kommentar zur Reihenfolge. `abnahme_D8_cloud.sh` war schon richtig (`mask | head`). Test: Klasse `TestT5N2MaskBeforeCut` (3 Tests, gegen die alte Probe 6 Fehlschläge).
3. **Abbruch der Probe zählt als Fehler, Aufräumen bei Abbruch (WICHTIG).** Probe: argparse-Fehler und jede Ausnahme außer Strg+C (auch `SystemExit`) ergeben eine FAIL-Zeile `<Nr>.x`, die Schlusszeile und Exit 1; `--help` bleibt Exit 0. `abnahme_D8.ps1`: `Test-ProbeEnd` verlangt die Schlusszeile passend zu den gezählten Zeilen **und** den passenden Exit-Code, sonst `FAIL <Nr>.ende` (in `Invoke-Probe` und `Invoke-ProbeInteractive`, dort über `$LASTEXITCODE`). `Start-McpHttp` trägt den Prozess gleich nach dem Start in `$script:Srv` ein; `$script:ServeOn = $true` steht vor `tailscale serve --bg`; `Disable-Serve` beim Aufräumen ohne Frage verlangt nur ein freies 443 danach (ein `off` für einen nie eingeschalteten Serve steht nur im Detail). `abnahme_D8_cloud.sh` (`#!/bin/sh`, auch unter `dash`): Exit-Status der Probe über die Statusdatei `probe.rc` (kein `pipefail`/`PIPESTATUS` in POSIX-sh), dieselbe Prüfung mit `FAIL <Nr>.ende`. Tests: Klasse `TestT5N2AbortCountsAsFail` (6 Tests); Trockenlauf des Cloud-Skripts mit Stub-Programmen unter dash, sh und bash (gute Probe 7 OK; Syntaxfehler, falscher Exit-Code, fehlende Schlusszeile je `FAIL C3.ende`).
4. **Passwortschutz auf dem VPS (WICHTIG).**
   - (a) `procguard.DenyInspection()` ist die erste Anweisung in `cmd/postkauz/main.go`, für jedes Kommando; die CLI setzt den Schutz beim Öffnen des Datenverzeichnisses für jedes Kommando erneut und schreibt einen Fehler ins Log (vorher nur in Prozessen mit Zugangsdaten). `t5n_secrets_test.go` angepasst (auch der MCP-Prozess setzt ihn). Test: `TestT5N2DenyInspectionFirstInMain` (liest `main.go` mit `go/parser`; die Wirkung prüft weiter `TestT5NDenyInspection`). **Gemessen** wie in der Prüfung (als `nobody`, ein zweiter Prozess desselben Nutzers liest `/proc/<pid>/environ` in einer Schleife, je 15 Starts von `accounts list` mit frischem Datenverzeichnis): alter Stand 15/15 lesbar bis 190–220 ms nach dem Start, neuer Stand 15/15 lesbar, aber nur bis 10–23 ms (Start der Go-Laufzeit vor `main`). Also kleiner, nicht zu; zu macht es (b).
   - (b) Das Passwort kommt auf dem VPS **nicht mehr über die Umgebung**. `internal/secrets` liest nach der Umgebungsvariable die systemd-Credential `$CREDENTIALS_DIRECTORY/POSTKAUZ_PASSWORD_<KONTO>` (Name wie die Variable; Festlegung 33). Keine neue Abhängigkeit (nur `os`). Sync-Unit: `LoadCredential=POSTKAUZ:/etc/postkauz/credentials/sync-%i` (Ordner mit der Datei `PASSWORD_<KONTO>`; systemd lädt jede Datei als `POSTKAUZ_<Dateiname>`; Verzeichnisform ab systemd 251, geprüft an `man/systemd.exec.xml` der Tags v250/v251/v252/v255 auf GitHub, freedesktop.org war aus der Sitzung gesperrt; leere Zuweisung zum Zurücksetzen und `LoadCredential` in transienten Units auch in v249 vorhanden, geprüft an `src/core/load-fragment.c` und `dbus-execute.c`). MCP-Unit: `InaccessiblePaths=-/run/credentials` zusätzlich (Credentials sind für den Nutzer des Dienstes lesbar, und das ist derselbe). `account add --password-from-env`, `account test`, `sync`, der Umzug von Verweisen, `doctor` (Passwort `credential`) und `doctor --secrets` nehmen die Credential an. `docs/betrieb.md` 4.4 neu (Ordner und Datei anlegen, `systemd-run -p LoadCredential=…`, Drop-in für systemd 249/250, Prüfbefehle mit `grep -c POSTKAUZ_PASSWORD /proc/$PID/environ` → `0`), die Sätze „gleich beim Start“ und „/proc nur mit eigenen Prozessen“ berichtigt (ohne `PrivatePIDs` sieht der MCP-Dienst die Prozesse desselben Nutzers und kann den Sync beenden, liest aber weder Umgebung noch Speicher noch Credentials). **Restrisiko offen genannt** (`betrieb.md` 4.4, `sicherheit.md`, ADR-010 Nachtrag, Unit-Kommentar): Ein übernommener HTTP-Dienst schreibt in dieselbe Datenbank und könnte `accounts.host` auf einen eigenen Server ändern; der Sync meldete sich beim nächsten Lauf dort an. Abhilfe erst mit zwei Systemnutzern (Roadmap). Tests: `TestT5N2SystemdCredential` (`internal/secrets`: Zeilenumbruch, leere Datei, Umgebung zuerst, Link und zu große Datei → Fehler ohne Inhalt, relatives Verzeichnis zählt nicht), `TestT5N2PasswordFromSystemdCredential` (`internal/cli`: Fake-IMAP, `account add --password-from-env`, `account test`, `doctor` zeigt `credential`, Kanarie in keiner Ausgabe, Umgebung geht vor). `systemd-analyze verify` (systemd 255) für beide Units ohne Fehler, nur „Unknown key name 'PrivatePIDs'“ (erwartet).
5. **Kleineres (KLEIN):**
   - (a) `[http]` wird als `toml.Primitive` gelesen (`fileConfig`, das äußere Feld verdeckt `Config.HTTP`) und gesondert dekodiert; ein falscher Typ dort setzt nur `HTTPErr` (`config: <datei>: [http]: … incompatible types …`), `cfg.HTTP` bleibt dann leer. Syntaxfehler (auch unter `[http]`) und falsche Typen außerhalb bleiben global. Der bisherige Grenzfall in `TestT5NHTTPErrorDoesNotStopLoad` ist entfernt. Tests: `TestT5N2HTTPTypeErrorDoesNotStopLoad` (vier Fälle, darunter `http = 5`, und drei globale), `TestT5N2HTTPTypeErrorOnlyStopsMCPHTTP` (`accounts list` läuft, `mcp --http` Exit 1, `doctor` `INVALID`, Syntaxfehler stoppt `accounts list`). `docs/cli.md` Abschnitt 5 angepasst.
   - (b) `TestT5N2RefetchWithClock`: Uhr läuft vor jedem Token mit fremdem `iss` 11 s weiter, Sollwert 0 JWKS-Abrufe (grün; ohne Vorprüfung wären es 5).
   - (c) `deploy/testauth/setup.sh`: ein vorhandener Ordner nur mit `[ -O ]` (sonst Abbruch), neu mit `mkdir -m 700` (ohne `-p` für den Ordner selbst); ein vorhandener leerer eigener Ordner bekommt 700. Geprüft: fremder Ordner (`chown nobody`) leer und mit Marke → Abbruch; neu, zweiter Lauf, eigener mit 755 → 700. Dazu `make testauth-up` beim Integrationslauf.
   - (d) Festlegung 21 um den Satz zu `jwks_uri` ergänzt.
   - (e) `abnahme_D8_cloud.sh`: Maske für `D8_ISSUER` (URL → `<issuer>`, Host:Port und Host → `<issuer-host>`, Prüfung auf `https://`), `sed -u` wenn vorhanden und `python3 -u`, damit Hinweise im Modus `oauth` sofort erscheinen (im Trockenlauf geprüft). Doku: Arbeitsordner steht in der **zweiten** Zeile (Kommentar im Skript berichtigt, wie der Code), Beispiel in `abnahme_D8.md` startet das Skript im Hintergrund; `CHANGELOG.md` „Festlegungen 19–29“ → 30; `$outFile` entfernt. `-ExpectVersion` Standard `0.3.0-alpha.8c`.

## Festlegungen (Einspruch möglich)

31. **Vorrat für Fehlschläge** (berichtigt Festlegung 19): vorher nur ansehen, nach einem Fehlschlag ziehen. Ein 503 im Modus `key` (Datenbank nicht lesbar) zählt wie ein Fehlschlag, weil ohne Datenbank kein Schlüssel als gültig gilt; im Modus `oauth` zieht nur ein 503 nach geprüfter Signatur nicht. Grund: Unangemeldete sollen keinen unbegrenzten Strom von 503 bekommen. **Grenze (Nachtrag der Prüfsitzung):** Ansehen und Ziehen sind zwei getrennte Schritte. Bei vielen gleichzeitigen Fehlschlägen sehen alle den Vorrat, bevor einer zieht; dann entstehen mehr `key_invalid`-/`principal_unknown`-Zeilen als 30 je Minute, nachgestellt etwa N/2 je Sekunde bei N gleichzeitigen Anfragen. Die Antworten bleiben begrenzt (429), nur das Audit nicht. Ein puffernder Audit-Writer steht in der Roadmap.
32. **`DenyInspection` in `main`** meldet einen Fehler nicht selbst (vor `main` gibt es noch kein Log); die CLI setzt den Schutz beim Öffnen erneut und schreibt einen Fehler als Warnung. Er gilt jetzt auch für MCP-Prozesse (schadet nicht, schützt Schlüssel in deren Umgebung).
33. **systemd-Credential:** Name wie die Umgebungsvariable (`POSTKAUZ_PASSWORD_<KONTO>`), Umgebung geht vor; nur eine reguläre Datei (kein Link) bis 64 KiB, ein Zeilenumbruch am Ende (`\n` oder `\r\n`) zählt nicht, eine leere Datei gilt als nicht gesetzt; eine unbrauchbare Datei ist ein Fehler mit Namen, ohne Inhalt, **ohne Rückfall** auf den Schlüsselbund. `$CREDENTIALS_DIRECTORY` zählt nur absolut. Die Sync-Unit nutzt die Verzeichnisform (Dateiname `PASSWORD_<KONTO>`), damit die Vorlage je Konto gleich bleibt; systemd 249/250 (Ubuntu 22.04) brauchen das Drop-in aus `betrieb.md` 4.4 (Ubuntu 24.04 und Debian 12 nicht).
34. **Docker-Compose bleibt bei `env_file`** für den Sync-Dienst: Jeder Container hat einen eigenen PID-Namensraum, der MCP-Container sieht den Sync-Prozess nicht. Credentials für Docker (`secrets:`) erst, wenn Docker der Weg auf dem VPS wird.
35. **Probe:** Exit 1 auch bei falschen Argumenten (vorher 2), einheitlich „1 bei FAIL“; Strg+C bricht ohne Schlusszeile ab (zählt in den Skripten als FAIL); ein sauberes Ende ist keine eigene OK-Zeile (die bekannten Zählungen, etwa 29 OK in Teil A, bleiben). `D8_ISSUER` wird im Cloud-Skript angegeben, nicht aus den Metadaten abgeleitet; Masken unterscheiden Groß- und Kleinschreibung (POSIX-`sed` hat kein `I`).

## In die Roadmap (nicht gebaut)

- GET/DELETE an `/mcp` mit gültigem Bearer erst nach der Anmeldung prüfen (ziehen heute aus dem kleinen Vorrat, KLEIN).
- Größengrenze oder Rotation für `postkauz.log` (Sync und HTTP schreiben dorthin).
- Zwei Systemnutzer auf dem VPS (Rechtemodell; schließt auch das Restrisiko `accounts.host`).
- GreenMail-Integrationstest ohne Reihenfolge-Abhängigkeit.

## Prüfläufe (zweite Nachbesserung)

| Lauf | Ergebnis |
|---|---|
| `make check` | grün nach dem Merge (`e5c24d1`) und vor jedem Commit (in einem eigenen Prüf-Worktree bzw. im Worktree des Subagenten), zuletzt am vollständigen Stand `78be5f7`/`a641160` |
| `make race` | grün an `a641160`, 39 Pakete ohne Datenrennen, 21:33 min gesamt; `internal/cli` **1.291 s (21:31 min, Grenze 45 min)**. `internal/ops`, `internal/httpmcp`, `internal/secrets`, `internal/procguard` u. a. aus dem Go-Testcache eines ersten Laufs an `97941ee` (gleicher Code dieser Pakete; der erste Lauf endete an der 30-min-Grenze der Hintergrundaufgabe der Sitzung, nicht am Test) |
| `make integration` | `IT=dovecot,stalwart,greenmail,authelia` an `78be5f7` (Code wie `a641160`): 41 PASS, 1 SKIP (`TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit); danach `TestIntegrationCapabilitiesAndRoles` und `TestIntegrationMailboxActions` gegen GreenMail einzeln: PASS. Images über `mirror.gcr.io` geholt und umgetaggt; Authelia über das gehärtete `make testauth-up` (Ordner neu mit 700) |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64); `GOOS=windows go vet ./...` sauber |
| `make vuln` | nicht möglich: `vuln.go.dev` antwortet in der Cloud mit 403 (wie bisher); bitte in der Prüfsitzung. Keine neue Abhängigkeit |
| `systemd-analyze verify` | beide Units ohne Fehler (systemd 255, Programmdatei als Attrappe unter `/usr/local/bin/postkauz`, danach entfernt); einziger Hinweis „Unknown key name 'PrivatePIDs'“ (erwartet) |
| Python-Test der Probe | `python3 -m unittest -v docs/abnahme_D8_probe_t5n2_test.py`: 9 Tests OK (gegen die alte Probe rot) |
| Messung `/proc/<pid>/environ` | als `nobody`, je 15 Starts: alt bis 190–220 ms lesbar, neu bis 10–23 ms; mit Credential steht das Passwort gar nicht in der Umgebung |
| PowerShell | kein `pwsh` im Container: `abnahme_D8.ps1` nicht geparst (Klammer-/String-Prüfer des Subagenten und ASCII/BOM-Guard grün). Parsen in PS 5.1 und Lauf: Prüfsitzung (besonders `Test-ProbeEnd` und `$LASTEXITCODE` in `Invoke-ProbeInteractive`) |

### Build 0.3.0-alpha.8c

Gebaut an `a641160` (enthält den ganzen Code; danach nur dieses Protokoll und das Löschen der Wegwerf-Tests) in einem frischen Klon ohne Tags (`git clone --no-tags --branch claude/determined-fermi-1g8w63`, sauberer Arbeitsbaum, 0 Tags), Nachbau in einem zweiten Klon ohne Tags: `SHA256SUMS` gleich (ohne die Zeile `VERSION`), `go version -m`: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261029124141-a641160cce07`, `vcs.revision=a641160cce07802e15d51e43ff2d097f08d43b73`, `vcs.modified=false`. Go 1.27.3, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`. `postkauz version` → `postkauz 0.3.0-alpha.8c (go1.27.3 linux/amd64)`.

```
18261272dd5ff5b09ee7dfca665247eb942bc912382480369a761062951a70e5  postkauz-linux-amd64
3cbca56aca93c33183ecf8321afa5392053c0cdfc5afb07fad0c388256dfb1cd  postkauz-windows-amd64.exe
dcfe8efa7cca026cb3c66a96562b9639eae2a3b385e2837fb465c1e4ca1ae04f  postkauz-darwin-arm64
26043f606d050fd1095f31019e1a1e3d3ce68bdde402ab4b2b5ce1fe17abcdef  Register-AuditExport.ps1
8a50ee74c064cc731446ed6f224d1d3ba8247d7abcea6cb9da834f2e40ec91c1  Register-SyncWatch.ps1
6973c8473a8a244172530c9e926d4d2b854b2bbee05bc013bde1c700bcdf3de3  Run-SyncWatch.ps1
9d40eb8aaef6b4c2cb68a8eea02651823ead6ac87c0a4917b29d9d3fc6a785e9  check-example.ps1
4bf639c3a3a5e3dd66c759ac21d8b1f0b1be6e7174bf67072bfcad01ecececc5  check-example.sh
84af4fb2c005c1655cfbb22dd680aa6b286137ac9fe044ff3684b61c4aa63408  postkauz-sync@.service
55989ca4bfe408cacbba9ee4662ce3baa1f3ad698f80f9f65b71ea4b8a10f96b  postkauz-mcp-http.service
```

Nur für die Abnahme D8, nicht für den Dauerbetrieb (Rahmen 5). Nach bestandener D8 kann dieser Build laut Auftrag direkt nach `main`.

## Offen und Fragen (zweite Nachbesserung)

- Prüfsitzung: `abnahme_D8.ps1` in PS 5.1 parsen, Teil A erneut (29 OK erwartet, dazu keine `.ende`-Zeile); `make vuln`; Teil C aus einer Cloud-Sitzung mit `D8_ISSUER` im Modus `oauth`.
- VPS: vor dem ersten echten Konto `systemctl --version` prüfen (Verzeichnisform von `LoadCredential=` ab 251, sonst Drop-in aus `betrieb.md` 4.4).
- Fragen an den Betreiber: roadmap.md, Offene Fragen 6 bis 8 (8 mit dem Restrisiko `accounts.host` ergänzt).

## Commits (zweite Nachbesserung)

`e5c24d1` (Merge `main`), `d1ef60b` (Punkt 1), `ae8b446` (5b), `c300375` (5a), `8ef0f3a` (4a), `97941ee` (4b), `22a78b8` … `54beb01` (Subagent: 2, 3, 5c, 5e, `-ExpectVersion`), `78be5f7` (`.gitignore`), `a641160` (Protokoll, Roadmap, CHANGELOG); danach dieser Eintrag und das Löschen von `auftraege/A03_T5nb2_tests/`.

## Nachtrag der Prüfsitzung (Kurzprüfung an `a641160`)

Zwei frische Claude-Läufe und Codex an `a641160`, kein BLOCKER. Erledigt nach dem Schluss-Commit `c133ac8` (der lag schon auf dem Zweig, als der Nachtrag kam), **ohne Änderung am Programmcode**; der Build 0.3.0-alpha.8c an `a641160` bleibt gültig.

1. **Test als Nicht-root (WICHTIG):** `TestT5N2SystemdCredential` legte die Datei mit 0o400 an und überschrieb sie; als Nicht-root (CI-Nutzer `runner`) `permission denied`. Jetzt `os.Remove` vor jedem Schreiben, der Fall mit Verknüpfung unter Windows übersprungen. Geprüft mit `go test -c` und `setpriv --reuid=65534 --regid=65534 --clear-groups`: alter Test FAIL (`permission denied`), neuer PASS; als root weiter grün, `GOOS=windows go vet` sauber.
2. **Doku:** (a) Festlegung 31 und `docs/mcp.md` nennen die Grenze: Ansehen und Ziehen sind getrennt, bei N gleichzeitigen Fehlschlägen etwa N/2 Audit-Zeilen je Sekunde statt 30 je Minute. (b) Restrisiko ohne `PrivatePIDs` (systemd < 257) in `betrieb.md` 4.3 und 4.4, `sicherheit.md` und im Kommentar von `postkauz-mcp-http.service`: Kindprozesse des Syncs (`pdftotext`, DOCX-Extraktor) sperren sich nicht, über ihr `/proc/<pid>/root` wäre die Credential-Datei lesbar, ein vor der ersten Anweisung geöffnetes `/proc/<pid>/mem` bleibt erreichbar; Empfehlung für den VPS systemd ≥ 257 oder zwei Systemnutzer. Der Satz in 4.4, der MCP-Dienst könne „weder Umgebung noch Speicher noch Credentials“ lesen, ist entsprechend abgeschwächt. (c) `betrieb.md` 4.7: `sync.env` braucht weiter Zeilen `POSTKAUZ_PASSWORD_<KONTO>=…`, die Credential-Ordner aus 4.4 gelten nur für systemd. (d) Merge-Vermerk in „Phase 2“: Nummer 64 → 65.
3. **Roadmap** (nicht gebaut): puffernder Audit-Writer für gescheiterte Anmeldungen; Fehlschläge der Vorprüfung bei leerem Vorrat mit 401 statt 429; `doctor` warnt bei `ErrCredential`; `accounts add` nennt die Quelle Credential und meldet `ErrCredential` gesondert; `DenyInspection` im `init` des DOCX-Kindes; VPS-Nachstellung von `LoadCredential`/`PrivatePIDs`.
4. **Build und Prüfläufe** (Abschnitte oben, unverändert gültig): Build-Commit **`a641160`**, Klon ohne Tags, Nachbau bytegleich, `go version -m` mit `vcs.revision=a641160cce07…`, `vcs.modified=false`. Die Prüfsitzung hat an `a641160` nachgebaut und dieselben Prüfsummen erhalten (Windows `3cbca56a…`, Linux `18261272…`, darwin `dcfe8efa…`); `make vuln` ohne Fund; **D8 Teil A mit diesem Build auf dem Büro-PC: 29 OK, 0 FAIL, 0 SKIP.** Eigene Läufe: `make race` an `a641160` grün, `internal/cli` 1.291 s (21:31 min); `make integration` (Dovecot, Stalwart, GreenMail, Authelia) 41 PASS, 1 SKIP (GreenMail-Reihenfolge), einzeln PASS; `make cross` grün; Python-Test der Probe 9 OK; `make check` am Ende dieses Nachtrags grün.
   - Hinweis: Der Kommentar in `deploy/systemd/postkauz-mcp-http.service` hat sich geändert. Ein neuer `make release` ergäbe für diese eine Unit-Datei eine andere Prüfsumme; die Programme und alle anderen Dateien bleiben gleich. Für D8 und den Merge gilt der Build an `a641160`.
