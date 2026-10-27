# T4 — Microsoft-Postfächer per OAuth2: Stand und Übergabe

Teil T4 aus [auftraege/A03_teile.md](../../auftraege/A03_teile.md) (S3.7, E11, R2), parallel gebaut von `main` `1c9847a` auf dem Zweig `claude/great-johnson-fcqyuh` (27.10.2026; Nachbesserung nach der Gegenprüfung am selben Tag, Build `0.3.0-ms.2`, Abschnitt 1a; zweite Nachbesserung nach der Nachprüfung und Zusammenführung mit `main` `f536810` (T1a), Build `0.3.0-ms.3`, Abschnitt 1b). **Keine Migration.** Nach Rahmen 10 stehen Stand und CHANGELOG-Zeilen nur hier; die Prüfsitzung überträgt sie beim Merge nach `docs/roadmap.md` und `CHANGELOG.md` und führt `docs/betrieb_microsoft.md` als Abschnitt „Microsoft“ in `docs/betrieb.md` (aus T2) ein.

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
| T4.1: Windows-Schlüsselbund ≤ 2.560 Byte je Eintrag | nur Refresh-Token und Ablauf; über 2.000 Byte verteilt auf `oauthpart<n>g<generation>/<datadir-id>/<name>` mit SHA-256 im Haupteintrag (W2: neue Generation je Speichern) | `TestSaveLoad4KBTokenInParts` (4-KB-Token, Speicher mit 2.560-Byte-Grenze, kürzeres Token räumt Teile ab, beschädigter/fehlender Teil fällt auf) |
| T4.2: Speichern scheitert → Sync bricht laut ab; Token-JSON gezielt schwärzen | `ErrTokenNotStored` (Quelle gibt kein Token mehr aus, bis das Speichern gelingt; Anmeldefehler, `sync --watch` endet); Ablage kodiert ausdrücklich mit `Reveal`; `secrets.RedactTokens` für freien Text | `TestSourceStopsWhenTokenCannotBeStored`, `TestTokenNotStoredStopsLoudly`, `TestTokenJSONIsRedactedButStorageIsNot` |
| T4.3: Anleitung `docs/betrieb_microsoft.md` („Stand Oktober 2026, ungeprüft“) | mit vollständigen Klickfolgen | Datei |
| Haltepunkt: Abnahme D9 | `docs/abnahme_D9.ps1` + `.md` (`-DataDir`/`-Config` Pflicht, Abbruch bei Standard-/Live-Pfad, OK/FAIL je Schritt, Maskierung) | Offline-Teil mit dem Linux-Programm nachgestellt (gleiche Kommandos, gleiche Muster) |

Doku im selben Commit wie das Verhalten: `docs/cli.md` (2a, Tabelle, Fehlercodes), `docs/mcp.md` (nicht über MCP), `docs/sicherheit.md` (Abschnitt 4), `docs/datenmodell.md` (`accounts`), `docs/architektur.md` (Paketübersicht), `README.md` (Install, Fehlersuche).

Commits:

1. `feat: add Microsoft OAuth2 flows and keyring token storage`
2. `feat: add Microsoft accounts with OAuth2 sign-in`
3. `docs: add acceptance D9 and the T4 handover`
4. `fix: show the login mechanism only for OAuth2 accounts`
5. `docs: record the T4 checks before the push`

## 1a. Nachbesserung nach der Gegenprüfung (Build `0.3.0-ms.2`)

Die Gegenprüfung (unabhängiger Prüfer, am Code belegt) ergab PASS ohne Blocker. Behoben je Punkt mit eigenem Commit und Test:

