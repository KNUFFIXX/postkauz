# Architektur

Stand: S1.6 (16.10.2026). Dieses Dokument beschreibt die Zielarchitektur der Schnitte S1–S3 und markiert, was das Gerüst schon enthält. Verbindlich sind die Entscheidungen in [entscheidungen.md](entscheidungen.md); wo dieses Dokument davon abweicht, gilt entscheidungen.md.

„PostKauz" ist ein Arbeitstitel. Alle Namen stehen in `internal/brand` (siehe unten, „Namen").

## 1. Ziel in einem Absatz

Ein lokal laufender E-Mail-Client **ohne GUI**, der ein vorhandenes IMAP-Postfach spiegelt, durchsuchbar macht und Menschen (CLI) sowie KI-Agenten (MCP) bedient. Agenten bekommen Scopes, Risikostufen, Trockenlauf, Freigaben und ein Audit-Log. Der MVP ist **draft-only**: Er schreibt Entwürfe in den Entwürfe-Ordner des Postfachs, versendet aber nie selbst (ADR-002).

## 2. Komponenten

```mermaid
flowchart LR
  subgraph Oberflaechen["Oberflächen (dünne Adapter)"]
    CLI["CLI: postkauz / pk"]
    MCP["MCP-Server: stdio (S1), HTTP (S3)"]
    REST["REST + OpenAPI (nach S3)"]
  end
  subgraph Kern["Kern"]
    REG["Operationsregister (internal/ops)"]
    EXE["Executor: Scope, Policy, Freigabe, Audit"]
    POL["Policy (internal/policy)"]
    AUD["Audit (internal/audit)"]
    SVC["Dienste: Suche, Threads, Entwürfe, Tags"]
    JOB["Jobs: Sync, Index, Extraktion, Import"]
  end
  subgraph Speicher["Lokaler Speicher"]
    DB[("SQLite: Metadaten, FTS5, Audit")]
    BLOB[("Blob-Store: .eml, Anhänge")]
    KEY[("Schlüsselbund / Umgebung")]
  end
  subgraph Anbieter["Provider (internal/provider)"]
    IMAP["IMAP-Provider (S1)"]
    JMAP["JMAP-Provider (später)"]
  end
  SRV[("Mailserver")]

  CLI --> EXE
  MCP --> EXE
  REST --> EXE
  EXE --> REG
  EXE --> POL
  EXE --> AUD
  REG --> SVC
  SVC --> DB
  SVC --> BLOB
  SVC --> JOB
  AUD --> DB
  JOB --> IMAP
  JOB --> JMAP
  IMAP --> KEY
  IMAP --> SRV
  JMAP --> SRV
```

In Textform: Drei Oberflächen rufen **ausschließlich** den Executor. Der Executor schlägt die Operation im Register nach, prüft Scope und Policy, schreibt den Audit-Eintrag und ruft erst dann den Handler. Handler arbeiten gegen den lokalen Speicher; alles, was den Server berührt, läuft über Jobs und das Provider-Interface.

### Paketübersicht

