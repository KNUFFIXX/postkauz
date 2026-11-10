# Stand A07 „Alltag 3: Versanderkennung schließen, Kopien aufräumen“

Auftrag: [auftraege/A07_alltag3.md](../../auftraege/A07_alltag3.md). Zweig `claude/compassionate-mayer-ntc1b5`, Basis `main` `d5d9af0` (Stand 0.3.0-rc4, Build-Commit `b41f9eb`). Kein Schema-Upgrade, keine Migration (Schema 22). Gründe und Zeitpunkte kommen wie in A06 aus dem Audit (`copy_gone`: `drafts.remote_check`/`missing`; `closed_reason`: `drafts.copy_gone_detected`, `drafts.supersede`).

## 1. Commits

| Commit | Teil | Inhalt |
|---|---|---|
| `3d59e10` | – | Auftrag abgelegt, CLAUDE.md: A07 aktuell, A06 abgeschlossen |
| `8bb3a2f` | 0 | Tag `v0.3.0-rc4` und GitHub-Release vermerkt (CHANGELOG, Roadmap 0d, A06_stand) |
| `0f9fd11` | 1 | Antwort-Entwurf ohne Empfänger: (d) im Verlauf und bei „ersetzt“ erfüllt, wenn der Entwurf kein To/Cc hat und die Antwort mindestens einen Empfänger; `remote_note` nennt den Fall |
| `40c0d73` | 2 | Rangfolge bei Gleichstand (gerundet auf 3 Stellen: `taken_over`, dann jüngeres `content_changed_at`, dann älteste Nachricht, dann kleinere ID); `refreshRemote` setzt `taken_over` → `missing`, wenn keine Fassung mehr im Entwürfe-Ordner liegt |
| `0815d66` | 3 | vierter Weg über den Betreff für neue Mails (Grund `subject`), Vorfilter nur über ID und Betreff |
| `c1c2434` | 4 | Auffangregel „Kopie weg“ (`copy_gone`, `[draft] copy_gone_after`, Standard 24 h) |
| `ac145b9` | 5 | `drafts.supersede` (R2, `mail:draft`, MCP `mail_supersede_draft`, CLI `draft supersede`), Runner-Ausnahme für die Herkunft `draft_supersede`, Nachtrag zu E2/E3 |
| `6574047` | 6 | Sammelpunkt 0e: Kommentar `replaced.go`, Sync-Test `MissingBefore`, Doku 3b, `push_failed` mit Grund, G202 |
| `69117c5` | 7 | Abschluss: D13 (Skript und Anleitung), Roadmap, CHANGELOG, Versanderkennung Stand und Abschnitt 5, README |

`make check` lief vollständig auf den Ständen `c1c2434` (Teil 4) und `ac145b9` (Teil 5) und auf dem Abschluss; für die Commits davor liefen gezielt die betroffenen Pakete (`internal/drafts`, `internal/ops`, `internal/cli`, `internal/store`) mit `go vet`, `gofmt`, Guard und PII-Gate (die Teile 1 bis 3 entstanden zusammen und wurden danach in Commits getrennt).

## 2. Festlegungen (Standardantworten von Claude, Einspruch bis zum Merge)