| Punkt | Befund | Behebung | Test |
|---|---|---|---|
| W1 | R2 fragte nur `Status().Available`; `insecure-file` meldet `true`, ein echtes Refresh-Token landete in `insecure-secrets/secrets.json` | `oauthStoreAllowed`: `insecure-file` nur für Konten mit `insecure_test_server`; geprüft in `accounts.add`, `accounts.login` und `Credentials.For` (`account test`, `sync`), Fehler `secret_store_unavailable` | `TestOAuth2RejectsInsecureFileStore` |
| W2 | `Save` überschrieb Teile an Ort und Stelle; Fehler vor dem Haupteintrag zerstörte auch das alte Token | Teile unter neuer, zufälliger Generation → Haupteintrag → alte Generation löschen; Fehler löscht die neuen Teile; `Load` liest neu, wenn ein Teil während des Lesens verschwindet; Format ohne Generation (ms.1) lesbar und abgeräumt | `TestSaveFailureKeepsOldToken` (Fehler an jeder Schreibstelle), `TestConcurrentSavesNeverCorrupt` (zweiter Prozess an jeder Stelle des ersten), `TestConcurrentSavesStress` (zwei schreibende, ein lesender, auch unter `-race`), `TestSaveReplacesLegacyParts` |
| K3 | `account test --dry-run`, `sync --dry-run` speicherten das erneuerte Token | `Credentials.For(a, dryRun)`: im Trockenlauf kein `OnRefresh`; Festlegung 4 aufgehoben | `TestDryRunRefreshStoresNothing` |
| K4 | `::1` belegt → Flow lief nur auf 127.0.0.1 weiter | `loopbackListeners`: neuer Port (bis 20 Versuche), bis beide gebunden sind; ohne IPv6-Loopback genügt 127.0.0.1 | `TestLoginLoopbackPortFreeOnBothAddresses` (nachgestelltes ::1, läuft auch ohne IPv6) |
| K5 | Tausch unter der Restfrist des Logins (rohes `DeadlineExceeded`); Device-Code ohne `expires_in` unbegrenzt | Tausch mit eigener Frist 60 s, Zeitüberschreitung → `ErrLoginTimeout` (`login_failed`); Device-Code höchstens 15 min | `TestLoginLoopbackExchangeHasOwnDeadline`, `TestLoginDeviceCodeLimitWithoutExpiresIn` (synctest: genau 15 min) |
| K6 | `abnahme_D9.ps1` baute mit `cmdkey` nur Haupteintrag und Teile 1–4 ab | Abbau über `account remove --json` (3.1), Kontrolle nur lesend mit `cmdkey /list` (3.2: keine Zeile `/<kennung>/<konto>`), Datenverzeichnis (3.3); `.md` Abschnitt 5 ebenso | Klammern gezählt (unverändert ausgeglichen), `pwsh` fehlt weiter |
| K7 | Quelle las das Token nur beim Start; zweiter Prozess erneuerte mit einem schon ersetzten Token | `Source.Reload` vor jedem Refresh: weicht das gespeicherte vom zuletzt gelesenen/selbst gespeicherten ab, wird es übernommen | `TestSourceReloadsNewerStoredToken`, `TestTwoProcessesShareRotatedToken` (Fake-AS mit Einmal-Token) |
| K8 | `oauth.refresh` hat `ok`/`error`, A03 S3.7 nennt `ok`/`failed` | als Abweichung festgehalten (Festlegung 18), Code unverändert | — |

Commits der Nachbesserung:

6. `fix: allow OAuth2 with the insecure file store only for test servers` (W1)
7. `fix: store split OAuth2 tokens under a new generation` (W2)
8. `fix: store no renewed OAuth2 token in a dry run` (K3)
9. `fix: pick a new sign-in port when ::1 is taken` (K4)
10. `fix: give the code exchange and device flow their own deadlines` (K5)
11. `fix: reread the stored OAuth2 token before a refresh` (K7)
12. `docs: tear down acceptance D9 with account remove` (K6)
13. `fix: delete old token parts when a shorter token replaces them` (eigener Fund beim Gegenlesen von W2: Ersetzte ein kurzes Token ein langes im Format von ms.1, blieben dessen Teile liegen; Test `TestSaveReplacesLegacyParts`, scheitert auf dem Stand davor)
14. `docs: record the T4 follow-up after the cross-check`

## 1b. Zweite Nachbesserung nach der Nachprüfung und Anschluss an `main` (Build `0.3.0-ms.3`)

Die Nachprüfung ergab PASS mit einer wichtigen Lücke (W) und drei kleineren Punkten. Behoben je Punkt mit eigenem Commit und Test, danach `main` `f536810` (T1a) zusammengeführt:

