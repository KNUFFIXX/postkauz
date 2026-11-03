# A04 „Alltag“: Stand, Prüfläufe, Build 0.3.0-rc2

Auftrag [auftraege/A04_alltag.md](../../auftraege/A04_alltag.md) (Betreiber-Sitzung DRACO-PK12, 03.11.2026). Teil 4 als Nachtrag des Betreibers am selben Tag. Zweig `claude/auftrag-a04-alltag-2jwww9`, Basis `main` `6c7ef13` (v0.3.0-rc1 + Doku). Kein Tag, kein Release, kein Merge.

## 1. Commits

| Commit | Inhalt |
|---|---|
| `e681575` | Auftrag A04 wörtlich abgelegt |
| `53216cd` | Teil 1: Weiterleitungen tragen `In-Reply-To`/`References` (E11 geändert, Entschiedene Fragen 70) |
| `dec07df` | Teil 2: `derive_recipients` für `drafts.create`, `draft reply --no-derive-recipients` (Entschiedene Fragen 71) |
| `1c74379` | Teil 3: Abnahmemaßstab nach dem Lastenheft vom 03.11.2026, Offene Frage 1 |
| `f90fa9e` | Roadmap-Stand und CHANGELOG (erster Build von 0.3.0-rc2 mit Teil 1 bis 3, ersetzt, siehe Abschnitt 3) |
| `0ad5911` | dieses Protokoll, erste Fassung |
| `8175b51` | Teil 4 (Nachtrag): `mcp --remote` und `remote add|list|remove|test` (zweiter Build von 0.3.0-rc2, ersetzt) |
| `c1f10d3` | Protokoll zu Teil 4 |
| `cb9c2c9` | Nachbesserung Teil 1/2 nach der Gegenprüfung (Nachtrag 2, Abschnitt 1a; dritter Build von 0.3.0-rc2, ersetzt) |
| `2b33dce` | Protokoll zum dritten Build |
| `7231758` | Nachbesserung Teil 4 nach der Gegenprüfung (Nachtrag 3, Abschnitt 1b); **Build-Commit von 0.3.0-rc2** |

## 1a. Nachbesserung Teil 1/2 nach der Gegenprüfung (Nachtrag 2)

Gegenprüfung (Opus) der Commits bis `0ad5911`: PASS, kein Blocker. Umgesetzt:

- **W1:** Beschreibung von `mail_create_draft`, `docs/cli.md` (`draft reply`) und `docs/mcp.md`: genannte Empfänger kommen zu den abgeleiteten hinzu; `derive_recipients=false` (nur reply/reply_all): nur die genannten, auch keine.
- **K1:** `derive_recipients: false` bei `new`/`forward` → `invalid_input` (`derive_recipients: false is allowed only with kind reply or reply_all`, kein Wert in der Meldung); `true` bleibt erlaubt.
- **K2:** Upgrade-Folgen von Teil 1 für alte, offene Weiterleitungs-Entwürfe dokumentiert und hingenommen (CHANGELOG, Entschiedene Fragen 70): `ready` fällt beim Push zurück; `pending_approval` scheitert schon bei `approve` mit `approval_stale`; `drafts.get` zeigt leere `in_reply_to`/`references` bis zum nächsten `drafts.update`; `draft show --eml` meldet „changed after the copy“ ohne menschliche Änderung.
- **K3:** `docs/mcp.md` bei `mail_get_thread`: Weiterleitungen (und ihre Entwurfskopie) stehen jetzt im Thread des Originals; Unterscheidung über Betreffpräfix und Empfänger.
- **K4:** `redactDraftInput` schreibt `params.derive_recipients` (Wahrheitswert) ins Audit.
- **K5:** Tests `TestA04ForwardWithoutMessageIDKeepsReferences` (Original ohne Message-ID mit References: References ohne In-Reply-To, wie bei reply) und `TestA04ReviewDeriveRecipients` (K1, K4, `drafts.update` ohne `to` behält die leere Empfängerliste).

## 1b. Nachbesserung Teil 4 nach der Gegenprüfung (Nachtrag 3)

Gegenprüfung (Opus) von `8175b51`: PASS, kein Blocker. Umgesetzt:

