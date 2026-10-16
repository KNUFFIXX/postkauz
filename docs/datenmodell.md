# Datenmodell

Stand: S1.6 (16.10.2026). Das Schema steht als ausführbares SQL in [`internal/store/migrations/`](../internal/store/migrations/) (`0001_init.sql`, `0002_sync_state.sql`, `0003_threads_bodies_attachments.sql`) und wird von `internal/store/store_test.go` gegen `modernc.org/sqlite` geprüft (Tabellen vorhanden, Fremdschlüssel, CHECKs, Audit-Sperre, FTS5). Dieses Dokument nennt je Tabelle die Felder und **warum** sie so sind.

## Leitlinien

- **An JMAP angelehnt, nicht vollständig.** Account, Identity, Mailbox, Message, Thread, Attachment mit JMAP-Begriffen (Rolle, Zustand, Keywords). Wo IMAP etwas braucht, das JMAP nicht kennt (UID je Ordner, UIDVALIDITY), steht es zusätzlich da.
- **Lokale IDs.** Jede Tabelle hat einen `INTEGER PRIMARY KEY`. Die Nachrichten-ID ist das, was die CLI zeigt (`postkauz show 18233`) und was Agenten als Ziel verwenden.
- **Zeit:** Unix-Millisekunden UTC als `INTEGER`, Spalten enden auf `_at` oder heißen `ts`. Header-Daten (`date_header`) und Serverdatum (`internal_date`) bleiben getrennt.
- **Listen und Objekte** als `TEXT` mit JSON, abgesichert durch `CHECK (json_valid(...))`.
- **`STRICT`-Tabellen**, Fremdschlüssel eingeschaltet (`PRAGMA foreign_keys=ON` beim Öffnen).
- **Rohmails nie als BLOB in SQLite**, sondern im Blob-Store; die Datenbank hält nur den Hash.
- **Keine Geheimnisse in der Datenbank.** Konten führen `credential_ref` (Name im Schlüsselbund), API-Schlüssel nur Hashes.
- **Rollen und Status als CHECK-Listen**, nicht als freie Texte: Ein Tippfehler wird zum Fehler beim Schreiben statt zum stillen Datenfehler.

## Tabellen

### Konten und Ordner