| Punkt | Befund | Behebung | Test |
|---|---|---|---|
| W | `Keyring.call` meldet nach 10 s Zeitüberschreitung, die Goroutine schreibt aber weiter. `Save` hielt den Haupteintrag für gescheitert und löschte die neuen Teile; kam das Schreiben danach an, zeigte der Haupteintrag auf eine gelöschte Generation (`Load`: `ErrTokenCorrupt`) | Neuer Fehler `secrets.ErrTimeout` (gehört weiter zu `ErrUnavailable`, unsicherer Ausgang). Läuft das Schreiben des Haupteintrags in die Zeitgrenze, löscht `Save` weder neue noch alte Teile und meldet den Fehler (`token_not_stored`). Jeder Haupteintrag nennt zusätzlich die Generation, die er abgelöst hat (`prev_gen`, `prev_parts`); das nächste Speichern räumt über alt/aktuell auch diese ab, `account remove` ebenso. Ein Lesefehler nach dem Schreiben zählt nicht mehr als „abgelöst“ (sonst hätte `Save` die eigenen gültigen Teile gelöscht) | `TestSaveMainTimeoutWriteArrivesLate` (Speicher, der verspätet schreibt: groß→groß, klein→groß, groß→klein, nie ankommend; scheitert auf dem Stand davor mit `ErrTokenCorrupt`), `TestKeyringTimeoutIsUncertain` (echtes `Keyring.call` mit hängendem Set, das danach ankommt) |
| K1 | Kommentar in `abnahme_D9.ps1` behauptete „alle Teile jeder Generation“ | Kommentar berichtigt (Haupteintrag, seine Generation, die davor abgelöste, Teile ohne Generation; ältere verwaiste findet niemand, der Schlüsselbund kann nicht auflisten). Prüfung 3.2 schreibt die übrigen Einträge ins Protokoll; `abnahme_D9.md` Abschnitt 5 erklärt, was bei FAIL in 3.2 zu tun ist (`cmdkey /delete` je Zeile, Zustimmung entziehen, Bewertung) | Klammern gezählt, kein `pwsh` |
| K2 | `TestConcurrentSavesStress` prüfte nur „nie korrupt“ | Prüfung ergänzt: am Ende nur die Teile des gültigen Haupteintrags. **Fund dabei:** Ohne weitere Änderung scheiterte das in 93 von 300 Läufen; der in Festlegung 5 als „selten“ beschriebene Rest ist unter echter Gleichzeitigkeit häufig. Behoben mit der dort genannten vollständigen Lösung: Alle Prozesse speichern über `oauth.SaveLocked` unter einer Sperrdatei `oauth-<verweis>.lock` neben der Datenbank (`internal/flock` aus T1a, deshalb nach dem Merge gebaut) | `TestConcurrentSavesStress` (unter Sperre, 200 Läufe sauber, auch unter `-race`), `TestSaveLockedWaitsForTheLock` (Warten, Zeitgrenze, ohne Ordner keine Sperre), `TestOAuth2AccountCLI` (Sperrdatei liegt neben der Datenbank) |
| K3 | `insecure_test_server` prüfte nur, ob der IMAP-Host lokal ist; ein Konto mit lokalem IMAP und echten Microsoft-Adressen hätte ein echtes Refresh-Token in `insecure-secrets/` gelegt | `oauthStoreAllowed` verlangt bei `insecure-file` zusätzlich, dass Anmeldeseite, Token- und Device-Code-Endpunkt auf localhost zeigen (nur der Fake-AS); geprüft in `accounts.add`, `accounts.login`, `Credentials.For` | `TestOAuth2InsecureFileNeedsLocalSignIn` (echte Microsoft-Adressen, fremde Adressen, nur Token-Endpunkt fremd: alle drei abgelehnt, kein Kontakt zum Anmeldeserver, nichts gespeichert; scheitert ohne die Prüfung) |
| Merge | `main` `f536810` (T1a samt Nachbesserung) | Konflikte in `internal/cli/doctor.go`, `docs/cli.md`, `docs/sicherheit.md`, `docs/architektur.md`; beide Seiten vollständig erhalten (siehe Commit 19) | `make check`, `make race`, `make integration` auf dem Stand nach dem Merge |

**Grenze des Modells (K2):** `TestConcurrentSavesStress` und `TestConcurrentSavesNeverCorrupt` laufen mit Goroutinen auf einem Speicher im RAM (`memStore`, Grenze 2.560 Byte wie Windows), nicht gegen einen echten Schlüsselbund und nicht mit getrennten Prozessen. Abgebildet sind die Reihenfolgen der Aufrufe und die Sperre (`flock` sperrt je geöffneter Datei, deshalb schließen sich auch Goroutinen mit je eigenem `TryLock` aus; unter Windows `LockFileEx` je Handle, in dieser Sitzung nicht unter Windows gelaufen). Nicht abgebildet: Zeitverhalten und Fehlerbilder des echten Windows-Anmeldeinformationsspeichers bzw. Secret Service, Abstürze mitten im Schreiben (der Prozess hält dann keine Sperre mehr, die Generationen schützen das Token), Zeitüberschreitungen außer im gezielten Test aus W.

Commits der zweiten Nachbesserung:

15. `fix: keep token parts when the keyring times out on the main entry` (W)
16. `docs: state what account remove cannot find in acceptance D9` (K1)
17. `fix: allow the insecure file store for OAuth2 only with a local sign-in` (K3)
18. `chore: merge main (T1a) into T4`
19. `fix: store OAuth2 tokens under a lock file and check for leftover parts` (K2; nach dem Merge, weil `internal/flock` aus T1a kommt)
20. `docs: record the second T4 follow-up and the merge with main`
21. `docs: record the test runs of build 0.3.0-ms.3` (dieser Stand)

## 2. Festlegungen (Einspruch möglich)

