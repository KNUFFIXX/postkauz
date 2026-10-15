# Datenmodell

Stand: Phase 0. Das Schema steht als ausführbares SQL in [`internal/store/migrations/0001_init.sql`](../internal/store/migrations/0001_init.sql) und wird von `internal/store/store_test.go` gegen `modernc.org/sqlite` geprüft (Tabellen vorhanden, Fremdschlüssel, CHECKs, Audit-Sperre, FTS5). Dieses Dokument nennt je Tabelle die Felder und **warum** sie so sind.

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
| `mailboxes` | `account_id`, `path` (UTF-8), `name`, `role`, `parent_id`, `uidvalidity`, `uidnext`, `highestmodseq`, `total`, `unread`, `last_sync_at` | `role` kommt aus SPECIAL-USE oder Heuristik. `uidvalidity/uidnext/highestmodseq` sind der Sync-Zustand (JMAP: ein Token). `UNIQUE(account_id, path)`. |

### Nachrichten

| Tabelle | Wichtige Felder | Begründung |
|---|---|---|
| `messages` | `account_id`, `message_id_header`, `thread_id`, `subject`, `from_addr`, `to_addrs`/`cc_addrs` (JSON), `date_header`, `internal_date`, `size`, `flag_seen/flagged/answered/draft`, `keywords` (JSON), `has_attachments`, `snippet`, `blob_sha256`, `in_reply_to`, `refs` (JSON), `list_id`, `auth_results`, `content_hash`, `deleted_remote_at` | Eine Zeile je **Nachricht**, nicht je Ordnerkopie. Die vier Systemflags sind eigene Spalten (häufig gefiltert, indizierbar); freie Keywords als JSON. `blob_sha256` darf NULL sein: „nur Kopf gespiegelt" (Erstsync in Fenstern, Body bei Bedarf). `content_hash` erlaubt den Abgleich beim Import. `deleted_remote_at` setzt der Sync, wenn der Server die Nachricht nicht mehr führt; das Archiv behält sie (ADR-003). `bcc` fehlt absichtlich: Es steht nur in Entwürfen. |
| `message_mailboxes` | `message_id`, `mailbox_id`, `uid`, `modseq` | Eine Nachricht kann in mehreren Ordnern liegen (Gmail-Labels als Ordner, Kopien). `UNIQUE(mailbox_id, uid)` ist der Idempotenzschlüssel beim Sync. |
| `bodies` | `message_id`, `text_plain`, `text_from_html`, `hidden_text`, `charset` | Bereinigter Text. HTML wird **nie** ungefiltert gespeichert (nur als Rohmail im Blob). `hidden_text` markiert, dass versteckter Text entfernt wurde (Prompt-Injection-Indiz). |
| `attachments` | `message_id`, `part_id`, `filename`, `content_type`, `declared_type`, `size`, `sha256`, `extracted_text`, `extract_status` | `content_type` stammt aus **Magic Bytes**, `declared_type` ist die Angabe der Mail. Inhalt im Blob-Store, Text per Subprozess extrahiert (`pending`\|`ok`\|`failed`\|`skipped`). |
| `threads` | `account_id`, `subject_norm`, `first_date`, `last_date`, `message_count`, `participants` (JSON) | Zuordnung über `References`/`In-Reply-To`; Rückfall: normalisierter Betreff innerhalb von 30 Tagen. Ein Thread gehört zu **einem** Konto. |
| `tags`, `message_tags` | `name`, `source` (`local`\|`server`), `set_by`, `set_at` | Lokale Etiketten (S3), optional auf IMAP-Keywords abgebildet. `set_by` hält fest, ob Mensch oder Schlüssel. |
| `blobs` | `sha256`, `size`, `compressed`, `created_at` | Verzeichnis der Blob-Dateien; Fremdschlüsselziel für Nachrichten und Anhänge. |

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
- **Schreiben atomar:** in eine Temporärdatei im selben Verzeichnis, `fsync`, dann `rename`. Erst danach die Zeile in `blobs`. Eine verwaiste Datei ist unschädlich, eine Zeile ohne Datei nicht.
- **Komprimierung:** optional zstd, erkennbar an `blobs.compressed`; der Hash bleibt der des Klartexts.
- **Lesen prüft nichts neu**, aber `postkauz doctor` kann Stichproben-Hashes prüfen (`doctor --blobs`, ab S1).
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
- **Vor jeder Migration eines bestehenden Bestands** ein Snapshot per `VACUUM INTO` (noch offen; nötig ab der ersten Migration 0002).
- **Abwärtskompatibilität der Programme:** Ein Programm, das eine neuere Schemaversion findet als es kennt, verweigert die Arbeit (`store.ErrSchemaTooNew`, `doctor` meldet `too_new`; umgesetzt und getestet S1.1).
- **Treiber:** `modernc.org/sqlite`, reines Go, keine CGO-Abhängigkeit (ADR-005). FTS5 und `contentless_delete` sind enthalten und im Test belegt.
- **Datensicherung:** konsistente Snapshots per `VACUUM INTO` plus Blob-Verzeichnis; Wiederherstellung wird in S3 mit einem Restore-Test belegt.
