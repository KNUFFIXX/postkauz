# T4 — Microsoft-Postfächer per OAuth2: Stand und Übergabe

Teil T4 aus [auftraege/A03_teile.md](../../auftraege/A03_teile.md) (S3.7, E11, R2), parallel gebaut von `main` `1c9847a` auf dem Zweig `claude/great-johnson-fcqyuh` (27.10.2026). **Keine Migration.** Nach Rahmen 10 stehen Stand und CHANGELOG-Zeilen nur hier; die Prüfsitzung überträgt sie beim Merge nach `docs/roadmap.md` und `CHANGELOG.md` und führt `docs/betrieb_microsoft.md` als Abschnitt „Microsoft“ in `docs/betrieb.md` (aus T2) ein.

## 1. Stand

Gebaut und getestet (alles gegen Fake-Server, nie gegen ein echtes Postfach):

| Vorgabe | Umsetzung | Nachweis |
|---|---|---|
| S3.7: `golang.org/x/oauth2` direkt | v0.35.0 (schon im Modulgraph über das MCP-SDK, per `go get` bestätigt, BSD-3-Clause) | `go.mod` |
| S3.7: `internal/oauth` Loopback-Flow (freier Port, `state`, Frist 5 min, Browser nur öffnen) | `Microsoft.LoginLoopback`: 127.0.0.1 und ::1, `http://localhost:<port>`, PKCE S256, kein Client-Secret, falscher `state` beendet den Flow nicht | `TestLoginLoopbackEndToEnd`, `…DeniedAndTimeout`, `…IgnoresWrongState` |
| S3.7: Device-Code-Flow (Abfrage nach `interval`) | `Microsoft.LoginDeviceCode` über `x/oauth2` (`authorization_pending`, `slow_down` +5 s) | `TestLoginDeviceCodePendingAndSlowDown` (synctest: genau 30 s, 4 Abfragen), `…Denied` |
| S3.7: HTTP-Refresher mit `oauth.Source`, Refresh 2 min vor Ablauf, Fehler → Sperrzeit 5 min, Audit, kein zweiter Versuch | `Microsoft.Refresher`; `Source.Report` → Systemereignis `oauth.refresh` (`ok`/`error` + Code) | `TestRefresherWithSource`, `TestAccountsLoginTestAndRefreshAudit` |
| S3.7: `account add --auth oauth2 …`, `account login [--device-code]` (HumanOnly, `admin`), `account test` mit XOAUTH2, `doctor` zeigt Token-Ablauf | Operation `accounts.login` (R1, kein MCP-Tool), `accounts.add` mit `client_id`/`tenant`, `provider.Credential` bis zur IMAP-Anmeldung | Vertragstest (ops, CLI, MCP), `TestProbeWithXOAuth2TokenSource`, `TestOAuth2AccountCLI` |
| S3.7 Akzeptanz 3: Kanarientest, kein Token in Ausgabe, Datei, Audit | Token-Präfix `CANARY-tok` des Fake-AS | `TestOAuth2AccountCLI` (alle Ausgaben, jede Datei im Datenverzeichnis), `TestAccountsLoginTestAndRefreshAudit` (Audit, Systemereignisse, Datenbank), `TestEndpointErrorsCarryNoProviderText` |
| S3.7 Akzeptanz 4: ohne Schlüsselbund lehnt `account add --auth oauth2` ab (R2) | `secret_store_unavailable` mit Hinweis; ebenso `account login` | `TestAccountsAddOAuth2` |
| T4.1: Windows-Schlüsselbund ≤ 2.560 Byte je Eintrag | nur Refresh-Token und Ablauf; über 2.000 Byte verteilt auf `oauthpart<n>/<datadir-id>/<name>` mit SHA-256 im Haupteintrag | `TestSaveLoad4KBTokenInParts` (4-KB-Token, Speicher mit 2.560-Byte-Grenze, kürzeres Token räumt Teile ab, beschädigter/fehlender Teil fällt auf) |
| T4.2: Speichern scheitert → Sync bricht laut ab; Token-JSON gezielt schwärzen | `ErrTokenNotStored` (Quelle gibt kein Token mehr aus, bis das Speichern gelingt; Anmeldefehler, `sync --watch` endet); Ablage kodiert ausdrücklich mit `Reveal`; `secrets.RedactTokens` für freien Text | `TestSourceStopsWhenTokenCannotBeStored`, `TestTokenNotStoredStopsLoudly`, `TestTokenJSONIsRedactedButStorageIsNot` |
| T4.3: Anleitung `docs/betrieb_microsoft.md` („Stand Oktober 2026, ungeprüft“) | mit vollständigen Klickfolgen | Datei |
| Haltepunkt: Abnahme D9 | `docs/abnahme_D9.ps1` + `.md` (`-DataDir`/`-Config` Pflicht, Abbruch bei Standard-/Live-Pfad, OK/FAIL je Schritt, Maskierung) | Offline-Teil mit dem Linux-Programm nachgestellt (gleiche Kommandos, gleiche Muster) |