1. **`x/oauth2` v0.35.0** statt der neuesten v0.37.0: Die Version ist schon im Modulgraph (MCP-SDK) und von `make vuln` abgedeckt; ein Sprung hebt andere Abhängigkeiten mit an. Anheben, wenn `govulncheck` etwas meldet.
2. **Client-ID und Mandant ohne Migration in `accounts.sync_policy`** (JSON `{"oauth2":{"client_id","tenant"}}`; die Spalte war seit 0001 vorhanden und unbenutzt `{}`). T4 darf keine Migration schreiben (Rahmen 4). Eine spätere Migration kann sie in eigene Spalten ziehen.
3. **Mandant ohne Angabe `common`**; D9 nutzt `consumers` (private Konten).
4. **Gespeichert: Refresh-Token, Ablauf des letzten Access-Tokens, Zeitpunkt der Speicherung.** Folge: Jeder neue Prozess (`sync`, `account test`) erneuert einmal zu Beginn und schreibt das neue Refresh-Token (Microsoft ersetzt es); `sync --watch` erneuert etwa stündlich. ~~Auch ein Trockenlauf von `sync` erneuert und speichert.~~ *Aufgehoben nach der Gegenprüfung (K3):* Ein Trockenlauf von `account test` und `sync` erneuert das Access-Token (ohne Token keine Anmeldung), speichert das neue Refresh-Token aber nicht. Das ist sicher, weil Microsoft ein eingelöstes Refresh-Token nicht widerruft (Microsoft Learn, „Refresh tokens in the Microsoft identity platform“, abgerufen 27.10.2026); der Fake-AS prüft das mit `KeepUsedRefresh`. **Festlegung (Einspruch möglich):** Das Systemereignis `oauth.refresh` entsteht im Trockenlauf trotzdem, mit `"dry_run":true` in den Parametern, weil der Token-Endpunkt wirklich angesprochen wurde.
5. **Teilgröße 2.000 Byte, höchstens 32 Teile**, Verweise `oauthpart1g<generation>/<datadir-id>/<name>` usw. (die Form des Verweises bleibt gültig, der Kontoname wird nicht länger). *Geändert nach der Gegenprüfung (W2):* Generation = 8 zufällige Hex-Zeichen je Speichern; Reihenfolge neue Teile → Haupteintrag → alte Generation löschen; ein Fehler beim Schreiben löscht die schon geschriebenen neuen Teile wieder. Teile ohne Generation (`oauthpart<n>/…`, Format von `0.3.0-ms.1`) werden noch gelesen und beim nächsten Speichern bzw. von `account remove` gelöscht. ~~**Rest (Einspruch möglich):** Der Schlüsselbund kann nicht auflisten. Stellt ein zweiter Prozess genau zwischen dem letzten Lesen und dem Schreiben des Haupteintrags eines ersten Prozesses um, bleiben die Teile des zweiten liegen (ein abgelöstes Refresh-Token, das `account remove` nicht findet). Das Fenster ist ein einzelner Schreibaufruf, und K7 macht gleichzeitige Erneuerungen selten; eine prozessübergreifende Sperre wäre die vollständige Lösung und ist nicht gebaut.~~ *Geändert in der zweiten Nachbesserung (K2):* Das Fenster trat im Stresstest in rund jedem dritten Lauf auf; seit `0.3.0-ms.3` speichern alle Prozesse eines Datenverzeichnisses unter der Sperrdatei (Festlegung 21). **Verbleibender Rest (Einspruch möglich):** Teile bleiben nur noch liegen, wenn der Schlüsselbund beim Umstellen nicht binnen 10 s antwortet und das Schreiben danach nie oder erst nach dem nächsten vollständigen Speichern ankommt (W; das Token bleibt in jedem Fall lesbar), oder wenn ein älterer Build (`0.3.0-ms.1`/`ms.2`) ohne Sperre gleichzeitig speichert. Abhilfe dann nach `abnahme_D9.md` Abschnitt 5.
6. **Kein `POSTKAUZ_PASSWORD_<NAME>` für OAuth2-Konten**; gelesen wird nur der Speicher. *Geändert nach der Gegenprüfung (W1):* Der unsichere Testspeicher `insecure-file` meldet `Available: true` und zählt trotzdem nicht als Schlüsselbund; `account add --auth oauth2`, `account login` und die Tokenquelle (`account test`, `sync`) lehnen ihn ab (`secret_store_unavailable`), außer für Konten mit `insecure_test_server = true` (lokaler Testserver). Vorher landete ein echtes Refresh-Token in `insecure-secrets/secrets.json`.
7. **Speicherfehler:** Die Quelle versucht beim nächsten Aufruf nur das Speichern erneut, keinen zweiten Refresh.
8. **Systemereignis `oauth.refresh`:** Akteur `system`, Schlüssel `system`, Scope `mail:sync`, Ziel `account:<name>`, Ergebnis `ok` oder `error`, Grund = Code (`invalid_grant`, `token_not_stored`, `network`, `timeout`, `http_<status>` …). Es entsteht bei `account test` und `sync`, nicht bei `account login` (dort steht `accounts.login` im Audit).
9. **`accounts.login`:** R1, Scope `admin`, HumanOnly, kein MCP-Tool. Adresse und Code der Anmeldung gehen als Fortschritt (`oauth_prompt`) auf stderr, auch mit `--json`.
10. **Neue Fehlercodes:** `no_token`, `token_not_stored`, `oauth_refresh` (auch für ein unlesbares Token), `login_failed`; alle Exit 1.
11. **SASL:** `OAUTHBEARER` vor `XOAUTH2`, je nach CAPABILITY vor der Anmeldung (vorhandene Logik); Microsoft bietet `XOAUTH2`.
12. **`account remove`** löscht bei OAuth2-Konten den Haupteintrag, die Teile der Generation, die er nennt, die der davor abgelösten Generation (seit `ms.3`) und Teile ohne Generation, je bis 32 (berichtigt in der zweiten Nachbesserung, K1: „alle Teile“ stimmte nicht, verwaiste ältere Generationen findet es nicht); die Ausgabe heißt weiter `stored password deleted` (Text unverändert, ein bestehender Test hängt daran).
13. **Abnahme-Skript:** die zwei Anmeldungen brauchen ein sichtbares Fenster (Enter, Code); ohne `-Zustimmung "ja …"` läuft nur der Offline-Teil.

