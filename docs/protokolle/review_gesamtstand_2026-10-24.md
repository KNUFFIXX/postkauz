# Gegenprüfung des Gesamtstands vor Schnitt 3 (24.10.2026)

Unabhängige Architektur- und Wartbarkeitsprüfung durch einen Review-Agenten (Claude Opus 5.5, nur lesend) auf `main` `50b37e4`, frischer Klon. Auftraggeber: Betreiber-Session THV-PK03. Nicht gelaufen: `make check`, `race`, Integration. Die Punkte sind in `auftraege/A03_s3.md`, Nachtrag 24.10.2026, als Teil-A-Nachbesserungen übernommen.

**Urteil: FAIL vor dem Start von Schnitt 3.** Ein Blocker muss vor dem ersten MOVE- oder Papierkorb-Aufruf behoben sein. Für Schnitt 1 und 2 kein Blocker.

## BLOCKER

- `internal/imapx/mailbox.go:160` `uidSet` reicht jede UID ungeprüft an `AddNum` weiter. In go-imap v2 bedeutet UID 0 „*", also die neueste Mail im Ordner (Modulquelle `numset.go:123`). Dazu lässt `Ensure` die UIDVALIDITY-Prüfung aus, wenn der Wert 0 ist (`mailbox.go:77`, `:86`); `RemoteCopy.UIDValidity` ist `omitempty` (`store/drafts.go:128`), `messages.uid` hat kein `CHECK > 0` (`0001_init.sql:120`). Folge: Kommt aus einem Fehlerpfad eine UID 0, trifft Move oder Trash die neueste Mail. Kein Test deckt das ab (`mailbox_test.go:141` prüft nur ReadOnly und `\Deleted`).

## WICHTIG

- **Serveränderung ohne Audit-Zeile:** APPEND in `ops/draft_push.go:296`, Audit-Zeile erst danach in `drafts/remote.go:365`. Stirbt der Prozess dazwischen und folgt `draft abandon`, lässt `close()` (`drafts/service.go:722`) `push_attempt` stehen; der Entwurf fällt aus `draftPushPending` (`store/draftpush.go:20`), `adoptOrphan` (`remote.go:195`, `service.go:323`) läuft nie. Die Kopie liegt dauerhaft auf dem Server ohne Audit-Zeile. `review25_test.go:243` (K5) deckt nur den Abschluss im selben Prozess ab.
- **Jobs ohne Herkunft:** `jobs` (`0010_drafts.sql:45`) hat weder `key_id` noch `audit_id`. `ops.DraftPusher` (`draft_push.go:158`) prüft beim Ausführen weder Widerruf (wie `executor.go:212`) noch Policy; ein widerrufener Schlüssel bekommt seine Entwürfe noch abgelegt. Move-/Trash-Jobs würden das Muster erben.
- **Policy sieht keine Eingabe:** `policy.Check` (`policy.go:151`) bekommt nur Schlüssel, Requirement, dryRun. `[move] agent_may` und `bulk_confirm_threshold` (`file.go:96-100`) sind daher nur im Handler prüfbar; dort steht im Audit schon „ok" (`executor.go:237`), Regeln verstreuen sich.
- **Variante A (Audit `started`), fehlende Anschlüsse:** Wert fehlt im CHECK (`0001:252`) und im Enum `auditEventSchema` (`ops/audit.go:65`) → OutputSchema von `audit.list`/`mail_audit_log` bricht; Vertragstest erwartet genau einen Eintrag (`ops_test.go:608`); Übergangszeilen (`drafts/service.go:118`) können nicht auf den Start-Eintrag verweisen (weder `Call` `executor.go:255` noch `drafts.Who` `service.go:51` tragen eine Audit-ID); `ref_id` muss in `AuditEventOutput` (`ops/audit.go:27`) `omitempty` sein, sonst ändert der aus der DB neu gebaute Export alter Tage (`auditexport.go:114`) die Hash-Kette.
- **Restore klont die Identität:** `VACUUM INTO` kopiert `meta.datadir_id` (`0008_s2_prep.sql:17`) → Original und Restore teilen den Schlüsselbund-Eintrag `account/<id>/<name>` (`secrets.go:106`); `audit_exports.location` wird mitkopiert (`auditexport.go:331-343`), beide schreiben in denselben Exportordner, `audit verify` meldet „changed"; offene Jobs und `push_attempt` laufen nach dem Restore wieder an. A03 E7/S3.4 behandeln nichts davon. Bei Windows-Betrieb mit Export nach Google Drive ist ein Restore auf demselben Rechner realistisch.
- **`blobs.gc`-Wettlauf:** Blobs werden vor ihrer DB-Zeile und ohne Sperre geschrieben (`mailsync.go:818`, `importer.go:255`, `ops/drafts.go:686`, letzteres im MCP-Prozess). „Entfernt nur Dateien ohne Zeile" (A03:113) würde frische Blobs löschen → Mindestalter oder Sperre.

## KLEIN

- Neue Hotspots (AST-Messung), alle von S3 berührt: `mailsync.applyChanges` 162 Zeilen (`mailsync.go:470`), `AuditVerify` 158 (`auditexport.go:447`), `MessagesGet` 166 (`ops/messages.go:85`).
- Risikostufe falsch: `sync.run` ist R1 „changes local data" (`ops/sync.go:165`, `mcp/tools.go:225`), legt aber per APPEND auf dem Server ab; `drafts.request_ready` (R1, `drafts.go:1704`) reiht ebenfalls ein APPEND ein.
- Abschlusszeile des Executors trägt den Zeitstempel des Starts (`executor.go:198`; Z. 269 setzt nur die Dauer).
- Fester Text „never … moves mail" (`mcp/tools.go:123`) wird mit S3 falsch; kein Test bindet ihn an die Scopes.
- Export: kein Dateinamen-Filter; A03:112 deckt Windows nicht ab (`:`-Datenströme, CON/NUL, Punkt/Leerzeichen am Ende); Fuzz-Test fehlt.

## Doku-Drift

- `docs/sicherheit.md:3` nennt `sync.run` „lesend"; seit S2.5 legt sie per APPEND ab (`docs/cli.md:149`).
- `docs/architektur.md:57`: alles, was den Server berührt, laufe über Jobs — `sync.run` synct im Vordergrund (`ops/sync.go:161`).
- `internal/policy/policy.go:140,150` Kommentar „Stub für Phase 0 … folgt in Schnitt 2" widerspricht `roadmap.md` und `architektur.md:238`.

## Geprüft, in Ordnung

1. Handler nur über `executor.go:85`; ein Schreibweg ins Audit (`store/audit_query.go:185`); eine Implementierung des Inhalts-Hashes (`mailsync/hash.go`).
2. Hook: Ausgabe 4 KB (`hook.go:100`), Umgebung aus fester Liste (`hook.go:137`); MCP-Eingaben durch Schema und Executor-Grenzen (`executor.go:248-251`); `StoreFlags` verweigert `\Deleted` (`mailbox.go:512`).
3. MCP ruft nur den Executor (`server.go:119`); Tools nach Scope gefiltert (`server.go:70`); HumanOnly vor Scope (`policy.go:152`).
4. Audit-Trigger nach allen Migrationen getestet (`store_test.go:82-85`); `policy_version` lässt alte Exporte byte-gleich (`ops/audit.go:43`).
5. Doku stimmt: Tool-Zahlen 9/15 (`docs/mcp.md:46`, `server_test.go:195-200`), Kettenformat (`docs/cli.md:217`, `auditexport.go:71-84`).
