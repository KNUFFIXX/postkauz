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
| `8175b51` | Teil 4 (Nachtrag): `mcp --remote` und `remote add|list|remove|test`; **Build-Commit von 0.3.0-rc2** |

## 1a. Nachbesserung Teil 1/2 nach der Gegenprüfung (Nachtrag 2)

Gegenprüfung (Opus) der Commits bis `0ad5911`: PASS, kein Blocker. Umgesetzt:

- **W1:** Beschreibung von `mail_create_draft`, `docs/cli.md` (`draft reply`) und `docs/mcp.md`: genannte Empfänger kommen zu den abgeleiteten hinzu; `derive_recipients=false` (nur reply/reply_all): nur die genannten, auch keine.
- **K1:** `derive_recipients: false` bei `new`/`forward` → `invalid_input` (`derive_recipients: false is allowed only with kind reply or reply_all`, kein Wert in der Meldung); `true` bleibt erlaubt.
- **K2:** Upgrade-Folgen von Teil 1 für alte, offene Weiterleitungs-Entwürfe dokumentiert und hingenommen (CHANGELOG, Entschiedene Fragen 70): `ready` fällt beim Push zurück; `pending_approval` scheitert schon bei `approve` mit `approval_stale`; `drafts.get` zeigt leere `in_reply_to`/`references` bis zum nächsten `drafts.update`; `draft show --eml` meldet „changed after the copy“ ohne menschliche Änderung.
- **K3:** `docs/mcp.md` bei `mail_get_thread`: Weiterleitungen (und ihre Entwurfskopie) stehen jetzt im Thread des Originals; Unterscheidung über Betreffpräfix und Empfänger.
- **K4:** `redactDraftInput` schreibt `params.derive_recipients` (Wahrheitswert) ins Audit.
- **K5:** Tests `TestA04ForwardWithoutMessageIDKeepsReferences` (Original ohne Message-ID mit References: References ohne In-Reply-To, wie bei reply) und `TestA04ReviewDeriveRecipients` (K1, K4, `drafts.update` ohne `to` behält die leere Empfängerliste).

## 2. Prüfläufe

Umgebung: Cloud-Sitzung, Go 1.27.3 (einmal über `GOTOOLCHAIN=go1.27.3 go version` geholt, danach `GOTOOLCHAIN=local`, Ausnahme laut CLAUDE.md).

| Lauf | Ergebnis |
|---|---|
| `make check` (gofmt, `go vet`, `go test ./...` ohne `-short`, Guards, PII-Gate) | grün, vor jedem Commit und an `f90fa9e` |
| `make cross` (linux/amd64, windows/amd64, darwin/arm64) | grün |
| `make integration IT=dovecot,stalwart,greenmail` an `8175b51` | grün (Exit 0): 41 PASS, 0 FAIL, 1 SKIP (`TestIntegrationAuthelia`, nicht gewählt). Vorher an `53216cd`…`f90fa9e`: 40 PASS, 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit wie in S3.8). `dockerd` von Hand gestartet; Docker Hub lieferte `429 Too Many Requests`, die drei Images kamen unverändert (gleiche Tags) über `mirror.gcr.io` |
| Race-Detector | `go test -race` für `internal/remote`, `internal/config` und die A04-Tests in `internal/cli`, `internal/httpmcp`, `internal/ops` grün; das volle `make race` nicht gelaufen (nicht Teil des Auftrags) |
| `make vuln` | nicht gelaufen (in der Cloud gesperrt) |
| Rauchtest Teil 4 mit dem Linux-Programm aus dem Build | `mcp --http` (Büro-PC) und `remote add`/`remote test`/`mcp --remote` (zweiter PC) in getrennten Verzeichnissen: `tools: 20`, `key: key:zweiter-pc (role assistant)`, `initialize` und `mail_search` über die Brücke beantwortet, der Schlüssel in keiner Datei im Klartext |

Neue Tests Teil 4: `internal/remote` (`TestBridgeStatelessJSON`, `TestBridgeStatefulSSE` mit Session-ID, SSE, GET-Strom, 404-Neuanmeldung und Abbruch, `TestBridgeErrors`, `TestCheck`), `TestA04BridgeAgainstHTTPServer` (`internal/httpmcp`, gegen `mcp --http` in-process: Tools wie über stdio, Suche, Audit `transport: http`, falscher Schlüssel, Kanarie in Log, Fehler und Audit), `TestA04RemoteCommands`, `TestA04MCPRemoteBridge` (kein Datenverzeichnis), `TestA04DoctorShowsRemote` (`internal/cli`), `TestA04RemoteAddVerifiesAndRollsBack` (`internal/ops`), `TestA04CheckRemoteURL`, `TestA04RemoteFileEdits`, `TestA04RemoteErrDoesNotStopLoad` (`internal/config`); Vertragszeilen `remote.*` (Register und CLI), `notOverMCP`, Fehlercodes in `TestExitCodesMatchDocs`.

Neue Tests Teil 1 und 2 (je vorher rot, nachher grün geprüft): `TestA04ForwardCarriesThreadHeaders`, `TestA04ForwardsToCorpus` (1.000 Weiterleitungen, alle im Thread des Originals), `TestA04ForwardDraftStaysInThread` (Executor bis zur abgelegten Kopie), `TestA04ReplyWithoutDerivedRecipients`, `TestA04ReplyWithoutDerivedRecipientsCLI`; angepasst: `TestForward`, Vertragszeile `drafts.create` (`internal/ops`), CLI-Vertragsprobe `draft reply` mit `--no-derive-recipients`.

## 3. Build 0.3.0-rc2

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags (`git clone --no-tags`), je `git checkout 8175b51`, `git status --short` leer, `make release VERSION=0.3.0-rc2`.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`, sie enthält die Bauzeit).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103084613-8175b516678d`, `vcs.revision=8175b516678dbd5c4c69f1bfa85a9b5f165e993a`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc2 (go1.27.3 linux/amd64)`.

```
87c98c7436eeac89015cb56de5e53af6df2636eddf11b0c3d47f5cc6385af97b  postkauz-windows-amd64.exe
02495a20f1973c4e4d29feb46b891a8b117867a9a7d7ad17bf24966eefb9e9a4  postkauz-linux-amd64
d31683c39dc8daf7cb61ce3e70528e1534bedbdd5a0ef44745c061c177e23f1c  postkauz-darwin-arm64
```

Windows-Programm: 22.578.688 Byte. Der erste Build an `f90fa9e` (nur Teil 1 bis 3, Windows `3b33dc00…6625b3`) ist damit ersetzt und nicht zu verwenden.

## 4. Offen

- Probe des Betreibers mit rc2 im echten Gmail-Postfach (Vorgang 4): Weiterleitung anlegen, im Browser senden, Verlauf prüfen; Antwort mit `--no-derive-recipients`.
- Offene Frage 1 der Roadmap (VPS und D8 Teil D ruhen).
- Probe Vorgang 6 Phase 2: Claude Desktop am zweiten PC nach [betrieb.md](../betrieb.md) 4.9 (Pfad und Menüweg von `claude_desktop_config.json` aus Anleitungen Dritter, Stand November 2026; bei Abweichung bitte melden).
- `mcp --remote` ist nur unter Linux gelaufen; unter Windows nur kompiliert (Schlüsselbund, conhost-Aufgabe für `mcp --http`).
- Ältere, schon freigegebene Weiterleitungs-Entwürfe fallen beim ersten Push mit rc2 auf `draft` zurück (Entschiedene Fragen 70).