Neu mit der Nachbesserung (`0.3.0-ms.2`):

14. **Trockenlauf und Audit (K3):** `oauth.refresh` entsteht auch im Trockenlauf, mit `"dry_run":true` in den Parametern (der Token-Endpunkt wurde wirklich angesprochen); gespeichert wird nichts. Microsoft widerruft ein eingelöstes Refresh-Token nicht (Microsoft Learn, „Refresh tokens in the Microsoft identity platform“, abgerufen 27.10.2026); der Fake-AS bleibt standardmäßig strenger (Einmal-Token) und hat dafür `KeepUsedRefresh`.
15. **Port-Suche (K4):** höchstens 20 Versuche, dann Abbruch mit Fehler; „kein IPv6-Loopback“ heißt: `[::1]:0` lässt sich gar nicht binden. Neues Feld `oauth.Microsoft.Listen` nur für Tests.
16. **Fristen (K5):** Tausch des Codes 60 s ab Eingang der Anmeldung (`ExchangeTimeout`), unabhängig von den 5 min des Browser-Flows; Device-Code 15 min (`DeviceTimeout`), bei kürzerem `expires_in` gilt dieses. Netzfehler beim Tausch bleiben `error` (nicht `login_failed`), nur Zeitüberschreitungen werden `login_failed`.
17. **„Neuer“ (K7) heißt „anders als zuletzt gelesen oder selbst gespeichert“:** Der gespeicherte Wert ändert sich nur durch Schreiben; weicht er ab, hat danach jemand gespeichert. `UpdatedAt` (Sekundenauflösung) wird nicht verglichen. Ein Lesefehler vor dem Refresh zählt nicht (dann mit dem eigenen Token). Gegenseitiges Ausschließen zweier Prozesse (Sperre) ist nicht gebaut; siehe Rest bei Festlegung 5.
18. **Abweichung von A03 S3.7 (K8):** Das Ergebnis von `oauth.refresh` heißt `ok` oder **`error`** (mit Code im Grund), nicht `ok`/`failed`. Grund: `audit.Result` kennt nur `ok`, `denied`, `needs_approval`, `error`; ein eigener Wert `failed` nur für dieses Ereignis würde Auswertungen (`audit --json`, D9-Schritt 2.10, Export) uneinheitlich machen. Wer `failed` sucht, sucht `result = error AND operation = oauth.refresh`.

Neu mit der zweiten Nachbesserung (`0.3.0-ms.3`):