1. **Teil 1:** Der Hinweis in `draft show` bei `remote: missing` und einer Antwort ohne To/Cc lautet „this draft has no To or Cc, so it is closed automatically only when a sent reply to the original (In-Reply-To) with at least one recipient is found …“. Bcc des Entwurfs zählt weiter nicht.
2. **Teil 2:** Die Ähnlichkeit wird für die Rangfolge auf drei Nachkommastellen gerundet (`roundScore`); bei völligem Gleichstand entscheidet nach der Nachricht ihre lokale ID. `refreshRemote` schreibt nur den Wechsel `taken_over` → `missing` (kein Audit, wenn `taken_over` bleibt).
3. **Teil 3:** Der rohe Betreff wird vor der Präfix-Prüfung von unsichtbaren Zeichen befreit (wie `NormalizeSubject`). Die Kandidaten des Betreffs zählen nicht in `CheckReport.Examined` (das bleibt die Zahl des Rückfalls; der Benchmark prüft `steady` weiter mit `Examined == 0`). Vorfilter: ohne unsichtbare Zeichen muss das längste Wort des normalisierten Entwurfsbetreffs im rohen Betreff stehen, sonst wird nicht normalisiert (verliert nichts, weil `sanitize.Text` dann nichts ändert).
4. **Teil 4:** `"0s"` gilt wie `"0"` als aus; Tage (`"2d"`) wie bei anderen Dauern der Konfiguration. Im Dienst heißt `CopyGoneAfter` 0 „Standard 24 h“ und negativ „aus“. `missing_since` steht im Audit als RFC 3339 (UTC). Die Zeile `missing` des Syncs nennt jetzt die Auffangregel statt nur „close with draft mark-sent or draft abandon“.
5. **Teil 5:** Gründe der Ablehnung: `other_account`, `not_own_draft`, `changed_by_other`, `successor_closed`, `successor_not_newer` (streng jünger nach `created_at`), `approved_by_human` (`ready`), `draft_closed`, `kind_mismatch` (auch `reply` gegen `reply_all`), `other_original`, `other_subject`, `copy_changed`, `copy_missing`, `no_trash_mailbox`, `same_draft`. `PolicyInput` meldet Papierkorb und `1 + len(remote_previous)` Ziele; mehr als 50 frühere Kopien bräuchten wie jede Aktion `confirm_count`, das die Operation nicht kennt (bekannte Grenze, praktisch ohne Bedeutung). Die Aktion wird `queued` ohne Antrag angelegt; der Runner verlangt `mail:draft` nur, wenn **jedes** Ziel in einem Ordner mit Rolle `drafts` oder im Ablageordner eines Entwurfs liegt, sonst `mail:trash` (ohne Antrag scheitert dann ein Schlüssel ohne `mail:trash`); ist ein Ziel bei der Prüfung `stale`, gelten alle als `stale`, und nichts geht an den Server. Der Audit-Eintrag des Abschlusses trägt Akteur und Schlüssel des Aufrufers. Der Vertragstest bekam je Operation eine optionale Vorbereitung (`prep`), weil `drafts.supersede` Entwürfe des aufrufenden Schlüssels verlangt; die CLI-Probe des Vertragstests endet erwartet mit Exit 3.
6. **Teil 6 (f):** `push_failed` wird ohne Migration beim Lesen erklärt: `drafts.get` baut die Nachricht neu und nennt den Grund (z. B. `invalid_address`), ohne Adressen oder Text.
7. **D13:** neue Stufe `WARN` (ein Fehler eines Syncs, dessen sofortige Wiederholung fehlerfrei endet); Schlusszeile `D13: <n> OK, <m> FAIL, <w> WARN`.

## 3. Messungen

`BenchmarkCheckAfterSync22k` und `BenchmarkCheckAfterSync22kSubject` (Cloud-Container, 4 Kerne, je drei Läufe; Tabelle in [versanderkennung.md](../versanderkennung.md) Abschnitt 3c.1):

| Teil | vor A07 (`8bb3a2f`) | mit A07 |
|---|---|---|
| `steady` | 5,4–5,7 ms | 4,5–5,1 ms |
| `one_new` | 8,7–9,3 ms | 8,0–8,8 ms |
| `steady_replies` | 17,3–17,9 ms | 15,8–17,5 ms |
| `steady_subject` (30 neue Entwürfe ohne Kopie, 22.000 Nachrichten im Fenster) | 4,9–5,2 ms (Weg gab es nicht) | 37,6–40,6 ms (erste Fassung 414–561 ms) |
| `steady_copy_gone` (dieselben 30 als `missing`, 100.000 Audit-Einträge) | – | 14–16 ms |
| `missing_since_lookup` (eine Abfrage) | – | 0,04–0,10 ms |

`steady` bleibt unter der Grenze von 20 ms aus dem Auftrag; keine Karte nötig.

## 4. Prüfläufe

- `make check` auf dem Abschluss `69117c5`: grün (gofmt, `go vet`, alle Tests, Guards, PII-Gate; 40 Pakete mit Tests, 1.104 Testfunktionen, davon 33 neu in A07; 11:48 min im Container). Vorher auch auf `c1c2434` und `ac145b9` grün.
- `make cross` (linux/amd64, windows/amd64, darwin/arm64): grün.
- Race-Detector auf dem letzten Code-Commit `6574047` (`go test -race -timeout 45m`) für die von A07 berührten Pakete `internal/drafts`, `store`, `actions`, `mailsync`, `config`, `policy`, `mcp`, `ops`, `cli`: grün, keine Meldung, 25:41 min (`internal/cli` 25:37 min). Der volle `make race` über alle Pakete brach in der Sitzung an der Zeitgrenze von 30 min für Hintergrundbefehle ab (5 Pakete fertig, ohne Befund), weil gleichzeitig `make check` und der Build liefen; die übrigen Pakete hat A07 nicht geändert.
- `make integration`: nicht gelaufen, im Container läuft kein Docker-Daemon.
- golangci-lint (G202): die Abfragen in `store/sentdetect.go` sind jetzt Konstanten; ein Lauf von `make lint` steht in der CI des PR.