| Paket | Aufgabe | Stand im Gerüst |
|---|---|---|
| `cmd/postkauz` | `main`, ruft `cli.Run` | fertig |
| `internal/brand` | Name, Alias, Präfixe an einer Stelle | fertig |
| `internal/ops` | Register, Executor (mit Schwärzung je Operation, Fortschrittsmeldungen), `accounts.list/add/test/remove`, `sync.run`, `mailboxes.list`, `messages.get`, `threads.get`, `attachments.list`, `attachments.text` | S1.6 + Tests |
| `internal/policy` | Scope, Risiko R0–R3, `Check` | Stub + Tests |
| `internal/audit` | Event-Typ, Writer-Schnittstelle | Typ + Stubs (SQLite-Writer im Store) |
| `internal/store` | Migrationen mit Schema-Schutz und Snapshot (`VACUUM INTO`), Konten, SQLite-Audit-Writer, Spiegel (Ordner, Nachrichten, Zuordnungen, Fenster als Transaktion) | S1.5 + Tests |
| `internal/config` | TOML-Konfiguration, Pfade (Konfiguration, Daten, Datenbank, Log) | S1.1 + Tests |
| `internal/secrets` | Schlüsselbund (go-keyring), Umgebung, unsicherer Testspeicher, `Secret`-Typ | S1.1 + Tests |
| `internal/imapx` | dünne Schicht über go-imap (ADR-007, v2.0.0-beta.8 gepinnt): Dial mit Watchdog (misst Schweigen, Rückrufe ausgenommen, plus Obergrenze je Kommando; ADR-007 Nachtrag 1) und TCP-Keepalive, `Session` mit Reconnect und Backoff, Login, SASL-OAuth (Entwurf), CAPABILITY, LIST mit SPECIAL-USE/LIST-STATUS, CREATE, NOOP, EXAMINE/SELECT (CONDSTORE), UID SEARCH (ESEARCH), FETCH (Metadaten, Flags mit CHANGEDSINCE, Rohmail gestreamt), STATUS, APPEND, STORE (nur Testwerkzeuge/S3, kein `\Deleted`), IDLE mit eigenem Neustart | S1.5 + Tests |
| `internal/oauth` | Token-Quelle mit Refresh und Sperrzeit (Entwurf P3) | Entwurf + Tests |
| `internal/blob` | Blob-Store: atomar, SHA-256, optional zstd, Größenlimit | S1.2 + Tests |
| `internal/mimeparse` | MIME-Parser auf `go-message` mit Charset-Reader: Kopf, Adressen, tolerantes Datum (Zone ohne Vorzeichen, Ersatz `INTERNALDATE`), Text/HTML, Anhänge mit SHA-256 und Rückruf für den dekodierten Inhalt (`OnAttachment`); Limits für Kopf, Teile, Tiefe, Teilgröße; entpackt nie Archive | S1.2/S1.6 + Korpus-Test |
| `internal/sanitize` | Body-Bereinigung: HTML → Text mit Allowlist, versteckter Text erkennen und entfernen, Links sichtbar mit Warnungen, unsichtbare Formatzeichen entfernen, Quote-Stripper | S1.6 + Korpus-Test, Fuzz-Test |
| `internal/magic` | Typ eines Anhangs aus Magic Bytes (ZIP-Formate nur über Eintragsnamen, nie entpackt), Vergleich mit Content-Type und Endung, ausführbare Typen | S1.6 + Korpus-Test |
| `internal/extract` | Textextraktion: PDF über `pdftotext`, DOCX im eigenen Kindprozess, Text/HTML/Mail im Prozess; Zeit-, Ein-/Ausgabe- und Speichergrenzen, knappe Umgebung | S1.6 + Tests (hängender Fake-Extraktor) |
| `internal/thread` | Threading: Zuordnung über Message-ID/In-Reply-To/References (vereinigt Threads), Rückfall normalisierter Betreff im 30-Tage-Fenster für Antworten ohne Verweise; Index im Speicher und in SQLite (`internal/store`) | S1.6 + Korpus-Test |
| `internal/corpus` | Generator des synthetischen Testkorpus mit Manifest (`make corpus`); `imapload` und `cmd/corpusload` legen ihn per APPEND in einem lokalen Testserver ab (`make corpus-load`) | S1.2/S1.4 + Tests |
| `internal/provider` | `MailProvider`, `Submission` | Schnittstellen |
| `internal/provider/imap` | IMAP-Provider über `imapx`: `Connect`, Fähigkeiten, Strategie, `ListMailboxes` mit Rollen (SPECIAL-USE, Namen, Gmail-Namen) und Zustand, `Changes` (Fenster, CONDSTORE bzw. UID-Scan, UID-SEARCH-Abgleich), `FetchHeaders`, `FetchRaw`, `Idle` (eigene Verbindung); Verbindungsprüfung für `account test` | S1.5 + Tests, Integrationstest (Docker) |
| `internal/mailsync` | Sync-Engine gegen eine schmale Provider-Schnittstelle: Ordnerabgleich, Fenster, Abgleich per Message-ID/Größe bzw. `content_hash`, Blob-Store, Parser, Aufbereitung (Text, Links, versteckter Text, Anhänge mit Typprüfung), Threads, Transaktion je Fenster; Nachziehen älterer Bestände und Textextraktion am Ende jedes Laufs; `Watch` (IDLE + Intervall); `Runner` für `sync.run` | S1.6 + Tests, Leck-Tests, Integrationstest (Docker) |
| `internal/cli` | cobra-Adapter, Exit-Codes, `doctor`, Strg+C-Behandlung | `version`, `doctor`, `accounts list`, `account add/test/remove`, `sync`, `mailbox list`, `show`, `thread`, `attach list/text` (minimal) |
| `internal/mcp` | MCP-Adapter | nur Plan (`doc.go`) |