- **W1:** `docs/betrieb.md` 4.9, zweiter PC: durchgängig `--config C:\PostKauz\config.toml` (und `--data-dir C:\PostKauz\data`) bei `remote add`/`remote test`, in `claude_desktop_config.json` `"args": ["mcp", "--remote", "buero", "--config", "C:\\PostKauz\\config.toml"]`; README ebenso. `mcp --remote` schreibt die Umleitungswarnung auf stderr (`config.RemoteRedirectWarnings`: umgeleitete Konfigurationsdatei oder Konfiguration unter `%APPDATA%`/`%LOCALAPPDATA%` in einem App-Paket); fehlt die Konfigurationsdatei, nennt `remote not found` ihren Pfad und `--config`.
- **W2:** Benachrichtigungen und Antworten des Clients über eine eigene geordnete Warteschlange (eine Goroutine); `cancelCall` vor dem Einreihen. Test `TestReview4HangingNotificationDoesNotBlockPing` (ping nach hängender Benachrichtigung in unter 2 s).
- **K1:** Panic in der Anfrage-Goroutine → `-32603` mit derselben `id` (`TestReview4PanicAnswersInternalError`, Text ohne Inhalt des Panics).
- **K5:** `remote add` liest den vorherigen Schlüssel immer und stellt nach einem Schreibfehler genau diesen Stand her (ohne vorherigen Schlüssel: gelöscht, auch wenn der Abschnitt schon stand); Test vorher rot, nachher grün.
- **K6:** `TestReview4SelfSignedCertificate` (`-32010`, „TLS certificate not accepted“), `TestReview4TooLargeAnswer` (über 16 MiB als JSON und SSE), `TestReview4BatchInAnswer`, `TestReview4StdinEndWithHangingServer` (Ende nach 10,1 s; unter `-short` übersprungen), `TestA04RemoteURLLoopbackEdges` (abgelehnt `127.1`, `localhost.`, `foo.localhost`, `0.0.0.0`, `127.0.0.1.nip.io`, `fe80::1%lo`, `127.0.0.1@evil.com`; erlaubt `localhost`, `[::1]`, `127.0.0.2`), `TestA04RemoteRedirectWarnings`.
- **K7:** Hinweis `HTTPS_PROXY`/`NO_PROXY` (`.ts.net`) in 4.9.
- **K8:** `Config.Remote` mit `json:"-"`.
- **K9:** CHANGELOG: ältere Versionen halten an `[remote]` an; Rückfall nur mit entfernten Abschnitten.
- **K2 bis K4:** nur als bekannte Grenze in `docs/mcp.md` (Wettlauf bei der Neuanmeldung, GET-Strom nach 404, Wiederaufnahme eines SSE-Stroms mit `Last-Event-ID`).

## 2. Prüfläufe

Umgebung: Cloud-Sitzung, Go 1.27.3 (einmal über `GOTOOLCHAIN=go1.27.3 go version` geholt, danach `GOTOOLCHAIN=local`, Ausnahme laut CLAUDE.md).

| Lauf | Ergebnis |
|---|---|
| `make check` (gofmt, `go vet`, `go test ./...` ohne `-short`, Guards, PII-Gate) | grün, vor jedem Commit, zuletzt an `7231758` (das PII-Gate erst mit `scripts/pii-allow.txt` für den URL-Testfall `127.0.0.1@evil.com`) |
| `make cross` (linux/amd64, windows/amd64, darwin/arm64) | grün, zuletzt an `7231758` |
| `make integration IT=dovecot,stalwart,greenmail` an `7231758` | grün (Exit 0): 40 PASS, 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit). An `cb9c2c9` und `8175b51` je 41 PASS, 0 FAIL, 1 SKIP. Vorher an `53216cd`…`f90fa9e`: 40 PASS, 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit wie in S3.8). `dockerd` von Hand gestartet; Docker Hub lieferte `429 Too Many Requests`, die drei Images kamen unverändert (gleiche Tags) über `mirror.gcr.io` |
| Race-Detector | an `7231758`: `go test -race -short` für `internal/remote`, `internal/config` und `-race` für die A04- und Remote-Tests in `internal/cli`, `internal/httpmcp`, `internal/ops` grün; vorher ebenso für `internal/remote`, `internal/config` und die A04-Tests in `internal/cli`, `internal/httpmcp`, `internal/ops` grün; das volle `make race` nicht gelaufen (nicht Teil des Auftrags) |
| `make vuln` | nicht gelaufen (in der Cloud gesperrt) |
| Rauchtest Teil 4 mit dem Linux-Programm aus dem Build (zuletzt an `7231758`, mit `ping`) | `mcp --http` (Büro-PC) und `remote add`/`remote test`/`mcp --remote` (zweiter PC) in getrennten Verzeichnissen: `tools: 20`, `key: key:zweiter-pc (role assistant)`, `initialize` und `mail_search` über die Brücke beantwortet, der Schlüssel in keiner Datei im Klartext |

