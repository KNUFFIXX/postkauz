# Spike P-AUTH: Autorisierungsserver und zustandsloser HTTP-Transport (A03 S3.0, T5 Phase 1)

Stand 27.10.2026. Ausgangsstand `main` `1c9847a`, Zweig `claude/charming-wozniak-qy1x1c`. Eine Cloud-Sitzung, nur im Linux-Container (Docker 29.9.2, Go 1.27.3 nach der Ausnahme in `CLAUDE.md`). Keine echten Postfächer, keine echten Zugangsdaten: ein synthetisches Testkonto `tester`, Zufallspasswort und Schlüssel nur zur Laufzeit außerhalb des Repositorys, Test-CA für die Namen `*.pauth.test` (nur `/etc/hosts` im Container). Rohdaten: [S30_pauth_rohdaten.txt](S30_pauth_rohdaten.txt). Spike-Code: `spike/pauth` (eigenes Modul, nur auf dem Zweig). Entwurf der Entscheidung: [ADR-010_entwurf.md](ADR-010_entwurf.md).

## 1. Aufbau

1. **Prototyp `mcp --http`** (`spike/pauth/rs`): MCP Go SDK v1.8.0, `StreamableHTTPOptions{Stateless: true, JSONResponse: true}`, nur `POST /mcp`; `auth.RequireBearerToken` mit eigenem Prüfer: Signatur über die JWKS des Ausstellers (`github.com/coreos/go-oidc/v3` v3.21.0, Apache-2.0, per `go get` aufgelöst; zieht `go-jose/v4` v4.1.4 und `golang.org/x/oauth2` ≥ v0.36.0), `iss`, `exp` mit 30 s Uhrabweichung, `aud` über `oauthex.MatchesResource` gegen die eigene Resource-URL, optional JOSE-Typ `at+jwt` (RFC 9068), dann `sub` → PostKauz-Schlüsselname; unbekannter `sub` → 403 ohne Hinweis. PRM unter `/.well-known/oauth-protected-resource/mcp` (und ohne Pfad), `WWW-Authenticate` mit `resource_metadata`, `/healthz`. Ein Tool `whoami` zeigt Schlüssel, Principal und `transport: http`.
2. **Unit-Tests ohne Docker** gegen einen Fake-AS (`httptest`, eigener RSA-Schlüssel, JWKS) — alle grün.
3. **Messprogramm** (`spike/pauth/cmd/probe`) gegen den echten AS im Docker: Anmeldung des Testkontos wird automatisch „geklickt“ (Authelia: `/api/firstfactor` und `/api/oidc/consent`; Dex: Formular). Der Prototyp läuft im selben Prozess auf `https://mcp.pauth.test:8443/mcp`. Der **SDK-Client** (`auth.AuthorizationCodeHandler`, vorregistrierter öffentlicher Client `claude-code`) macht Discovery (PRM → AS-Metadaten), PKCE S256 und `resource` selbst. Ausgegeben werden nur Kopf und Claims, nie ein Token.
4. **Netz im Container:** `ghcr.io` liefert keine Image-Blobs (403 vom Proxy), `quay.io` ist gesperrt, Docker Hub antwortete zeitweise mit 429. Dex wurde deshalb zuerst aus dem Quelltext gebaut (Go-Proxy, Stand `master` `b02ead0`, 26.10.2026) und danach mit dem offiziellen Image `dexidp/dex:v2.46.1` von Docker Hub wiederholt; die Tabelle nennt den Release-Stand.

## 2. Prototyp: Ergebnisse der Unit-Tests (`go test ./rs`)

| Test | Ergebnis |
|---|---|
| ohne Bearer | 401, `WWW-Authenticate: Bearer resource_metadata="…/.well-known/oauth-protected-resource/mcp"` |
| PRM (mit und ohne Pfad) | JSON mit `resource` und `authorization_servers` |
| gültiges Token | 200, kein `Mcp-Session-Id` |
| andere `aud`; `aud` = Client-ID (ID-Token-Muster); abgelaufen; ohne `exp`; falsche Signatur; fremder `iss`; `typ: JWT` bei Pflicht `at+jwt`; Müll | je 401 |
| `aud` mit abschließendem `/`; abgelaufen innerhalb 30 s | 200 |
| unbekannter `sub` | 403, Text `forbidden`, ohne Subjekt |
| Token in Antwort oder Kopfzeile | nie |
| GET/DELETE `/mcp` | 405 |
| SDK-Client Ende zu Ende | `initialize`, `tools/list`, `tools/call` ok; ausgehandelte Version **2026-07-28** |
| hinter Reverse-Proxy mit fremdem `Host` | SDK-Schutz an: **403** `invalid Host header`; eigene Allowlist ohne den Namen: 421; mit dem Namen: 200 |

## 3. Prüfliste P-AUTH je Kandidat