Doku im selben Commit wie das Verhalten: `docs/cli.md` (2a, Tabelle, Fehlercodes), `docs/mcp.md` (nicht über MCP), `docs/sicherheit.md` (Abschnitt 4), `docs/datenmodell.md` (`accounts`), `docs/architektur.md` (Paketübersicht), `README.md` (Install, Fehlersuche).

Commits:

1. `feat: add Microsoft OAuth2 flows and keyring token storage`
2. `feat: add Microsoft accounts with OAuth2 sign-in`
3. `docs: add acceptance D9 and the T4 handover`

## 2. Festlegungen (Einspruch möglich)

1. **`x/oauth2` v0.35.0** statt der neuesten v0.37.0: Die Version ist schon im Modulgraph (MCP-SDK) und von `make vuln` abgedeckt; ein Sprung hebt andere Abhängigkeiten mit an. Anheben, wenn `govulncheck` etwas meldet.
2. **Client-ID und Mandant ohne Migration in `accounts.sync_policy`** (JSON `{"oauth2":{"client_id","tenant"}}`; die Spalte war seit 0001 vorhanden und unbenutzt `{}`). T4 darf keine Migration schreiben (Rahmen 4). Eine spätere Migration kann sie in eigene Spalten ziehen.
3. **Mandant ohne Angabe `common`**; D9 nutzt `consumers` (private Konten).
4. **Gespeichert: Refresh-Token, Ablauf des letzten Access-Tokens, Zeitpunkt der Speicherung.** Folge: Jeder neue Prozess (`sync`, `account test`) erneuert einmal zu Beginn und schreibt das neue Refresh-Token (Microsoft ersetzt es); `sync --watch` erneuert etwa stündlich. Auch ein Trockenlauf von `sync` erneuert und speichert (ohne Token keine Anmeldung).
5. **Teilgröße 2.000 Byte, höchstens 32 Teile**, Verweise `oauthpart1/<datadir-id>/<name>` usw. (die Form des Verweises bleibt gültig, der Kontoname wird nicht länger).
6. **Kein `POSTKAUZ_PASSWORD_<NAME>` für OAuth2-Konten**; gelesen wird nur der Speicher. Der unsichere Testspeicher `insecure-file` bleibt für Tests wählbar (ausdrücklich gesetzt, `doctor` warnt wie bisher).
7. **Speicherfehler:** Die Quelle versucht beim nächsten Aufruf nur das Speichern erneut, keinen zweiten Refresh.
8. **Systemereignis `oauth.refresh`:** Akteur `system`, Schlüssel `system`, Scope `mail:sync`, Ziel `account:<name>`, Ergebnis `ok` oder `error`, Grund = Code (`invalid_grant`, `token_not_stored`, `network`, `timeout`, `http_<status>` …). Es entsteht bei `account test` und `sync`, nicht bei `account login` (dort steht `accounts.login` im Audit).
9. **`accounts.login`:** R1, Scope `admin`, HumanOnly, kein MCP-Tool. Adresse und Code der Anmeldung gehen als Fortschritt (`oauth_prompt`) auf stderr, auch mit `--json`.
10. **Neue Fehlercodes:** `no_token`, `token_not_stored`, `oauth_refresh` (auch für ein unlesbares Token), `login_failed`; alle Exit 1.
11. **SASL:** `OAUTHBEARER` vor `XOAUTH2`, je nach CAPABILITY vor der Anmeldung (vorhandene Logik); Microsoft bietet `XOAUTH2`.
12. **`account remove`** löscht bei OAuth2-Konten Haupteintrag und alle Teile; die Ausgabe heißt weiter `stored password deleted` (Text unverändert, ein bestehender Test hängt daran).
13. **Abnahme-Skript:** die zwei Anmeldungen brauchen ein sichtbares Fenster (Enter, Code); ohne `-Zustimmung "ja …"` läuft nur der Offline-Teil.