19. **Unsicherer Ausgang (W):** Nur eine Zeitüberschreitung beim Schreiben des **Haupteintrags** gilt als unsicher; dann löscht `Save` nichts. Läuft das Schreiben eines **Teils** in die Zeitgrenze, löscht `Save` die neuen Teile weiter (der Haupteintrag zeigt noch nicht auf sie, das Token bleibt also lesbar; kommt das Teil später an, bleibt höchstens dieses eine Teil liegen). Der Fehlertext enthält `oauth: store token (outcome unknown, nothing deleted): secrets: secret store unavailable: no answer in time (keyring did not answer within 10s)`, Code `token_not_stored`.
20. **`prev_gen`/`prev_parts` im Haupteintrag (W):** neues, optionales Feld im Format `v:1` (ältere Builds lesen es nicht und stören sich nicht daran); es nennt die Generation, die beim Umstellen im Haupteintrag stand. Mehrkosten: je Speichern bis zu drei zusätzliche Löschaufrufe für schon gelöschte Teile.
21. **Token-Sperre (K2):** Datei `oauth-<verweis mit _ statt />.lock` neben der Datenbank (wie `sync-<id>.lock`), exklusiv, Warten höchstens 60 s in Schritten von 20 ms, danach `token_not_stored` mit `another process is storing the token of this account`. Gesperrt wird nur das Speichern (`account login`, Erneuerung in `account test`/`sync`), nicht das Lesen und nicht der Refresh selbst (K7 deckt das Neu-Lesen ab). `account remove` sperrt nicht: Speichert gleichzeitig ein anderer Prozess (etwa ein laufendes `sync --watch`), kann ein Eintrag im Schlüsselbund liegen bleiben; Kontrolle wie D9 Prüfung 3.2 (Rest, Einspruch möglich; `account remove` verweigert ohnehin, solange ein Sync die Kontosperre hält, T1a). Ohne Datenbankpfad (`:memory:`, Tests) keine Sperre.
22. **Lokale Anmeldeadressen (K3):** „lokal“ heißt `localhost`, 127.0.0.0/8 oder `::1` (dieselbe Prüfung wie für den IMAP-Host, `ops.IsLoopbackHost`), für alle gesetzten Adressen (Anmeldeseite, Token, Device-Code). Im Produktivbetrieb setzt niemand die Adressen (nur Tests über `oauth.Microsoft.Endpoint`); `insecure-file` ist für OAuth2 damit außerhalb der Tests immer abgelehnt.

## 3. Prüfungen in dieser Sitzung

| Prüfung | Ergebnis |
|---|---|
| `make check` vor jedem Commit (im eigenen Worktree auf dem Commit) | grün |
| `make race` | grün (20 min, auf Commit 2; Commits 3 bis 5 ändern nur Doku und eine Ausgabezeile, `make check` darauf grün) |
| `make integration` (Docker: Dovecot, Stalwart, GreenMail; `dockerd` von Hand gestartet) | **ein Fehlschlag, nicht von T4:** `TestIntegrationImportOverlapDovecot` (Abschnitt 7); alle übrigen Integrationstests grün, darunter `internal/provider/imap` und `internal/mailsync` gegen alle drei Server |
| `make vuln` | **nicht gelaufen:** Die Netzregel der Sitzung sperrt `vuln.go.dev` (Proxy 403). `x/oauth2` v0.35.0 war schon vorher im Modulgraph; die Prüfsitzung führt `make vuln` aus |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make release VERSION=0.3.0-ms.1` | gebaut auf dem letzten Commit; `dist/` ist nicht im Repository, die Prüfsitzung baut nach und vergleicht `SHA256SUMS` |
| PowerShell-Syntax von `abnahme_D9.ps1` | **nicht geprüft**: kein `pwsh` im Container, das Herunterladen hat die Sitzung nicht erlaubt. Klammern gezählt (ausgeglichen), Offline-Kommandos mit dem Linux-Programm nachgestellt. Die Prüfsitzung prüft vor dem Lauf auf dem Büro-PC: `[System.Management.Automation.Language.Parser]::ParseFile('abnahme_D9.ps1', [ref]$null, [ref]$e); $e` (leer = gut) und PSScriptAnalyzer, falls vorhanden. |

**Nachbesserung `0.3.0-ms.2`** (gleiche Sitzungsart, Toolchain go1.27.3 nach der Ausnahme in `CLAUDE.md`):

| Prüfung | Ergebnis |
|---|---|
| `make check` je Commit 6 bis 14 (eigener Worktree auf dem Commit) | grün |
| `go test -race` für `internal/oauth` (dreimal, mit den Nebenläufigkeitstests aus W2) | grün; ein voller `make race` lief in der Nachbesserung **nicht** (Auftrag: `make check` und `make cross`); die Prüfsitzung führt ihn beim Merge aus |
| `make integration`, `make vuln` | nicht erneut gelaufen (Auftrag; `make vuln` ohnehin gesperrt, siehe oben); die Nachbesserung berührt IMAP und Abhängigkeiten nicht |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make release VERSION=0.3.0-ms.2` | gebaut auf dem letzten Commit (`dist/` nicht im Repository) |
| `abnahme_D9.ps1` | weiterhin nur Klammern gezählt (Verhältnis wie vorher), kein `pwsh`; Prüfung vor dem Lauf wie oben |

**Zweite Nachbesserung `0.3.0-ms.3`** (gleiche Sitzungsart, Toolchain go1.27.3 nach der Ausnahme in `CLAUDE.md`; Stand nach dem Merge mit `main` `f536810`):