| Frage | Authelia 4.39.28 | Dex v2.46.1 |
|---|---|---|
| PKCE S256 Pflicht? | **ja** je Client (`require_pkce`, `pkce_challenge_method: S256`): ohne `code_challenge` und mit `plain` → `invalid_request` | **ja** global (`oauth2.pkce.enforce`, nur `S256`): ohne und mit `plain` → `invalid_request` |
| RFC 8707 oder feste `aud` je Client? | **beides.** `resource` → `aud` = genau die Resource-URL; ohne `resource` bleibt `aud` leer, mit `requested_audience_mode: implicit` steht die konfigurierte `audience` trotzdem drin (gemessen) | **nein.** `resource` wird ignoriert; `aud` = `client_id`. Umweg: eigener Client mit der Resource-URL als ID und `trustedPeers`, der MCP-Client fordert den Sonder-Scope `audience:server:client_id:<url>` an → `aud = [<url>, claude-code]` |
| Access-Token als JWT mit JWKS? | **ja** mit `access_token_signed_response_alg: RS256`: `typ: at+jwt`, `client_id`, `scp`, `jti`, Lebensdauer 300 s (konfiguriert); ID-Token `typ: JWT`, `aud` = Client → vom RS sauber trennbar | **ja**, aber das Access-Token ist ein ID-Token: kein `typ`, gleiche `aud` wie das ID-Token, kein `client_id` (nur `azp`) → ID- und Access-Token sind für den RS **nicht unterscheidbar** |
| statischer Client mit URL als `client_id` (CIMD)? | **nein.** URL als `client_id` lehnt der Start ab („only RFC3986 unreserved characters“); CIMD nicht angeboten (`client_id_metadata_document_supported` fehlt), unbekannte URL → `invalid_client` | **URL als statische ID ja** (Flow ok, `aud` = die URL); CIMD nicht angeboten, unbekannte URL → 400 |
| Device-Code? | **ja** (`/api/oidc/device-authorization`, `user_code`, `verification_uri_complete`, Intervall 10 s; ohne Freigabe wartet die Abfrage) | **ja** (`/dex/device/code`, Intervall 5 s) |
| Speicher im Leerlauf < 300 MB? | **ja:** 52 MiB im Leerlauf, 117 MiB nach den Flows (Argon2-Hash beim Login); Image 110 MB | **ja:** 10,5 MiB (Image), 32 MiB (eigener Build) |
| Lizenz OSS? | Apache-2.0 | Apache-2.0 |
| gepflegt (Version < 6 Monate)? | ja, 4.39.28 vom 17.09.2026 | ja, v2.46.1 vom 21.10.2026 |
| Image-Tag und Datum | `authelia/authelia:latest` = 4.39.28, erstellt 17.09.2026, Revision `8da42b2` | `dexidp/dex:v2.46.1`, erstellt 21.10.2026, Revision `75eb192`; dazu Quellbau `master` `b02ead0` (26.10.2026) |
| Ende zu Ende mit SDK-Client und Prototyp | **ok:** Discovery über RFC 8414, `iss` in der Antwort (RFC 9207), `tools/list`, `whoami` → `cloud-reader`; Refresh in 8 ms, neues Token → 200 | nur mit Sonder-Scope ok; **Refresh hängt** (siehe B6) |
| Token-Weitergabe abgelehnt (Akzeptanzkriterium 5) | ja: Token des Clients `other-client` für `https://other.pauth.test/mcp` → 401; ID-Token → 401 | ohne Umweg lehnt der RS jedes Dex-Token ab (`aud` = Client); mit Umweg würde ein ID-Token desselben Clients angenommen |

**Vorschlag: Authelia 4.39.x** als Autorisierungsserver. Dex scheidet aus (B5, B6). PocketID und Zitadel waren hier nicht messbar (Images nur auf `ghcr.io`), Keycloak nicht (nur `quay.io`; laut Auftrag nur Rückfall) — nicht nötig, weil Authelia alle Pflichtpunkte erfüllt.

## 4. Befunde