## 3. Prüfungen in dieser Sitzung

| Prüfung | Ergebnis |
|---|---|
| `make check` vor jedem Commit (im eigenen Worktree auf dem Commit) | grün |
| `make race` | siehe Abschnitt 6 |
| `make integration` (Docker: Dovecot, Stalwart, GreenMail; `dockerd` von Hand gestartet) | siehe Abschnitt 6 |
| `make vuln` | siehe Abschnitt 6 |
| `make cross`, `make release VERSION=0.3.0-ms.1` | siehe Abschnitt 6 |
| PowerShell-Syntax von `abnahme_D9.ps1` | **nicht geprüft**: kein `pwsh` im Container, das Herunterladen hat die Sitzung nicht erlaubt. Klammern gezählt (ausgeglichen), Offline-Kommandos mit dem Linux-Programm nachgestellt. Die Prüfsitzung prüft vor dem Lauf auf dem Büro-PC: `[System.Management.Automation.Language.Parser]::ParseFile('abnahme_D9.ps1', [ref]$null, [ref]$e); $e` (leer = gut) und PSScriptAnalyzer, falls vorhanden. |

## 4. CHANGELOG-Zeilen (für „Unreleased“, Abschnitt Added)

- S3.7 (T4): Microsoft-Postfächer per OAuth2 („bring your own client“): `account add <name> --auth oauth2 --client-id <id> [--tenant common|consumers|organizations|<id>]`, `account login <name> [--device-code]` (nur für Menschen, kein MCP-Tool); Anmeldung an IMAP per SASL `XOAUTH2`/`OAUTHBEARER`, `account test` nennt das Verfahren. Im Schlüsselbund liegt nur das Refresh-Token (bei Bedarf in Teilen unter `oauthpart<n>/…`), das Access-Token bleibt im Speicher und wird 2 min vor Ablauf erneuert; jeder Refresh ist das Systemereignis `oauth.refresh`. Ohne Schlüsselbund abgelehnt (R2). Neue Fehlercodes `no_token`, `token_not_stored`, `oauth_refresh`, `login_failed`. `doctor` zeigt Token-Stand und -Ablauf, `doctor --secrets` sucht das Refresh-Token. Anleitung `docs/betrieb_microsoft.md`, Abnahme `docs/abnahme_D9.md`/`.ps1`.
- `secrets.RedactTokens` schwärzt Token-Felder und JWTs in freiem Text; Fehler des Token-Endpunkts tragen nur Code und AADSTS-Nummer.
- `golang.org/x/oauth2` v0.35.0 ist direkte Abhängigkeit.

Roadmap-Zeile (für „Stand S3“): S3.7 gebaut (T4, Build `0.3.0-ms.1`), Haltepunkt Abnahme D9 (Prüfsitzung, Büro-PC, nur lesend); App-Registrierung ungeprüft.