## 5. Build 0.3.0-rc5 (ersetzt durch den Build in Abschnitt 7.4)

Gebaut nach [betrieb.md](../betrieb.md) Abschnitt 7 in zwei frischen Klonen **ohne Tags** (`git clone --no-tags --branch claude/compassionate-mayer-ntc1b5`), je auf dem Build-Commit **`69117c5`** (Abschluss-Doku; letzter Code-Commit `6574047`), sauberer Arbeitsbaum, go1.27.3, `make release VERSION=0.3.0-rc5`. Beide `dist/SHA256SUMS` sind ohne die Zeile `VERSION` gleich (Nachbau bytegleich); `go version -m` in beiden: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261110094609-69117c5ef830`, `vcs.revision=69117c5ef830aa20b7e9f1c54acee2588b3bf976`.

| Datei | SHA256 |
|---|---|
| `postkauz-windows-amd64.exe` (22.746.624 Bytes) | `97ab3d84573ca1a5efc7e3b41000601da5929e5bdb6ca41441b2769048231a78` |
| `postkauz-linux-amd64` | `cf3d0cd8035a20dd6bbe785ad180f7fe5ad23b7315ef4e0b79a4a9504cdacbd9` |
| `postkauz-darwin-arm64` | `d488e410151a08b3a2feb8077f89943224212ed1681256aa3b3afe3fbbf1a9bd` |

Für D13: `-ExpectSha256 97ab3d84573ca1a5efc7e3b41000601da5929e5bdb6ca41441b2769048231a78`.

## 6. Offen

- Abnahme D13 (Teil A und Teil B) auf dem Büro-PC nach Entscheid des Betreibers; vorher Gegenprüfung.
- `abnahme_D13.ps1` ist nur maschinell geprüft (Klammerbilanz, ASCII mit BOM; PowerShell gibt es im Container nicht); die Anleitung beginnt mit einem Parser-Check.
- `make integration`: im Container läuft kein Docker-Daemon; kein Integrationslauf in dieser Sitzung.

## 7. Nachtrag 1 nach der Gegenprüfung (10.11.2026)

Gegenprüfung in der Betreiber-Sitzung DRACO-PK17 (zwei frische Claude-Läufe, Codex, jeder Fund am Quelltext nachgeprüft), Urteil FAIL. Der Betreiber hat rc5 nach Gegenprüfung und D13 für den Dauerbetrieb freigegeben (Karte A07-1).

### 7.1 Commits

| Commit | Gruppe | Inhalt |
|---|---|---|
| `d96b902` | B (Code) | B1 Sicherung der Auffangregel gegen weiterbearbeitete Fassungen; B2 `present` und Frist danach neu; B3 `supersede` nur in Entwürfe-Ordnern, Runner `not_in_drafts`; B4 `push_in_progress`; B5 `adoptOrphan` vor (f) bei Verlauf und Betreff; B6 `DraftClosedBy` mit `params.draft_id`; B7 `copy_gone_after` ≥ 1h; B8 Texte mit Aktion; B9 IDs als int64; dazu `copy_missing_since` für offene missing-Entwürfe |
| `1f001ed` | A (Skript) | A1 bis A8 in `abnahme_D13.ps1` und `abnahme_D13.md` |
| (dieser) | C (Doku) | versanderkennung.md 1, 3d, 5, 8; entscheidungen.md (Nachtrag zu E3 präzisiert); sicherheit.md; roadmap; CHANGELOG; dieses Protokoll |

### 7.2 Tests

Neu: `TestA07N1CopyGoneKeepsEditedVersion` (Antwort und neue Mail, ohne Fassung wie bisher `copy_gone`), `TestA07N1CopyBackResetsMissingSince` (23 h, Kopie zurück, 2 h später wieder weg → offen; nicht gespiegelt zählt nicht als zurück), `TestA07N1ThreadAdoptsOrphanFirst` (scheitert ohne die Änderung, geprüft), `TestA07N1SupersedeSkipsCopiesOutsideDrafts` (operator-Schlüssel, frühere Kopie im Archiv: kein MOVE dorthin, Hauptkopie in den Papierkorb), `TestA07N1RunnerRefusesSupersedeOutsideDrafts`, `TestA07N1SupersedeDeniedMore` (`other_account`, `push_in_progress` zweifach), `TestA07N1SupersedeClosedReasonOnlyForOld` (B6, B8), `TestA07N1DraftGetMissingSince`, `TestA07N1RenderSuperseded`, Konfiguration `1m`, `59m`, `-5h`, `1h`.

Prüfläufe: `make check` auf `d96b902` grün; Race-Detector (`go test -race -count=1 -timeout 45m`) für die berührten Pakete `internal/drafts`, `store`, `actions`, `config`, `mailsync`, `ops`, `cli` auf `d96b902`: grün, keine Meldung, 26:16 min (`internal/cli` 21:17 min). `make check` und `make cross` auf dem Build-Commit: Abschnitt 7.4. `make integration`: weiterhin kein Docker-Daemon im Container.

Messung `BenchmarkCheckAfterSync22k` erneut (Cloud-Container, 4 Kerne, je drei Läufe, CPU frei): `steady` 3,8–4,0 ms, `one_new` 6,1–6,5 ms, `steady_replies` 11,7–11,9 ms; `steady_subject` 27–30 ms, `steady_copy_gone` 14–15 ms (jetzt mit der Prüfung „Kopie zurück“ je missing-Entwurf), `missing_since_lookup` 0,07 ms. `steady` bleibt deutlich unter 20 ms.

### 7.3 Bewusst nicht übernommen (bekannte Grenzen)

- Die Rangfolge im Rückfall bevorzugt nicht den Entwurf, dessen Kopie fehlt (nur `taken_over` und die jüngere Fassung, Teil 2); bei Gleichstand zweier Fassungen mit vorhandener Kopie kann die Zuordnung so auf die falsche fallen, der Zustand bleibt richtig.
- `steady_subject` (30 neue Entwürfe ohne Kopie über 22.000 Nachrichten) bleibt bei etwa 40 ms; die Grenze von 20 ms aus dem Auftrag gilt für `steady`, `steady_subject` ist ein gewollt übertriebener Sonderfall, den die Auffangregel nach 24 h auflöst.
- A8 nutzt den MCP-Server statt der CLI, weil `--key` an der CLI nur den Namen eines Schlüssels nimmt, nie den Schlüssel selbst; der Weg entspricht genau dem eines Agenten.

### 7.4 Build 0.3.0-rc5 (neu)

Gebaut wie in Abschnitt 5 (zwei frische Klone ohne Tags, sauberer Arbeitsbaum, go1.27.3, `make release VERSION=0.3.0-rc5`) auf dem Build-Commit **`1fdd000`** (Doku des Nachtrags 1; letzter Code-Commit `d96b902`, Skript `1f001ed`). Beide `dist/SHA256SUMS` ohne die Zeile `VERSION` gleich (Nachbau bytegleich); `go version -m` in beiden `mod github.com/KNUFFIXX/postkauz v0.0.0-20261110192104-1fdd000aa2b6`, `vcs.revision=1fdd000aa2b6e6cb675019c39ee0a49e971f7d20`. **Ersetzt den Build an `69117c5`** (dessen Prüfsumme `97ab3d84…a231a78` gilt nicht mehr). `make check` (1.113 Testfunktionen, 40 Pakete mit Tests) und `make cross` auf `1fdd000` grün.

| Datei | SHA256 |
|---|---|
| `postkauz-windows-amd64.exe` (22.760.448 Bytes) | `d001341b2fa85a6928d127bc98e34d874ce13edca17def89b43ddbe99de0d619` |
| `postkauz-linux-amd64` | `4b2b710abb49532125ae5dcc7451805787237ad6dc3ca4a2139da156d5d524b6` |
| `postkauz-darwin-arm64` | `c737b05786832c1ae097a5769720f91a915119bd105598f3cd2694a36edf2e09` |

Für D13: `-ExpectSha256 d001341b2fa85a6928d127bc98e34d874ce13edca17def89b43ddbe99de0d619`.