- **B1 — DNS-Rebinding-Schutz des SDK blockiert den Betrieb hinter `tailscale serve`/Caddy.** Das SDK lehnt jede Anfrage, die über eine Loopback-Adresse mit einem fremden `Host` ankommt, mit 403 ab (`DisableLocalhostProtection` ist aus). Hinter einem Reverse-Proxy auf `127.0.0.1:8787` kommt aber genau das an, wenn der Proxy den `Host` des MagicDNS-Namens weiterreicht (Caddy tut das laut Doku standardmäßig, ungeprüft; `tailscale serve` ungeprüft, D8). Im Spike traf es schon den direkten Aufruf über `mcp.pauth.test`. Lösung im Prototyp: SDK-Schutz aus, eigene Allowlist (`[http] allowed_hosts`, Standard: Host der Resource-URL; sonst 421). Damit bleibt der Schutz gegen DNS-Rebinding erhalten.
- **B2 — `RequireBearerToken` schreibt `err.Error()` in die Antwort.** Der Prüfer darf deshalb nur feste Fehler liefern (Prototyp: immer `invalid token`; 403 nur `forbidden`). Kein Token, kein Subjekt, kein Grund im Text (Test).
- **B3 — Authelia verlangt bei `offline_access` immer eine ausdrückliche Zustimmung.** `consent_mode: implicit` gilt im Code-Flow nicht, sobald ein Refresh-Token angefragt wird; der Mensch klickt einmal je Anmeldung „Zustimmen“ (9 Klicks im Messlauf, einer je Flow). Für PostKauz in Ordnung (Anmeldung ist selten); mit `pre_configure` ließe sich die Zustimmung merken.
- **B4 — Ohne `resource` ist `aud` bei Authelia leer**, außer mit `requested_audience_mode: implicit`. Ob Claude Code `resource` mitschickt, ist ungeprüft; die Einstellung macht das unerheblich und gehört in die Anleitung.
- **B5 — Dex unterscheidet Access- und ID-Token nicht** (Tabelle, Zeile „Access-Token als JWT“: gleiche Claims, kein `typ`). Ein RS kann damit Token-Verwechslung nicht sicher ausschließen; der Umweg über den Sonder-Scope verlangt zusätzlich, dass jeder MCP-Client diesen Scope fest konfiguriert.
- **B6 — Dex-Refresh hängt.** Mit öffentlichem Client und lokalem Passwort-Connector blockiert `POST /token` (Refresh) dauerhaft, danach auch neue Anmeldungen; reproduziert mit `master` (Speicher `memory`) und `v2.46.1` (`memory` und `sqlite3`). Goroutine-Dump: `RefreshStore.Rotate` ruft innerhalb von `UpdateRefreshToken` erneut den Speicher (`GetOfflineSessions`) und wartet auf die eigene Sperre. Nach zwei Fehlversuchen nicht weiter verfolgt; ein Fehlerbericht an das Projekt wäre sinnvoll.
- **B7 — Claude Code als Client** (Doku `code.claude.com/docs/en/mcp`, Stand 27.10.2026, nicht gemessen): vorregistrierter öffentlicher Client mit `claude mcp add --transport http --client-id <id> --callback-port <port> …` (oder `oauth.clientId`/`callbackPort` in der JSON-Konfiguration); Umleitung **`http://localhost:<port>/callback`** (nicht `127.0.0.1`); Discovery über PRM und RFC 8414; hängt `offline_access` an, wenn der AS es anbietet; erneuert bei 401 einmal; prüft `iss` der Antwort. CIMD und DCR nutzt Claude Code selbst, beides bietet Authelia nicht → vorregistrierter Client ist der Weg. Der Browser-Login braucht eine interaktive Sitzung (`/mcp` oder `claude mcp login`, ohne Browser mit Einfügen der Umleitungs-URL); `claude -p` und Agent-SDK-Läufe können ihn nicht. Feste Kopfzeilen (`headers: {"Authorization": "Bearer ${VAR}"}`) und `headersHelper` gehen ohne Browser → für Cloud-Sitzungen ist der Modus `key` (R1) der praktische Weg; Risiko 3 bleibt für OAuth aus der Cloud-Sitzung bestehen, D8 misst es.
- **B8 — Protokoll:** Der SDK-Client handelt `2026-07-28` aus; der zustandslose Handler beantwortet auch das alte `initialize` (2025-11-25) ohne Session-ID. GET/DELETE → 405, wie in S3.5 verlangt.
- **B9 — Abhängigkeiten für S3.6:** `github.com/coreos/go-oidc/v3` v3.21.0 (01.09.2026, Apache-2.0) bringt `github.com/go-jose/go-jose/v4` v4.1.4 (Apache-2.0) und verlangt `golang.org/x/oauth2` ≥ v0.36.0 (Hauptmodul heute v0.35.0 indirekt; S3.7 macht es ohnehin direkt). Die `typ`-Prüfung (`at+jwt`) liest den JOSE-Kopf selbst, weil `go-oidc` sie nicht anbietet (zehn Zeilen, kein Krypto-Code).

## 5. Was der Spike nicht klärt (für S3.5/S3.6 und D8)

1. Ob `tailscale serve` den `Host` weiterreicht (B1) — D8 Schritt 2; die Allowlist deckt beide Fälle.
2. Ob Claude Code `resource` schickt und mit Authelia Ende zu Ende durchkommt (B4, B7) — D8 Schritt 4, am PC des Betreibers.
3. Verhalten bei Schlüsselrotation im AS (JWKS-Neuladen bei unbekannter `kid` macht `go-oidc` selbst; Test in S3.6 mit dem Fake-AS).
4. Authelia im Dauerbetrieb auf einem 5–8-€-VPS (Speicher, Updates) — nach dem VPS-Entscheid.

## 6. Spike-Code

`spike/pauth` liegt nur auf dem Arbeitszweig, in einem eigenen Commit (`chore: add P-AUTH spike code (branch only)`), damit die Prüfsitzung ihn beim Zusammenführen weglässt. Übernommen wird nichts davon unmittelbar; S3.6 baut `internal/httpmcp` neu nach ADR-010 und kann die Tests aus `rs_test.go` als Vorlage nehmen. Nachstellen: [`spike/pauth/README.md`](../../spike/pauth/README.md).