## 5. Hinweise für den Merge

- Reihenfolge laut Plan: T1a → T1c → **T4** → T1b → T2 → T3 → T5. Erwartete Konflikte: `internal/store/accounts.go` (`accountCols`, `scanAccount`; T2 ergänzt `drafts_mailbox` mit A5.1), `internal/ops/accounts.go`, `internal/ops/catalog.go` (`Deps`), `internal/cli/exit.go`, `internal/cli/bindings.go`, `docs/cli.md`, `docs/sicherheit.md`. Fachlich unabhängig.
- Die Schnittstellen `ops.SyncRunner.Sync`, `ops.AccountProber.Probe` und `mailsync.Runner.Connect` nehmen jetzt `provider.Credential` statt `secrets.Secret`; Teile, die neue Testfakes dafür schreiben (T1a, T1b), passen die Signatur an (`provider.PasswordCredential(pw)`).
- T1b (A1, Variante A): `oauth.refresh` ist ein einzelner Systemeintrag außerhalb des Executors (wie `drafts.remote_check`); der Vertragstest bleibt bei „ein Eintrag bei Ablehnung, zwei bei Erlaubnis“ für `accounts.login`.
- T1c (A3, neuer Passwortleser): `account login` liest nichts von der Tastatur; die Device-Code-Anzeige geht über den Fortschritt auf stderr. Nichts anzupassen.
- T2 legt `docs/betrieb.md` an; `docs/betrieb_microsoft.md` dort als Abschnitt „Microsoft“ einfügen (die Datei selbst kann dann entfallen; Verweise in `docs/cli.md`, `docs/sicherheit.md`, `README.md`, `docs/abnahme_D9.md` nachziehen).

## 6. Prüfergebnisse vor dem Push

(nachgetragen am Ende der Sitzung)

## 7. Fund außerhalb von T4

`TestIntegrationImportOverlapDovecot` (`internal/importer`) scheitert gegen das frische Dovecot im Container: `new 307, existing 171, skipped 3, errors 0`, erwartet `305/173/3/0` (zwei überlappende Nachrichten werden nicht als vorhanden erkannt). **Auf dem Ausgangsstand `1c9847a` identisch** (eigener Worktree, gleicher Lauf), also nicht durch T4 verursacht; T4 berührt den Import nicht. Vermutung (ungeprüft): Das Dovecot-Image hat sich geändert und schreibt zwei der Nachrichten anders ab (z. B. die ohne Message-ID, die nur über `content_hash` erkannt werden). Nicht in T4 behoben (anderer Teil, kein Anlass zum Verbreitern). **Vorschlag:** Die Prüfsitzung prüft Image-Tag und -Datum in `deploy/testmail/docker-compose.yml` gegen den letzten grünen Lauf (S2.9, `docs/protokolle/S29_pruefungen.txt`) und gibt den Punkt an T1a oder T1c.

## 8. Offene Fragen an den Betreiber (mit Standardantwort)

1. **Darf ein privates Microsoft-Konto ohne Azure-Verzeichnis eine App registrieren?** Ungeprüft. **Default:** In D9 ausprobieren; verlangt das Portal ein Verzeichnis, legst du ein kostenloses Azure-Konto an (ungeprüft, ob dafür eine Zahlungsangabe nötig ist), oder wir nehmen dein Firmenkonto mit `--tenant organizations`.
2. **Konto nach D9 behalten?** **Default:** nein, Abbau nach Abschnitt 5 von `abnahme_D9.md`; für den Dauerbetrieb wird das Konto nach der Freigabe von 0.3.0 im Live-Datenverzeichnis neu angelegt.
3. **Client-ID und Mandant später in eigene Spalten?** **Default:** ja, mit der nächsten Migration nach dem Merge (T1b oder T2), ohne Eile; bis dahin `accounts.sync_policy`.