Spätere Pakete (nicht angelegt): `internal/search`, `internal/importer`. Der Sync heißt `internal/mailsync` (nicht `internal/sync`, das kollidierte mit dem Standardpaket `sync`).

`make guard` prüft neben „kein SMTP-Code" auch, dass außer `internal/imapx` kein Paket go-imap importiert (ADR-007; Tests dürfen den Fake-Server der Bibliothek nutzen) und dass go-imap auf v2.0.0-beta.8 gepinnt bleibt.

## 3. Zwei Wahrheiten (ADR-003)

Es gibt keine einzelne „Wahrheit". Zwei Datenbestände haben je eigene Zuständigkeit:

| Frage | Wahrheit | Folge |
|---|---|---|
| Was liegt **jetzt** im Postfach, welche Flags, welcher Ordner? | **Der Server** | Bei Konflikt gewinnt der Server (außer bei rein lokalen Tags). |
| Was habe ich je gesehen, was ist archiviert, was ist indiziert, wer hat was freigegeben, was wurde ausgelöst? | **Die lokale Datenbank** | Archiv, Index, Freigaben und Audit hängen nie vom Server ab. |

Konsequenzen, die als Regeln gelten:

1. **Server gewinnt beim Zustand.** Der nächste `Changes()`-Lauf überschreibt lokale Flags und Ordnerzuordnungen. Lokale Schreibaktionen sind „optimistisch mit Pending-Markierung", bis der Server sie bestätigt.
2. **Der Server löscht, das Archiv behält.** Verschwindet eine Nachricht serverseitig, setzt PostKauz `messages.deleted_remote_at` und behält Rohmail und Index. Eine Aufbewahrungs- oder Löschfrist ist eine eigene, ausdrückliche Operation (ab S3, Löschkonzept), nie ein Nebeneffekt des Sync.
3. **Anbieterwechsel synchronisiert das Archiv nie zurück.** Wer das Konto wechselt, legt ein neues Konto an; das alte Archiv bleibt lesbar. Es gibt keine Operation „Archiv in neues Postfach hochladen" (außer dem bewussten Export, S3).
4. **Erstabgleich und Import über Identität, nicht über Datum.** Ein Thunderbird-Import (mbox/Maildir) und der erste IMAP-Abgleich erkennen Duplikate per `Message-ID` und `content_hash`, mit Überlappung; „alles ab Datum X" ist verboten, weil es Lücken und Doppelte erzeugt.
5. **Entwürfe sind lokal-führend bis zum APPEND.** Ein Entwurf entsteht lokal (`drafts`), wird per IMAP APPEND in den Entwürfe-Ordner gelegt und dort vom Menschen weiterbearbeitet oder gesendet. Ab dem Zeitpunkt, an dem der Mensch ihn im Mailclient anfasst, ist die Serverkopie maßgeblich; PostKauz überschreibt sie nicht mehr.

