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

19. **Ersetzt Festlegung 5 (Rate-Begrenzung):** `/healthz` außerhalb jeder Grenze. Alle Anfragen ohne gültige Anmeldung (fehlendes oder falsches Token, Metadaten, unbekannte Pfade, 405/415 an `/mcp`, fremder Host) teilen sich einen festen Vorrat von 30 je Minute (ebenso viele auf einmal); leer → 429. Nach der Anmeldung hat jeder Schlüssel einen eigenen Vorrat von `[http] max_requests_per_minute`. Eine gescheiterte Anmeldung schreibt ihre Audit-Zeile nur, solange der Vorrat Platz hat. Ein gültiger Schlüssel kommt immer durch. Restrisiko: Bei leerem Vorrat prüft der Modus `key` falsche Schlüssel weiter gegen die Datenbank (nur lesen).
20. **Aussteller nur als DNS-Name** (IP nur auf Loopback): `tailscale serve` stellt Zertifikate nur für MagicDNS-Namen aus, das SDK lehnt private Adressen in der URL ohnehin ab. Geprüft beim Laden (`doctor`) und bei der Discovery.
21. **Eigener Client für den AS:** Verbindung und Antwort je 10 s, keine Umleitungen, ohne SSRF-Sperre des SDK (Begründung Punkt 1).
22. **Hängende oder kaputte JWKS → 503 mit `Retry-After: 10`** statt 401 (ein 401 löste beim Client eine neue Anmeldung aus, am Token liegt es nicht). Anmeldung höchstens 15 s.
23. **JWKS höchstens alle 10 s neu laden.** Eine neue `kid` gilt spätestens 10 s nach dem letzten Abruf, bis dahin 503; eine falsche Signatur mit bekannter `kid` bleibt 401.
24. **Vorprüfung von `iss`, `exp`, `nbf`, `iat`** vor der Signatur, nur als Ablehnung; `nbf` und `iat` je 30 s, beide freiwillig. Ergänzt Festlegung 10: Die Discovery läuft weiter, auch wenn der auslösende Client trennt.
25. **Gescheiterte Anmeldung mit Schlüssel** schreibt `{"via":{"transport":"stdio|http"}}` (auch für stdio, einheitlich); alte Zeilen bleiben in der alten Form. Ergänzt Festlegung 9.
26. **VPS: beide Dienste als Nutzer `postkauz`** statt zweier Systemnutzer (Auftrag Punkt 6). Grund: Rechtemodell von Datenbank und Blobs (nur Eigentümer). Schutz statt dessen über `secrets.Denied` im MCP-Prozess, `PR_SET_DUMPABLE=0` in Prozessen mit Zugangsdaten, Passwortdateien `0600 root:root` außer Sicht der MCP-Unit, `PrivatePIDs=` ab systemd 257. Zwei Nutzer erst mit einem Rechtemodell für eine gemeinsame Gruppe (Roadmap).
27. **Fehler in `[http]`** (ungültiger Wert oder unbekannter Schlüssel unter `http.`) halten nur `mcp --http` auf, auch wenn `--listen` den Wert ersetzen würde; `doctor` zeigt `INVALID` und endet mit Exit 1. Syntaxfehler der Datei und falsche Typen halten weiter jedes Kommando auf (Datei nicht lesbar).
28. **D8:** Bei Abbruch schaltet das Skript `tailscale serve` ohne zweite Frage aus (die Frage vor dem Einschalten sagt es an); belegtes oder unlesbares 443 bricht ab (Exit 2); sagt der Betreiber am Ende von B „nein“, nutzt D die Einstellung weiter und fragt am Ende von D erneut. Teil C wartet auf Dateien (`go`, `callback`) statt auf eine Eingabe; die Anmelde-URL steht zusätzlich in `login-url.txt` (0600), weil eine Cloud-Sitzung kein `/dev/tty` hat.
29. **Images:** Tailscale fest `v1.104.4`, Alpine `3.22.7` (Linie 3.22 beibehalten), je mit Digest.
30. **Log abgewiesener Anfragen:** Sammelzeile höchstens alle 10 s, die erste nach einer ruhigen Zeit sofort; `/healthz` zählt mit (sonst ohne jede Grenze); ein angemeldeter Schlüssel mit 429 gilt als abgewiesen. Eine Größengrenze oder Rotation für `postkauz.log` selbst ist nicht Teil dieser Nachbesserung (Roadmap).

## In die Roadmap (nicht gebaut)

- Prüfen, dass die `listen`-Adresse auf einem Tailscale-Interface liegt.
- Unter Windows Sync und HTTP-MCP unter getrennten Benutzern.
- Auf dem VPS zwei Systemnutzer mit gemeinsamer Gruppe (braucht ein Rechtemodell für Datenbank, WAL und Blobs, Festlegung 26).
- Größengrenze oder Rotation für `postkauz.log` (auch der Dauer-Sync schreibt dorthin).
