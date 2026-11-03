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
| `cb9c2c9` | Nachbesserung Teil 1/2 nach der Gegenprüfung (Nachtrag 2, Abschnitt 1a); **Build-Commit von 0.3.0-rc2** |

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
| `make check` (gofmt, `go vet`, `go test ./...` ohne `-short`, Guards, PII-Gate) | grün, vor jedem Commit, zuletzt an `cb9c2c9` |
| `make cross` (linux/amd64, windows/amd64, darwin/arm64) | grün, zuletzt an `cb9c2c9` |
| `make integration IT=dovecot,stalwart,greenmail` an `cb9c2c9` und vorher an `8175b51` | je grün (Exit 0): 41 PASS, 0 FAIL, 1 SKIP (`TestIntegrationAuthelia`, nicht gewählt). Vorher an `53216cd`…`f90fa9e`: 40 PASS, 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit wie in S3.8). `dockerd` von Hand gestartet; Docker Hub lieferte `429 Too Many Requests`, die drei Images kamen unverändert (gleiche Tags) über `mirror.gcr.io` |
| Race-Detector | `go test -race` für `internal/remote`, `internal/config` und die A04-Tests in `internal/cli`, `internal/httpmcp`, `internal/ops` grün; das volle `make race` nicht gelaufen (nicht Teil des Auftrags) |
| `make vuln` | nicht gelaufen (in der Cloud gesperrt) |
| Rauchtest Teil 4 mit dem Linux-Programm aus dem Build | `mcp --http` (Büro-PC) und `remote add`/`remote test`/`mcp --remote` (zweiter PC) in getrennten Verzeichnissen: `tools: 20`, `key: key:zweiter-pc (role assistant)`, `initialize` und `mail_search` über die Brücke beantwortet, der Schlüssel in keiner Datei im Klartext |

Neue Tests Teil 4: `internal/remote` (`TestBridgeStatelessJSON`, `TestBridgeStatefulSSE` mit Session-ID, SSE, GET-Strom, 404-Neuanmeldung und Abbruch, `TestBridgeErrors`, `TestCheck`), `TestA04BridgeAgainstHTTPServer` (`internal/httpmcp`, gegen `mcp --http` in-process: Tools wie über stdio, Suche, Audit `transport: http`, falscher Schlüssel, Kanarie in Log, Fehler und Audit), `TestA04RemoteCommands`, `TestA04MCPRemoteBridge` (kein Datenverzeichnis), `TestA04DoctorShowsRemote` (`internal/cli`), `TestA04RemoteAddVerifiesAndRollsBack` (`internal/ops`), `TestA04CheckRemoteURL`, `TestA04RemoteFileEdits`, `TestA04RemoteErrDoesNotStopLoad` (`internal/config`); Vertragszeilen `remote.*` (Register und CLI), `notOverMCP`, Fehlercodes in `TestExitCodesMatchDocs`.

Neue Tests Teil 1 und 2 (je vorher rot, nachher grün geprüft): `TestA04ForwardCarriesThreadHeaders`, `TestA04ForwardsToCorpus` (1.000 Weiterleitungen, alle im Thread des Originals), `TestA04ForwardDraftStaysInThread` (Executor bis zur abgelegten Kopie), `TestA04ReplyWithoutDerivedRecipients`, `TestA04ReplyWithoutDerivedRecipientsCLI`; angepasst: `TestForward`, Vertragszeile `drafts.create` (`internal/ops`), CLI-Vertragsprobe `draft reply` mit `--no-derive-recipients`.

## 3. Build 0.3.0-rc2

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags (`git clone --no-tags`), je `git checkout cb9c2c9` (Teil 1 bis 4 und die Nachbesserung), `git status --short` leer, `make release VERSION=0.3.0-rc2`.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`, sie enthält die Bauzeit).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103085737-cb9c2c9cb7a5`, `vcs.revision=cb9c2c9cb7a53cff0a4349e1bf8a786254f69517`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc2 (go1.27.3 linux/amd64)`.

```
601a7025f08ad44fd839ff69e40efcfbeb6a80fd826440f4e389bc94b451e418  postkauz-windows-amd64.exe
3508a19b1b7634fd208978cd3c02210294490d302bb62cf6afc2b9034b4ae4f4  postkauz-linux-amd64
efc9330d1a02748bb8a7424f6923d09e376d5748917c9e704542ed648e6fa6ea  postkauz-darwin-arm64
```

Windows-Programm: 22.579.200 Byte. Ersetzt sind die beiden früheren rc2-Builds an `f90fa9e` (Teil 1 bis 3, Windows `3b33dc00…6625b3`) und an `8175b51` (Teil 1 bis 4, Windows `87c98c74…5af97b`); beide nicht verwenden.

## 4. Offen

- Probe des Betreibers mit rc2 im echten Gmail-Postfach (Vorgang 4): Weiterleitung anlegen, im Browser senden, Verlauf prüfen; Antwort mit `--no-derive-recipients`.
- Offene Frage 1 der Roadmap (VPS und D8 Teil D ruhen).
- Probe Vorgang 6 Phase 2: Claude Desktop am zweiten PC nach [betrieb.md](../betrieb.md) 4.9 (Pfad und Menüweg von `claude_desktop_config.json` aus Anleitungen Dritter, Stand November 2026; bei Abweichung bitte melden).
- `mcp --remote` ist nur unter Linux gelaufen; unter Windows nur kompiliert (Schlüsselbund, conhost-Aufgabe für `mcp --http`).
- Ältere, schon freigegebene Weiterleitungs-Entwürfe fallen beim ersten Push mit rc2 auf `draft` zurück (Entschiedene Fragen 70).