## 4. Provider-Interface: JMAP-Semantik, IMAP-Abbildung

Das Interface (`internal/provider/provider.go`) ist nach JMAP geschnitten, weil JMAP Postfach, Nachricht, Zustand und Änderungen am saubersten beschreibt. IMAP wird darauf abgebildet. Ein späterer JMAP-Provider passt 1:1.

| Methode | JMAP | IMAP (S1) |
|---|---|---|
| `Capabilities()` | Session-Objekt | CAPABILITY nach Login: CONDSTORE, QRESYNC, UIDPLUS, IDLE, MOVE, SPECIAL-USE, NAMESPACE, `X-GM-EXT-1`, PERMANENTFLAGS |
| `ListMailboxes()` | `Mailbox/get` (mit `role`) | `LIST` mit SPECIAL-USE; sonst Namensheuristik je Anbieter; Namen aus IMAP-UTF-7 dekodiert |
| `Changes(mailbox, since, limit)` | `Email/changes` mit `sinceState` | siehe unten |
| `FetchHeaders()` | `Email/get` (Kopf) | `UID FETCH (UID FLAGS INTERNALDATE RFC822.SIZE MODSEQ BODY.PEEK[HEADER])`; ganzer Kopf statt `HEADER.FIELDS (MESSAGE-ID)`, weil GreenMail für die von go-imap gequoteten Feldnamen nichts liefert |
| `FetchRaw()` | Blob-Download | `UID FETCH BODY.PEEK[]` für ein Fenster von UIDs (streamt je Nachricht; `.PEEK`, damit `\Seen` unberührt bleibt) |
| `SetFlags()` | `Email/set` (keywords) | `UID STORE`; Keywords nur bei `KeywordsPermanent` |
| `Move()` | `Email/set` (mailboxIds) | `UID MOVE`, sonst `COPY` + `\Deleted`-Markierung ohne Expunge-Aufruf der Agentenseite (siehe Hinweis) |
| `Trash()` | `Email/set` → Papierkorb | `MOVE` in den Ordner mit Rolle `trash`; bei Gmail ausdrücklich „in den Papierkorb verschieben" |
| `Append()` | `Email/import` | `APPEND` mit Flags und Datum; UID über `APPENDUID` (UIDPLUS) |
| `Idle()` | EventSource/Push | `IDLE` auf INBOX; Polling für übrige Ordner |

**Kein Expunge.** Das Interface enthält absichtlich keine Operation, die Nachrichten endgültig löscht (ADR-002, Entscheidung 6). Wo IMAP ohne MOVE-Erweiterung nur `COPY` + `\Deleted` + `EXPUNGE` kennt, ist dieser Ersatz eine interne Angelegenheit des IMAP-Providers und beschränkt sich auf `UID EXPUNGE` der soeben kopierten UIDs (UIDPLUS); ohne UIDPLUS und ohne MOVE meldet der Provider `ErrUnsupported`, statt ein globales `EXPUNGE` auszuführen.

### Änderungserkennung (`Changes`)

Die Strategie folgt den erkannten Fähigkeiten (`Capabilities.SyncStrategy()`). **Hinweis (ADR-007):** `go-imap` v2 beta.8 kennt kein QRESYNC; bis zu einem Beitrag oder Fork läuft auch auf QRESYNC-fähigen Servern die Strategie `condstore` mit UID-SEARCH-Abgleich für Löschungen. Alle IMAP-Zugriffe laufen über `internal/imapx` mit Watchdog (Schweigen höchstens 30 s, Obergrenze 10 min je Kommando, beides konfigurierbar; Close + Reconnect mit Backoff, IDLE-Neustart alle 5 bis 10 Minuten, TCP-Keepalive; ADR-007 Nachtrag 1).