| Prüfung | Ergebnis |
|---|---|
| `make check` je Commit 15 bis 19 (W, K1, K3 je im eigenen Worktree auf dem Commit; Merge und K2 im Arbeitszweig) | grün; Commit 20 (nur Doku) mit `make pii-check guard`, Commit 21 mit vollem `make check` |
| `make race` (auf Commit 19, Code-Endstand) | grün, 1.272 s (21 min), 29 Pakete, kein `DATA RACE` |
| `TestConcurrentSavesStress` einzeln | 200 Läufe sauber (ohne Sperre vorher 93 von 300 mit verwaisten Teilen) |
| `make integration` (Docker: Dovecot 2.4.5, Stalwart v0.11.8, GreenMail 2.1.14; `dockerd` von Hand, Images über `mirror.gcr.io`, weil Docker Hub mit 429 ablehnte; Tags unverändert) | **grün**, 161 s: alle 9 Integrationstests, je gegen alle drei Server, darunter `internal/provider/imap`, `internal/mailsync`; `TestIntegrationImportOverlapDovecot` jetzt grün (T1a-Nachbesserung W3 berücksichtigt die NUL-Umschreibung von Dovecot, `307/171/3/0`) |
| `make vuln` | nicht gelaufen (Netzregel sperrt `vuln.go.dev`, wie zuvor); keine neue Abhängigkeit (`internal/flock` ist eigenes Paket aus T1a) |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make release VERSION=0.3.0-ms.3` | gebaut auf dem letzten Commit (`dist/` nicht im Repository) |
| `abnahme_D9.ps1` | Klammern gezählt, kein `pwsh`; Prüfung vor dem Lauf wie oben |

## 4. CHANGELOG-Zeilen (für „Unreleased“, Abschnitt Added)

- S3.7 (T4): Microsoft-Postfächer per OAuth2 („bring your own client“): `account add <name> --auth oauth2 --client-id <id> [--tenant common|consumers|organizations|<id>]`, `account login <name> [--device-code]` (nur für Menschen, kein MCP-Tool); Anmeldung an IMAP per SASL `XOAUTH2`/`OAUTHBEARER`, `account test` nennt das Verfahren. Im Schlüsselbund liegt nur das Refresh-Token (bei Bedarf in Teilen unter `oauthpart<n>g<generation>/…`, je Speichern eine neue Generation, damit ein abgebrochenes Schreiben, eine Zeitüberschreitung des Schlüsselbunds oder ein zweiter Prozess das gespeicherte Token nie zerstört; gespeichert wird unter der Sperrdatei `oauth-<verweis>.lock` neben der Datenbank, damit keine Teile verwaisen), das Access-Token bleibt im Speicher und wird 2 min vor Ablauf erneuert (vorher liest der Prozess ein inzwischen von einem anderen Prozess gespeichertes Token neu); jeder Refresh ist das Systemereignis `oauth.refresh`; ein Trockenlauf speichert kein erneuertes Token. Ohne Schlüsselbund abgelehnt (R2), der unsichere Testspeicher `insecure-file` ebenso (außer für lokale Testserver mit lokalem Anmeldeserver, also nur in Tests). Anmeldung: Port auf 127.0.0.1 und ::1, Tausch des Codes mit eigener Frist 60 s, Device-Code höchstens 15 min. Neue Fehlercodes `no_token`, `token_not_stored`, `oauth_refresh`, `login_failed`. `doctor` zeigt Token-Stand und -Ablauf, `doctor --secrets` sucht das Refresh-Token. Anleitung `docs/betrieb_microsoft.md`, Abnahme `docs/abnahme_D9.md`/`.ps1`.
- `secrets.RedactTokens` schwärzt Token-Felder und JWTs in freiem Text; Fehler des Token-Endpunkts tragen nur Code und AADSTS-Nummer.
- `golang.org/x/oauth2` v0.35.0 ist direkte Abhängigkeit.
- Der Schlüsselbund meldet eine Zeitüberschreitung als `secrets.ErrTimeout` (unsicherer Ausgang: das Schreiben kann noch ankommen).

Roadmap-Zeile (für „Stand S3“): S3.7 gebaut (T4, Build `0.3.0-ms.3` nach Gegen- und Nachprüfung, mit `main` `f536810` zusammengeführt), Haltepunkt Abnahme D9 (Prüfsitzung, Büro-PC, nur lesend); App-Registrierung ungeprüft.

## 5. Hinweise für den Merge

- **Stand `0.3.0-ms.3`:** T1a (`main` `f536810`) ist schon im Zweig (Commit 18); der Zweig lässt sich ohne Konflikt auf diesen Stand von `main` zusammenführen. Neue Schnittstellen: `ops.AccountsAdd(repo, sec, ms oauth.Microsoft)`, `ops.Deps.TokenLockDir`, `ops.Credentials.LockDir`, `oauth.SaveLocked`, `oauth.LockName`, `secrets.ErrTimeout`. Wer ein OAuth2-Token speichert, nimmt `oauth.SaveLocked`, nie `oauth.Save` direkt.
- **Neu mit `0.3.0-ms.2`:** `ops.Credentials.For` nimmt jetzt `(a store.Account, dryRun bool)`; `oauth.PartRef` nimmt `(ref, gen string, i int)`; `oauth.Microsoft` hat die Felder `Listen`, `ExchangeTimeout`, `DeviceTimeout`, `oauth.Source` das Feld `Reload`. Teile, die `For` aufrufen (T1b: `sync.run`/Jobs), reichen `call.DryRun` durch.

- Reihenfolge laut Plan: T1a → T1c → **T4** → T1b → T2 → T3 → T5. Erwartete Konflikte (T1a erledigt): `internal/store/accounts.go` (`accountCols`, `scanAccount`; T2 ergänzt `drafts_mailbox` mit A5.1), `internal/ops/accounts.go`, `internal/ops/catalog.go` (`Deps`), `internal/cli/exit.go`, `internal/cli/bindings.go`, `docs/cli.md`, `docs/sicherheit.md`. Fachlich unabhängig.
- Die Schnittstellen `ops.SyncRunner.Sync`, `ops.AccountProber.Probe` und `mailsync.Runner.Connect` nehmen jetzt `provider.Credential` statt `secrets.Secret`; Teile, die neue Testfakes dafür schreiben (T1a, T1b), passen die Signatur an (`provider.PasswordCredential(pw)`).
- T1b (A1, Variante A): `oauth.refresh` ist ein einzelner Systemeintrag außerhalb des Executors (wie `drafts.remote_check`); der Vertragstest bleibt bei „ein Eintrag bei Ablehnung, zwei bei Erlaubnis“ für `accounts.login`.
- T1c (A3, neuer Passwortleser): `account login` liest nichts von der Tastatur; die Device-Code-Anzeige geht über den Fortschritt auf stderr. Nichts anzupassen.
- T2 legt `docs/betrieb.md` an; `docs/betrieb_microsoft.md` dort als Abschnitt „Microsoft“ einfügen (die Datei selbst kann dann entfallen; Verweise in `docs/cli.md`, `docs/sicherheit.md`, `README.md`, `docs/abnahme_D9.md` nachziehen).

## 6. Prüfergebnisse vor dem Push

Siehe Abschnitt 3.

## 7. Fund außerhalb von T4

*Erledigt mit dem Merge von T1a (`0.3.0-ms.3`):* `TestIntegrationImportOverlapDovecot` ist grün; T1a hat die Erwartung an die NUL-Umschreibung von Dovecot angepasst (Commit `e0cb1f7`, W3). Ursprünglicher Befund:

`TestIntegrationImportOverlapDovecot` (`internal/importer`) scheitert gegen das frische Dovecot im Container: `new 307, existing 171, skipped 3, errors 0`, erwartet `305/173/3/0` (zwei überlappende Nachrichten werden nicht als vorhanden erkannt). **Auf dem Ausgangsstand `1c9847a` identisch** (eigener Worktree, gleicher Lauf), also nicht durch T4 verursacht; T4 berührt den Import nicht. Vermutung (ungeprüft): Das Dovecot-Image hat sich geändert und schreibt zwei der Nachrichten anders ab (z. B. die ohne Message-ID, die nur über `content_hash` erkannt werden). Nicht in T4 behoben (anderer Teil, kein Anlass zum Verbreitern). **Vorschlag:** Die Prüfsitzung prüft Image-Tag und -Datum in `deploy/testmail/docker-compose.yml` gegen den letzten grünen Lauf (S2.9, `docs/protokolle/S29_pruefungen.txt`) und gibt den Punkt an T1a oder T1c.

## 8. Offene Fragen an den Betreiber (mit Standardantwort)

1. **Darf ein privates Microsoft-Konto ohne Azure-Verzeichnis eine App registrieren?** Ungeprüft. **Default:** In D9 ausprobieren; verlangt das Portal ein Verzeichnis, legst du ein kostenloses Azure-Konto an (ungeprüft, ob dafür eine Zahlungsangabe nötig ist), oder wir nehmen dein Firmenkonto mit `--tenant organizations`.
2. **Konto nach D9 behalten?** **Default:** nein, Abbau nach Abschnitt 5 von `abnahme_D9.md`; für den Dauerbetrieb wird das Konto nach der Freigabe von 0.3.0 im Live-Datenverzeichnis neu angelegt.
3. **Client-ID und Mandant später in eigene Spalten?** **Default:** ja, mit der nächsten Migration nach dem Merge (T1b oder T2), ohne Eile; bis dahin `accounts.sync_policy`.