| Tabelle | Wichtige Felder | Begründung |
|---|---|---|
| `accounts` | `name` (eindeutig), `email`, `provider_type` (`imap`\|`jmap`), `host`, `port`, `tls`, `username`, `auth_type`, `credential_ref`, `insecure_test_server`, `sync_policy` (JSON), `capabilities` (JSON), `status`, `last_error` | Ein Konto = ein Postfach. `insecure_test_server` erlaubt Klartext/selbstsignierte Zertifikate, nur für localhost (S1.1). `status`/`last_error`/`capabilities` setzt `account test`. `capabilities` speichert das Ergebnis der Fähigkeitserkennung (siehe `provider.Capabilities`), damit die Sync-Strategie ohne neue Verbindung feststeht. `credential_ref` verweist nur. |
| `identities` | `account_id`, `name`, `email`, `signature`, `reply_to`, `is_default` | Absenderidentitäten je Konto (JMAP Identity). Entwürfe verweisen darauf. |
| `mailboxes` | `account_id`, `path` (UTF-8), `name`, `role`, `parent_id`, `uidvalidity`, `uidnext`, `highestmodseq`, `sync_phase`, `total`, `unread`, `last_sync_at` | `role` kommt aus SPECIAL-USE oder Heuristik, einschließlich `all` (Gmail „Alle Nachrichten", SPECIAL-USE `\All`; seit Migration 0002). `uidvalidity/uidnext/highestmodseq` sind der Sync-Zustand (JMAP: ein Token): `uidnext` ist der Cursor (alle UIDs darunter sind gespiegelt; im Erstsync das Ende des letzten fertigen Fensters), `highestmodseq` der CONDSTORE-Stand. `sync_phase`: `new` (nie gesynct), `initial` (Erstsync in Fenstern läuft, wiederaufnehmbar), `ready`. `total` ist die Nachrichtenzahl des Servers beim letzten SELECT, `unread` die lokal gezählten ungelesenen Kopien. `UNIQUE(account_id, path)`. |

### Nachrichten

| Tabelle | Wichtige Felder | Begründung |
|---|---|---|
| `messages` | `account_id`, `message_id_header`, `thread_id`, `subject`, `from_addr`, `to_addrs`/`cc_addrs` (JSON), `date_header`, `internal_date`, `size`, `flag_seen/flagged/answered/draft`, `keywords` (JSON), `has_attachments`, `snippet`, `blob_sha256`, `in_reply_to`, `refs` (JSON), `list_id`, `auth_results`, `content_hash`, `deleted_remote_at` | Eine Zeile je **Nachricht**, nicht je Ordnerkopie. Die vier Systemflags sind eigene Spalten (häufig gefiltert, indizierbar); freie Keywords als JSON. `blob_sha256` darf NULL sein: „nur Kopf gespiegelt" (Erstsync in Fenstern, Body bei Bedarf). `content_hash` erlaubt den Abgleich beim Import und beim Sync; seit S1.4 ist er SHA-256 über die Rohmail ohne CR-Bytes (CRLF und LF gelten gleich; weitere Normalisierung für mbox-Kopfzeilen bei Bedarf in S1.10). `size` ist die Länge der Rohmail in Bytes. Seit S1.4 füllt der Sync Betreff, Absender (`Name <adresse>`), Empfänger (JSON `[{name,address}]`), Datum, Flags, Keywords, `has_attachments`, `snippet` (200 Zeichen), `in_reply_to`, `refs`, `list_id`, `auth_results` und `bodies.text_plain`; `thread_id` setzt der Sync seit S1.6; `attachments` und `text_from_html` siehe unten. `deleted_remote_at` setzt der Sync, wenn der Server die Nachricht nicht mehr führt; das Archiv behält sie (ADR-003). `bcc` fehlt absichtlich: Es steht nur in Entwürfen. Das Datum liefert `internal/mimeparse`: `Date`-Kopfzeile tolerant gelesen (auch Zonen ohne Vorzeichen wie „0100"); fehlt sie oder ist sie unlesbar, gilt `INTERNALDATE` (Spike-Befund), `date_header` bleibt dann NULL. |
| `message_mailboxes` | `message_id`, `mailbox_id`, `uid`, `modseq`, `flag_seen/flagged/answered/draft`, `keywords` | Eine Nachricht kann in mehreren Ordnern liegen (Gmail-Labels als Ordner, Kopien). `UNIQUE(mailbox_id, uid)` ist der Idempotenzschlüssel beim Sync. Flags stehen je Kopie, weil der Server sie je Kopie führt (seit 0002); `messages.flag_*` ist die Zusammenfassung: gesetzt, wenn mindestens eine vorhandene Kopie das Flag trägt (bei Gmail sind die Flags aller Labels ohnehin gleich). |
| `bodies` | `message_id`, `text_plain`, `text_from_html`, `hidden_text`, `charset`, `text_unquoted`, `links` (JSON), `clean_version` | Bereinigter Text. HTML wird **nie** ungefiltert gespeichert (nur als Rohmail im Blob). Seit S1.6 (`internal/sanitize`): `text_plain` ist der Klartextteil ohne unsichtbare Formatzeichen (Nullbreite, Bidi-Steuerung), `text_from_html` der Text aus dem HTML-Teil (Allowlist, Links sichtbar als „Text <URL>", versteckter Text entfernt), `hidden_text` markiert, dass versteckter Text entfernt wurde (Prompt-Injection-Indiz), `links` die http(s)-Links mit Host und Warnungen (`punycode`, `mixed_scripts`, `ip_address`, `userinfo`, `text_domain_mismatch`), `text_unquoted` den neuen Text ohne Zitate (aus dem Klartext, sonst aus dem HTML-Text; Grundlage des `snippet`). `clean_version` ist der Stand der Aufbereitung (`mailsync.CleanVersion`, derzeit 1); ältere Stände (0 = vor S1.6 gespiegelt) bereitet der nächste Sync aus der Rohmail neu auf. |
| `attachments` | `message_id`, `part_id`, `filename`, `content_type`, `declared_type`, `size`, `sha256`, `inline`, `content_id`, `truncated`, `type_mismatch`, `mismatch_reason`, `executable`, `extracted_text`, `extract_status`, `extract_error`, `hidden_text` | Seit S1.6 (Migration 0003) schreibt der Sync je MIME-Teil, der kein Body-Text ist, eine Zeile (auch Inline-Bilder und weitergeleitete Nachrichten). `content_type` stammt aus **Magic Bytes** (`internal/magic`; leerer Teil: `application/x-empty`), `declared_type` ist die Angabe der Mail; `type_mismatch` mit `mismatch_reason`, wenn Content-Type oder Endung etwas anderes behaupten (ein Programm unter fremder Flagge immer), `executable` für Programme, Skripte und riskante Endungen. Der dekodierte Inhalt liegt im Blob-Store (`sha256`; NULL, wenn der Teil über der Grenze von 64 MiB liegt, `truncated`); Archive werden nie entpackt. Text per Extraktion (`internal/extract`; PDF über `pdftotext`, DOCX im Kindprozess, Text/HTML/Mail im Prozess) am Ende jedes Syncs mit Zeitbudget: `pending` → `ok`\|`failed`\|`skipped`, Grund ohne Inhalt in `extract_error`; Programme, Typ-Widersprüche, nicht unterstützte Typen und Teile über 32 MiB werden übersprungen. `hidden_text`: HTML-Anhang mit verstecktem Text. |
| `threads` | `account_id`, `subject_norm`, `first_date`, `last_date`, `message_count`, `participants` (JSON), `root_orphan` | Zuordnung über `References`/`In-Reply-To`; Rückfall: normalisierter Betreff innerhalb von 30 Tagen. Ein Thread gehört zu **einem** Konto. Seit S1.6 (Migration 0003) ordnet der Sync jede neue Nachricht in derselben Transaktion zu (Entscheidung in `internal/thread`, `Assign`): Verweise verbinden und vereinigen Threads (wie Union-Find, unabhängig von der Reihenfolge); eine **Antwort ohne Verweise** (Betreff mit `Re:`/`AW:`/`WG:`/`Fwd:` …) kommt in den Thread gleichen normalisierten Betreffs, dessen Zeitspanne höchstens 30 Tage entfernt ist, und ein wachsender Thread sammelt solche Waisen-Threads ein (`root_orphan`: die früheste Nachricht ist so eine Antwort). Nachrichten ohne Präfix werden nie allein über den Betreff verbunden. `subject_norm`: Betreff der frühesten Nachricht ohne Präfixe und Listenmarken, nur Buchstaben und Ziffern, klein (Clients ersetzen Gedankenstriche und Emoji je nach Zeichensatz). `first_date`/`last_date`: Datum der Kopfzeile, sonst `INTERNALDATE`; `participants`: bis zu 20 verschiedene Absender. Bestand ohne Thread (vor S1.6 gespiegelt) ordnet der nächste Sync nach Datum zu. |
| `thread_refs` | `account_id`, `ref_id`, `thread_id` | Jede bekannte Message-ID (eigene und verwiesene, auch von fehlenden Nachrichten) zeigt auf ihren Thread; darüber findet eine neue Nachricht ihren Thread, und eine Nachricht, die zwei Threads verbindet, vereinigt sie (seit 0003). |
| `tags`, `message_tags` | `name`, `source` (`local`\|`server`), `set_by`, `set_at` | Lokale Etiketten (S3), optional auf IMAP-Keywords abgebildet. `set_by` hält fest, ob Mensch oder Schlüssel. |
| `blobs` | `sha256`, `size`, `compressed`, `created_at` | Verzeichnis der Blob-Dateien; Fremdschlüsselziel für Nachrichten und Anhänge. Seit S1.4 schreibt der Sync die Zeile in derselben Transaktion wie die Nachricht (die Datei liegt vorher schon da). |

### Entwürfe, Freigaben, Schlüssel

| Tabelle | Wichtige Felder | Begründung |
|---|---|---|
| `drafts` | `account_id`, `identity_id`, `kind` (`new`\|`reply`\|`reply_all`\|`forward`), `in_reply_to_msg`, `message_id_header` (eindeutig), `to/cc/bcc_addrs`, `subject`, `body_text`, `body_html`, `attachment_blobs`, `remote_uid`, `status`, `preview_hash`, `created_by`, `approval_id`, `sent_message_id` | Die `Message-ID` wird **beim Anlegen** vergeben und steht in der APPEND-Kopie; damit erkennt der Sync den Versand durch den Menschen (`sent_by_human`). `status`: `draft` → `pending_approval` → `ready` → `sent_by_human`, oder `abandoned`. `preview_hash` bindet eine Freigabe an den exakten Inhalt. |
| `approvals` | `subject_type`, `subject_id`, `description`, `requested_by`, `requested_at`, `preview_hash`, `decision`, `decided_by`, `decided_at`, `reason` | Antrag → Entscheidung eines Menschen. `description` enthält **keinen Mailinhalt**. Ändert sich der Entwurf nach der Freigabe, passt `preview_hash` nicht mehr und die Freigabe ist ungültig. |
| `api_keys` | `name`, `key_hash`, `role`, `scopes` (JSON), `dry_run_default`, `expires_at`, `last_used_at`, `revoked_at` | Nur Hashes; der Klartext wird einmal bei der Erstellung gezeigt. Standard `dry_run_default = 1` für neue Schlüssel. |
| `policies` | `api_key_id` (NULL = Standard), `document` (JSON), `version` | Richtlinien als versionierte Dokumente. Ein Schlüssel darf die Standardrichtlinie nur **verengen**. |

### Betrieb und Nachvollziehbarkeit

| Tabelle | Wichtige Felder | Begründung |
|---|---|---|
| `jobs` | `type`, `payload` (JSON), `state`, `run_at`, `attempts`, `last_error`, `locked_by`, `locked_at` | Warteschlange in SQLite statt Broker. Index auf `(state, run_at)`. |
| `audit_events` | `ts`, `actor_type`, `api_key_id`, `scope`, `operation`, `params_redacted` (JSON), `targets` (JSON), `result`, `reason`, `dry_run`, `duration_ms` | **Append-only:** Trigger blockieren `UPDATE` und `DELETE` (getestet). Nie Mailinhalte oder Geheimnisse; Parameter nur als IDs, Feldnamen, Zähler, Hashes. |
| `audit_exports` | `day`, `first_event_id`, `last_event_id`, `sha256`, `location`, `exported_at` | Nachweis des täglichen Exports (JSONL-Datei, Prüfsumme, zweiter Ort). Ersetzt eine Hash-Kette je Zeile. |
| `meta` | `key`, `value` | Standinformationen (z. B. `fts_version`). Keine Fachdaten. |
| `schema_migrations` | `version`, `name`, `applied_at` | Wird vom Migrationsläufer angelegt. |

### Bewusst nicht angelegt

Regeln und Regelläufe (ab S4), Embeddings, Kalender und Kontakte, Snooze, Versandwarteschlange (kein Versand im MVP). Tabellen entstehen mit dem Schnitt, der sie braucht, per neuer Migration.

## Blob-Store

Rohmails (`.eml`) und Anhänge liegen **als Dateien** unter `<datenverzeichnis>/blobs/`.

- **Adressierung:** SHA-256 der **unkomprimierten** Bytes; Pfad `blobs/ab/cd/<sha256>` (zwei Verzeichnisebenen aus den ersten vier Hex-Zeichen). Gleiche Inhalte liegen genau einmal (Deduplizierung, auch über Konten hinweg).
- **Schreiben atomar** (`internal/blob`, S1.2): in eine Temporärdatei unter `blobs/tmp/` (gleiches Dateisystem), `fsync`, `rename` an den Zielpfad, `fsync` des Zielverzeichnisses (nicht auf Windows). Erst danach die Zeile in `blobs`. Eine verwaiste Datei ist unschädlich, eine Zeile ohne Datei nicht. Ist der Inhalt schon vorhanden, bleibt die vorhandene Datei unverändert. Größenlimit je Blob (Standard 256 MiB, unkomprimiert); darüber wird nichts angelegt.
- **Komprimierung:** optional zstd (`github.com/klauspost/compress/zstd`), erkennbar an `blobs.compressed` und an der Dateiendung `.zst` (`blobs/ab/cd/<sha256>.zst`); der Hash bleibt der des Klartexts. Lesen erkennt die Form am Dateinamen, nie am Inhalt (ein Anhang darf selbst zstd-Daten enthalten). Ein bereits vorhandener Blob wird nicht in die andere Form umgeschrieben.
- **Lesen prüft nichts neu**, aber `Store.Verify` prüft Hash und Größe; `postkauz doctor` nutzt das für Stichproben (`doctor --blobs`, S1.11). Hash-Argumente werden streng geprüft (64 Kleinbuchstaben-Hex), sonst wären Pfade wie `../` möglich.
- **Aufräumen:** Mark-and-Sweep über `messages.blob_sha256`, `attachments.sha256` und Entwurfs-Anhänge; nur über eine ausdrückliche Wartungsoperation, nie automatisch beim Sync (ADR-003, Regel 2).
- **Speicherort:** niemals in einem Drive-/Cloud-synchronisierten Ordner (WAL + Synchronisierung = Korruption). Standard: das Datenverzeichnis des Betriebssystems unterhalb von `postkauz/`. Auf Windows ist eine **Ausnahme im Virenscanner** für den Blob-Ordner einzuplanen, weil echte Postfächer Phishing-Anhänge enthalten.
- **Verschlüsselung im Ruhezustand:** nicht im MVP (Datenträgerverschlüsselung des Betriebssystems ist Voraussetzung); Backups müssen verschlüsselt werden (S3).

## Volltextsuche (FTS5-Strategie)

Ziele: Umlaute und deutsche Flexionen finden („Rechnung" ↔ „Rechnungen"), Teilwörter in Komposita finden („Vollstreck" ↔ „Zwangsvollstreckung"), Antwort unter 300 ms bei 100.000 Nachrichten.

Zwei **contentless** FTS5-Tabellen (sie speichern nur den Index, nicht den Text; `contentless_delete=1` erlaubt Löschen). `rowid` = `messages.id`.

| Tabelle | Tokenizer | Spalten | Zweck |
|---|---|---|---|
| `fts_messages` | `unicode61 remove_diacritics 2` | `subject, sender, recipients, body, attachments_text, body_stem` | Wortsuche; ä/ö/ü/ß-Normalisierung; `body_stem` trägt den **app-seitig** berechneten deutschen Wortstamm |
| `fts_trigram` | `trigram` | `subject, sender, body` | Teilwort-/Infixsuche (ab 3 Zeichen) |

- **Warum app-seitig stemmen:** FTS5 bringt nur `unicode61` und einen englischen Porter-Stemmer mit. Der deutsche Wortstamm (Snowball-German) wird beim Indizieren berechnet und in `body_stem` geschrieben; dieselbe Funktion stemmt die Suchbegriffe. Welche Go-Bibliothek dafür dient, wird in S1 entschieden (Anforderung: reines Go, permissive Lizenz, Snowball-German-konform; andernfalls Portierung des Algorithmus). Bleve bleibt als Option, falls FTS5 die Qualitätsziele verfehlt.
- **Normalisierer (Befund Spike S0):** `unicode61` faltet ä/ö/ü, aber **nicht ß/ss** (Erdgeschoss und Erdgeschoß sind verschiedene Wörter) und findet keine Teilwörter. Ein app-seitiger Normalisierer faltet ß zu ss, in Indexierung und Anfrage gleichermaßen; Komposita bleiben Aufgabe des Trigram-Index.
- **Komposita:** Der Stemmer zerlegt sie nicht; dafür ist `fts_trigram` da. Die Suche kombiniert beide: Wortstamm-Treffer zuerst (Ranking), Trigram-Treffer ergänzend.
- **Indexpflege durch die Anwendung**, nicht durch Trigger (der Stamm ist app-seitig). Ein `index`-Job schreibt bzw. ersetzt die Zeilen einer Nachricht; Löschen per `DELETE … WHERE rowid = ?`.
- **Ranking:** `bm25()` mit Spaltengewichten (Betreff vor Absender vor Body); Cursor-Paginierung über `(rank, rowid)`.
- **Abfragesprache** (nach Gmail-Vorbild) wird in einen FTS-Ausdruck plus SQL-Filter übersetzt: `from:`, `to:`, `subject:`, `after:`, `before:`, `has:attachment`, `in:`, `tag:`, `is:unread`. Das Ergebnis des Übersetzers ist über `--explain` sichtbar. Nutzereingaben werden **nie** unmaskiert in `MATCH` gegeben (Syntaxfehler und Injektionen im Ausdruck).
- **Messbar machen:** ein Testsatz mit Suchbegriffen, Flexionen und erwarteten Treffern liegt im Korpus (siehe [testkorpus.md](testkorpus.md)); er wird mit jeder Änderung am Tokenizer oder Stemmer ausgeführt.
- **Versionierung:** Ändern sich Tokenizer oder Stemmer, erhöht sich `meta.fts_version`; ein `index`-Job baut den Index neu. Der Index ist jederzeit aus Datenbank und Blobs rekonstruierbar.
- Getestet im Gerüst: Einfügen, Umlaut-Treffer, Wortstamm-Präfix, Trigram-Infix, Löschen (`TestFTS5Strategy`).

## Migrationen

- **Eingebettete SQL-Dateien** `internal/store/migrations/NNNN_name.sql` (`go:embed`), lückenlos nummeriert. Der Läufer prüft die Lückenlosigkeit.
- **Nur vorwärts.** Eine angewendete Migration wird nie geändert; Korrekturen sind neue Dateien. `0001_init.sql` wurde zuletzt mit S1.1 überarbeitet (Spalte `accounts.insecure_test_server`); **ab S1.1 können echte Datenbanken mit Konten existieren, jede weitere Änderung ist eine neue Migration.**
- **Je Migration eine Transaktion**, danach ein Eintrag in `schema_migrations`.
- **Vor jeder Migration eines bestehenden Bestands** ein Snapshot per `VACUUM INTO` neben der Datenbankdatei: `postkauz.db.pre-NNNN-<UTC-Zeit>.bak` (0600; umgesetzt mit 0002, getestet). Snapshots werden nicht automatisch gelöscht.
- **Tabellenumbau:** SQLite kann CHECK-Listen nicht ändern; eine Migration baut die Tabelle dann neu auf. Beginnt die Datei mit `-- postkauz:foreign_keys=off`, schaltet der Läufer die Fremdschlüssel für diese Migration ab (sonst löscht `DROP TABLE` abhängige Zeilen per CASCADE) und prüft danach `PRAGMA foreign_key_check`.
- **Abwärtskompatibilität der Programme:** Ein Programm, das eine neuere Schemaversion findet als es kennt, verweigert die Arbeit (`store.ErrSchemaTooNew`, `doctor` meldet `too_new`; umgesetzt und getestet S1.1).
- **Treiber:** `modernc.org/sqlite`, reines Go, keine CGO-Abhängigkeit (ADR-005). FTS5 und `contentless_delete` sind enthalten und im Test belegt.
- **Datensicherung:** konsistente Snapshots per `VACUUM INTO` plus Blob-Verzeichnis; Wiederherstellung wird in S3 mit einem Restore-Test belegt.