| Strategie | Voraussetzung | Neue | Flag-Änderungen | Gelöschte |
|---|---|---|---|---|
| `qresync` | CONDSTORE + QRESYNC | UIDs ab `UIDNEXT` | `FETCH … (CHANGEDSINCE modseq)` | `VANISHED (EARLIER)` |
| `condstore` | CONDSTORE | UIDs ab `UIDNEXT` | `CHANGEDSINCE` | periodischer `UID SEARCH ALL`-Abgleich |
| `uid-scan` | nichts | UIDs ab `UIDNEXT` | Flags aller UIDs in Fenstern | periodischer `UID SEARCH ALL`-Abgleich |

- **UIDVALIDITY-Wechsel** setzt `ChangeSet.Reset`. Der Aufrufer gleicht dann den Ordner per `Message-ID`/`content_hash` neu ab (Regel 4 oben), er lädt nicht blind alles neu. Umsetzung (S1.5): Die alten Zuordnungen des Ordners werden sofort gelöst (die Nachrichten bleiben, vorübergehend mit `deleted_remote_at`), dann wird der Ordner wie neu in Fenstern abgeglichen; Köpfe mit bekannter Message-ID und gleicher Größe werden nur zugeordnet, ohne den Inhalt erneut zu laden (Test: 0 erneut geladene Inhalte auf Dovecot, Stalwart, GreenMail).
- **Erstsync in Fenstern** (`limit`, `ChangeSet.More`): resume-fähig, der neue `SyncState` wird erst **nach** erfolgreicher lokaler Verarbeitung gespeichert. Ein Absturz mitten im Fenster wiederholt das Fenster; Idempotenz entsteht über den eindeutigen Schlüssel `(mailbox_id, uid)` und `content_hash`.

**Umsetzung im Detail (S1.4/S1.5, `internal/provider/imap/sync.go`, `internal/mailsync`):**

1. `Changes` wählt den Ordner per `EXAMINE` (mit `CONDSTORE`, wo vorhanden; der Sync ändert am Server nichts).
2. **Cursor** ist `mailboxes.uidnext`: Alle UIDs darunter sind gespiegelt. Neue UIDs ermittelt `UID SEARCH UID n:*` (die RFC-Eigenheit, dass `n:*` immer die höchste UID liefert, wird gefiltert). Ein Fenster umfasst höchstens `limit` UIDs; der Cursor steht dann hinter dem Fenster (`More`), die Phase des Ordners ist `initial`.
3. **Löschabgleich nur bei Bedarf:** Stimmt `EXISTS` mit „lokal bekannt + neu" überein, kann nichts gelöscht worden sein. Sonst (und beim Erstabgleich, Reset, `--full`) folgt `UID SEARCH ALL` (mit `ESEARCH` als Bereiche); das Ergebnis (`Present`) bleibt für die weiteren Fenster desselben Laufs zwischengespeichert, solange UIDVALIDITY, UIDNEXT und EXISTS gleich bleiben. Der Aufrufer leitet daraus Löschungen (lokal, nicht mehr vorhanden) und Fehlendes (vorhanden, unterhalb des Cursors, lokal unbekannt; z. B. frühere Fehler) ab.
4. **Flags erst im letzten Fenster:** mit CONDSTORE per `UID FETCH 1:* (FLAGS) (CHANGEDSINCE m)`; `m` ist im Erstsync der Stand vor dem ersten Fenster (Untergrenze), damit Änderungen während des Erstsyncs nicht verloren gehen. Ohne CONDSTORE (Strategie `uid-scan`) werden die Flags aller bekannten UIDs verglichen; nur echte Änderungen zählen.
5. Die Engine holt für neue UIDs zuerst den **Kopf**. Gibt es die Nachricht im Konto schon (gleiche Message-ID und Größe, in diesem Ordner noch nicht zugeordnet: Kopie, Gmail-Label, verschobene Nachricht, Reset), wird sie nur zugeordnet. Sonst wird die Rohmail gestreamt (je FETCH höchstens 16 MiB angekündigte Größe, wegen der Obergrenze je Kommando), in den Blob-Store geschrieben, aus der Datei geparst und über `content_hash` (SHA-256 ohne CR-Bytes) noch einmal gegen Vorhandenes geprüft.
6. Ein Fenster wird in **einer Transaktion** geschrieben (Nachrichten, Texte, Zuordnungen, Flag-Änderungen, Löschungen, Zustand); Fenster mit mehr als 32 MiB Inhalt werden in mehrere Transaktionen geteilt, der Cursor wandert erst mit der letzten.
7. Fehler einzelner Nachrichten (z. B. Größenlimit, leerer Inhalt vom Server) zählen als `errors`; der Cursor läuft weiter, der nächste Lauf holt sie über Punkt 3 nach. Fehler eines Ordners überspringen den Ordner; Verbindungs- und Anmeldefehler sowie Abbruch beenden den Lauf.
- **Reconnect mit Backoff** (exponentiell, mit Obergrenze und Jitter). `ErrAuth` wird **nicht** wiederholt: ein Audit-Ereignis und ein Hinweis in `doctor`, sonst sperren Anbieter das Konto.
- **Gmail-Eigenheiten** (`GmailExtensions`): Der Ordner „Alle Nachrichten" enthält jede Nachricht ein weiteres Mal. PostKauz behandelt Labels als Ordner und dedupliziert über `Message-ID`; eine Nachricht darf in `message_mailboxes` mehrfach vorkommen, in `messages` nur einmal. Verbindungslimit (15 je Konto) beachten: ein Pool von höchstens 3–4 Verbindungen.