Neue Tests Teil 4: `internal/remote` (`TestBridgeStatelessJSON`, `TestBridgeStatefulSSE` mit Session-ID, SSE, GET-Strom, 404-Neuanmeldung und Abbruch, `TestBridgeErrors`, `TestCheck`), `TestA04BridgeAgainstHTTPServer` (`internal/httpmcp`, gegen `mcp --http` in-process: Tools wie über stdio, Suche, Audit `transport: http`, falscher Schlüssel, Kanarie in Log, Fehler und Audit), `TestA04RemoteCommands`, `TestA04MCPRemoteBridge` (kein Datenverzeichnis), `TestA04DoctorShowsRemote` (`internal/cli`), `TestA04RemoteAddVerifiesAndRollsBack` (`internal/ops`), `TestA04CheckRemoteURL`, `TestA04RemoteFileEdits`, `TestA04RemoteErrDoesNotStopLoad` (`internal/config`); Vertragszeilen `remote.*` (Register und CLI), `notOverMCP`, Fehlercodes in `TestExitCodesMatchDocs`.

Neue Tests Teil 1 und 2 (je vorher rot, nachher grün geprüft): `TestA04ForwardCarriesThreadHeaders`, `TestA04ForwardsToCorpus` (1.000 Weiterleitungen, alle im Thread des Originals), `TestA04ForwardDraftStaysInThread` (Executor bis zur abgelegten Kopie), `TestA04ReplyWithoutDerivedRecipients`, `TestA04ReplyWithoutDerivedRecipientsCLI`; angepasst: `TestForward`, Vertragszeile `drafts.create` (`internal/ops`), CLI-Vertragsprobe `draft reply` mit `--no-derive-recipients`.

## 3. Build 0.3.0-rc2

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags (`git clone --no-tags`), je `git checkout 7231758` (Teil 1 bis 4 und beide Nachbesserungen), `git status --short` leer, `make release VERSION=0.3.0-rc2`.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`, sie enthält die Bauzeit).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103092623-7231758868b0`, `vcs.revision=7231758868b09802222fd1b28825deaa20bbc1fe`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc2 (go1.27.3 linux/amd64)`.

```
824864b533d11b0cde6fc44ddd80c1d5d4362b58da632732f8cbe6c7d6a82eb5  postkauz-windows-amd64.exe
aa99d4a2f8b834c6b3865e5db57ad4e5552f257829363b938fa967a43e36728d  postkauz-linux-amd64
7a267cb23fbcd803da2fa96d0153de06f315b808650ac70328fca7190040bf56  postkauz-darwin-arm64
```

Windows-Programm: 22.586.880 Byte. Ersetzt sind die früheren rc2-Builds an `f90fa9e` (Windows `3b33dc00…6625b3`), `8175b51` (`87c98c74…5af97b`) und `cb9c2c9` (`601a7025…b451e418`); keinen davon verwenden.

## 4. Offen

- Probe des Betreibers mit rc2 im echten Gmail-Postfach (Vorgang 4): Weiterleitung anlegen, im Browser senden, Verlauf prüfen; Antwort mit `--no-derive-recipients`.
- Offene Frage 1 der Roadmap (VPS und D8 Teil D ruhen).
- Probe Vorgang 6 Phase 2: Claude Desktop am zweiten PC nach [betrieb.md](../betrieb.md) 4.9 (Pfad und Menüweg von `claude_desktop_config.json` aus Anleitungen Dritter, Stand November 2026; bei Abweichung bitte melden).
- `mcp --remote` ist nur unter Linux gelaufen; unter Windows nur kompiliert (Schlüsselbund, conhost-Aufgabe für `mcp --http`, Umleitungswarnung im App-Paket; `TestA04RemoteRedirectWarnings` bildet die Umleitung nur mit Symlinks nach).
- Bekannte Grenzen bei zustandsbehafteten Servern (K2 bis K4, `docs/mcp.md`); `mcp --http` ist zustandslos und nicht betroffen.
- Ältere, schon freigegebene Weiterleitungs-Entwürfe fallen beim ersten Push mit rc2 auf `draft` zurück (Entschiedene Fragen 70).
