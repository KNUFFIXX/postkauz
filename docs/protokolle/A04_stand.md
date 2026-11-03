# A04 „Alltag“: Stand, Prüfläufe, Build 0.3.0-rc2

Auftrag [auftraege/A04_alltag.md](../../auftraege/A04_alltag.md) (Betreiber-Sitzung DRACO-PK12, 03.11.2026). Zweig `claude/auftrag-a04-alltag-2jwww9`, Basis `main` `6c7ef13` (v0.3.0-rc1 + Doku). Kein Tag, kein Release, kein Merge.

## 1. Commits

| Commit | Inhalt |
|---|---|
| `e681575` | Auftrag A04 wörtlich abgelegt |
| `53216cd` | Teil 1: Weiterleitungen tragen `In-Reply-To`/`References` (E11 geändert, Entschiedene Fragen 70) |
| `dec07df` | Teil 2: `derive_recipients` für `drafts.create`, `draft reply --no-derive-recipients` (Entschiedene Fragen 71) |
| `1c74379` | Teil 3: Abnahmemaßstab nach dem Lastenheft vom 03.11.2026, Offene Frage 1 |
| `f90fa9e` | Roadmap-Stand und CHANGELOG; **Build-Commit von 0.3.0-rc2** |

## 2. Prüfläufe

Umgebung: Cloud-Sitzung, Go 1.27.3 (einmal über `GOTOOLCHAIN=go1.27.3 go version` geholt, danach `GOTOOLCHAIN=local`, Ausnahme laut CLAUDE.md).

| Lauf | Ergebnis |
|---|---|
| `make check` (gofmt, `go vet`, `go test ./...` ohne `-short`, Guards, PII-Gate) | grün, vor jedem Commit und an `f90fa9e` |
| `make cross` (linux/amd64, windows/amd64, darwin/arm64) | grün |
| `make integration IT=dovecot,stalwart,greenmail` | grün (Exit 0): 40 PASS, 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit wie in S3.8). `dockerd` von Hand gestartet; Docker Hub lieferte `429 Too Many Requests`, die drei Images kamen unverändert (gleiche Tags) über `mirror.gcr.io` |
| `make race`, `make vuln` | nicht gelaufen (nicht Teil des Auftrags; `make vuln` ist in der Cloud gesperrt) |

Neue Tests (je vorher rot, nachher grün geprüft): `TestA04ForwardCarriesThreadHeaders`, `TestA04ForwardsToCorpus` (1.000 Weiterleitungen, alle im Thread des Originals), `TestA04ForwardDraftStaysInThread` (Executor bis zur abgelegten Kopie), `TestA04ReplyWithoutDerivedRecipients`, `TestA04ReplyWithoutDerivedRecipientsCLI`; angepasst: `TestForward`, Vertragszeile `drafts.create` (`internal/ops`), CLI-Vertragsprobe `draft reply` mit `--no-derive-recipients`.

## 3. Build 0.3.0-rc2

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags (`git clone --no-tags`), je `git checkout f90fa9e`, `git status --short` leer, `make release VERSION=0.3.0-rc2`.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`, sie enthält die Bauzeit).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103080306-f90fa9ebe4d0`, `vcs.revision=f90fa9ebe4d0d27e036f08a20aefc197d7b5d171`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc2 (go1.27.3 linux/amd64)`.

```
3b33dc007827d1587350693c014154dffadaeab9ea32b2955d51a9b57f6625b3  postkauz-windows-amd64.exe
d667e31a56c879bd79760a880321597145e79b443e1c821fcb0fd33da6a309d0  postkauz-linux-amd64
ce003fc7b9727582fb14de716607bd0fb463ffb0daac692eb5dba1cda2e3ba5b  postkauz-darwin-arm64
```

Windows-Programm: 22.375.936 Byte.

## 4. Offen

- Probe des Betreibers mit rc2 im echten Gmail-Postfach (Vorgang 4): Weiterleitung anlegen, im Browser senden, Verlauf prüfen; Antwort mit `--no-derive-recipients`.
- Offene Frage 1 der Roadmap (VPS und D8 Teil D ruhen).
- Ältere, schon freigegebene Weiterleitungs-Entwürfe fallen beim ersten Push mit rc2 auf `draft` zurück (Entschiedene Fragen 70).