### Versand (`Submission`)

`internal/provider/submission.go` beschreibt den Versandweg (SMTP, später JMAP `EmailSubmission`) und trägt die Eigenschaft `SavesSentCopy()` (Gmail legt die Kopie selbst an, Dovecot nicht). Es ist im MVP **nicht implementiert und nicht verdrahtet** (ab S4).

## 5. Operationsregister und Adapter (ADR-004)

Jede fachliche Operation ist ein Eintrag **als Daten** in `internal/ops`:

```go
type Operation struct {
    Name         string       // "objekt.verb", z. B. "messages.search"
    Scope        policy.Scope // z. B. mail:search
    Risk         policy.Risk  // R0–R3
    Idempotent   bool
    InputSchema  json.RawMessage // JSON-Schema
    OutputSchema json.RawMessage
    Summary      string       // ein Satz für Hilfe und Tool-Beschreibung
    Handler      Handler
}
```

Die Oberflächen sind **dünne, ausdrücklich geschriebene Adapter** mit je einer Bindungstabelle. Es gibt keine Reflection und keinen Generator:

| Operation | CLI | MCP-Tool | REST (später) |
|---|---|---|---|
| `accounts.list` | `postkauz accounts list` | `mail_list_accounts` | `POST /v1/ops/accounts.list` |
| `messages.search` | `postkauz search` | `mail_search` | `POST /v1/ops/messages.search` |
| `drafts.create` | `postkauz draft new` / `draft reply` | `mail_create_draft` | `POST /v1/ops/drafts.create` |

Namen, Hilfetexte, Ausgabeformate und die Auswahl, was MCP überhaupt anbietet, sind Entscheidungen und stehen deshalb von Hand in den Bindungstabellen. Was der Adapter **nicht** tut: Scope prüfen, Policy auswerten, auditieren, Fachlogik. Das tut allein der Executor.

**Vertragstests** (je Operation, ab der zweiten Operation Pflicht):

1. Die Operation ist formal gültig (`Validate`: Name, Scope, Risiko, Schemas, Handler).
2. Jede Oberfläche, die sie bindet, liefert dasselbe Ein-/Ausgabeschema wie das Register.
3. Die Operation durchläuft für jeden Scope-Satz die Kette Scope → Policy → Freigabe → Audit → Handler (verweigerte Aufrufe erreichen den Handler nie und sind auditiert).
4. Der Executor ist **fail closed**: Schlägt das Schreiben des Audit-Eintrags fehl, läuft der Handler nicht (Test vorhanden).

Umsetzung von 1, 3 und 4 (S1.5): Alle Operationen stehen in **einer** ausdrücklich geschriebenen Liste, `ops.Catalog` (`internal/ops/catalog.go`); CLI und später MCP registrieren sie über `ops.NewDefaultRegistry`. `TestContractEveryOperationRoleAndDryRun` (`internal/ops/ops_test.go`) schickt jede Katalog-Operation mit jeder Rolle aus `policy.Roles` (dazu „ohne Scopes") und fünf Trockenlauf-Varianten (Standard, `--dry-run`, `dry_run=false`, Schlüssel-Standard an, Schlüssel-Standard an mit `dry_run=false`) durch den Executor und prüft: Entscheidung wie in `docs/sicherheit.md` (unabhängig von `policy.Check` formuliert), genau ein Audit-Eintrag ohne Geheimnis, Handler nur bei Erlaubnis, **Trockenlauf, R0 und Ablehnung verändern weder Datenbank noch Schlüsselbund** (Zustandsvergleich aller Tabellen), eine erlaubte schreibende Operation wirkt, die Ausgabe trägt die Pflichtfelder ihres `OutputSchema` und `dry_run` wie entschieden. `TestContractEveryOperationFailsClosed` prüft 4 je Operation. Eine neue Operation braucht eine Zeile mit gültiger Beispiel-Eingabe in `contractInputs`, sonst scheitert der Test. Punkt 2 folgt mit den Bindungstests von S1.8/S1.9.

## 6. Policy- und Audit-Kette

```mermaid
sequenceDiagram
  participant O as Oberfläche (CLI/MCP)
  participant X as Executor
  participant P as Policy
  participant A as Audit
  participant H as Handler
  O->>X: Request(Key, Operation, Input, DryRun?)
  X->>X: Register: Operation nachschlagen
  X->>P: Check(Key, Operation, DryRun)
  P-->>X: Decision (allow, deny, needs_approval; DryRun)
  X->>A: Append(Event: Ergebnis der Prüfung)
  alt allow
    X->>H: Handler(Input, DryRun)
    H-->>X: Output oder Fehler
    opt Fehler
      X->>A: Append(Event: error)
    end
    X-->>O: Result
  else deny oder needs_approval
    X-->>O: PolicyError (Exit 3 bzw. 4, MCP-Status)
  end
```

- **Reihenfolge:** Policy-Entscheid → Audit-Eintrag → Handler. Der Audit-Eintrag steht **vor** der Wirkung. Fehler im Handler erzeugen einen zweiten Eintrag (`error`).
- **Trockenlauf:** `dry_run` gilt für jede schreibende Operation. Standard je Schlüssel (`dry_run_default`), ausdrücklich überschreibbar. Der Handler bekommt `Call.DryRun` und darf dann nichts verändern.
- **`needs_approval` ist kein Fehler der Operation**, sondern ein Zustand: MCP liefert ihn als Ergebnis (nicht als `isError`), die CLI als Exit-Code 4.
- **Freigabe-Erzwingung liegt im Executor**, nicht in einer Oberfläche. Ein zweiter MCP-Server oder später REST kann sie nicht umgehen.

Stand im Gerüst: `policy.Check` ist ein Stub mit den Regeln „fehlender Scope → Deny, R0/R1 → Allow, R2 → Allow, R3 → NeedsApproval, Trockenlauf → Allow mit `DryRun`". Policy-Datei, Limits und die Freigabe-Zustandsmaschine kommen mit S2 (siehe [sicherheit.md](sicherheit.md)).

## 7. Jobs und Prozessmodell

Alles, was den Server berührt oder lange dauert, läuft als **Job** (Tabelle `jobs`, kein Redis, kein externer Broker):

| Job-Typ | Auslöser | Inhalt |
|---|---|---|
| `sync` | Intervall, IDLE-Signal, `postkauz sync`, `mail_sync_now` | `Changes()` eines Ordners, Köpfe und Rohmails holen, Blobs schreiben |
| `index` | nach `sync`/`import` | FTS-Zeilen und Wortstamm schreiben (die Body-Bereinigung macht seit S1.6 der Sync beim Speichern) |
| `extract` | nach `sync` | Anhangstext in Subprozess mit Zeit- und Speicherlimit (S1.6: noch ohne Tabelle `jobs`, am Ende jedes Sync-Laufs mit Zeitbudget 60 s; Rest im nächsten Lauf) |
| `import` | `postkauz import mbox/maildir` | Einlesen mit Message-ID-/Hash-Abgleich |
| `export` | `postkauz export`, täglich (Audit) | mbox/EML-Verzeichnis, Audit-JSONL mit Prüfsumme |

Regeln: Jobs sind **idempotent** und wiederholbar; Sperren über `locked_by`/`locked_at` mit Ablauf; `attempts` und `last_error` für Backoff; ein Job je (Konto, Ordner) gleichzeitig.

Prozesse im MVP:

- `postkauz sync --watch` (S1.5): Dauerläufer im Vordergrund. Er synct sofort, danach alle 5 Minuten (`--interval`) und bei jeder IDLE-Meldung der INBOX (eigene Verbindung, IDLE-Neustart alle 5 bis 10 Minuten, Standard 8; ein Blackhole fällt beim Neustart am unbeantworteten `DONE` auf). Verbindungsfehler eines Laufs beenden ihn nicht (nächster Lauf, IDLE nach 30 s neu), ein Anmeldefehler schon. Die Tabelle `jobs` nutzt er noch nicht (erst mit Index und Extraktion, S1.6/S1.7). Später als Windows-Aufgabe, systemd-Dienst oder Docker-Container.
- `postkauz mcp` (S1): kurzlebiger stdio-Prozess je Agent-Sitzung, **ohne** eigenen Sync; er liest die Datenbank und kann Jobs einreihen.
- `postkauz …` (CLI): kurzlebig.

Alle Prozesse teilen **eine SQLite-Datei** im WAL-Modus mit `busy_timeout` (5 s); schreibende Transaktionen beginnen mit `BEGIN IMMEDIATE` (`_txlock=immediate`), damit ein zweiter Schreiber wartet, statt mit `SQLITE_BUSY` abzubrechen (getestet). Zwei schreibende Syncs **desselben Kontos** schließt eine Dateisperre je Konto aus (`sync-<id>.lock`, flock bzw. `LockFileEx`); der zweite endet mit `sync_running`. Es gibt genau einen schreibenden Dauerläufer je Datenbank; die kurzlebigen Prozesse schreiben nur kurze Transaktionen (Audit, Entwürfe, Job-Einreihung). Die Datenbank darf nie in einem Cloud-synchronisierten Ordner liegen (siehe [datenmodell.md](datenmodell.md)).

## 8. Namen (Arbeitstitel)

Der Projektname ist ein Platzhalter. Umbenennen heißt: `internal/brand/brand.go`, Modulpfad in `go.mod` samt Importen (`go mod edit -module …` plus `sed`), `BINARY`/`ALIAS` im Makefile und Verzeichnis `cmd/postkauz`. Doku und Meldungen leiten sich aus `brand` ab, soweit es der Aufwand rechtfertigt; die Doku nennt „PostKauz" im Text und wird bei einer Umbenennung per Suchen/Ersetzen angepasst.
