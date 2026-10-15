# Architekturentscheidungen (ADR)

Format je Eintrag: Kontext · Entscheidung · Folgen · Status. Neue Entscheidungen bekommen die nächste Nummer; ein überholter Eintrag bleibt stehen und erhält den Status „ersetzt durch ADR-NNN". Stand: 14.10.2026.

## ADR-001: Sprache Go

- **Kontext:** Das Werkzeug soll auf Windows, Linux und macOS laufen, als Dauerläufer auf einem kleinen Server und als kurzlebiger Prozess für Agenten. Entwickelt wird in KI-Sitzungen mit kurzen Build-Zyklen; der Betreiber ist kein Programmierer und soll eine einzelne Datei installieren können. Verglichen wurden Python, TypeScript, Go und Rust.
- **Entscheidung:** **Go** (Zielversion 1.27, siehe ADR-008; bis zur Anhebung gilt 1.24). Statische Binärdatei je Plattform, Cross-Compile ohne C-Compiler, Builds in Sekunden, offizielles MCP-SDK. IMAP über `github.com/emersion/go-imap/v2`, CLI über `github.com/spf13/cobra`, MCP über das offizielle `github.com/modelcontextprotocol/go-sdk`, Secrets über `github.com/zalando/go-keyring`, Konfiguration TOML, Logging `log/slog`.
- **Folgen:** `go-imap` v2 ist eine Beta-Version (ohne QRESYNC, ohne Kommando-Timeouts); das **Gate** (Phase-0-Spike) hat das geprüft, Ergebnis und Auflagen in ADR-007. Rust ist verworfen (Aufwand, Build-Zeiten). JMAP-Bibliotheken in Go sind dünn; ein schlanker eigener JMAP-Client ist später denkbar.
- **Status:** akzeptiert. **Gate bestanden mit Auflagen** (14.10.2026, `spike/s0/REPORT.md`; Auflagen in ADR-007 und ADR-008).

## ADR-002: MVP ist draft-only

- **Kontext:** Das größte Risiko eines Agenten mit Mailzugang ist, dass eine eingeschleuste Anweisung eine Mail **versendet** oder Daten löscht. Versand ist außerdem der aufwendigste und fehleranfälligste Teil (SMTP, Gesendet-Kopie, Limits, Rückruf).
- **Entscheidung:** Der MVP enthält **keinen SMTP-Code**. Entwürfe mit korrekten `Message-ID`/`In-Reply-To`/`References` werden per IMAP `APPEND` in den Entwürfe-Ordner gelegt; der **Mensch sendet** im gewohnten Mailclient; PostKauz erkennt den Versand im Ordner „Gesendet" (`sent_by_human`). Agenten erreichen Löschen nur bis zum Papierkorb; das Provider-Interface hat kein Expunge; Regeln werden von Agenten nur vorgeschlagen.
- **Folgen:** Rule of Two ist im MVP strukturell erfüllt (ein lesender Schlüssel kann nicht nach außen wirken). Der Mensch bleibt der Engpass beim Senden, gewollt. Die Erkennung des Versands braucht einen Rückfall, weil manche Clients beim Senden die `Message-ID` ändern (S2 misst das). Versand mit Freigabe folgt frühestens in S4.
- **Status:** akzeptiert.

## ADR-003: Zwei Wahrheiten

- **Kontext:** Ein lokaler Spiegel kann entweder bloßer Cache des Servers sein oder ein eigenständiges Archiv. Als Cache verliert er bei Löschung, Anbieterwechsel oder Fehlern des Servers genau das, wofür man ihn will; als alleinige Wahrheit weicht er vom Postfach ab.
- **Entscheidung:** **Der Server ist die Wahrheit für den aktuellen Postfachzustand** (Flags, Ordner, Existenz). **Die lokale Datenbank ist die Wahrheit für Beobachtung, Archiv, Index, Freigaben und Audit.** Ein Anbieterwechsel synchronisiert das Archiv nie in das neue Postfach zurück. Import und Erstabgleich laufen über `Message-ID`/`content_hash` mit Überlappung, nicht über „ab Datum".
- **Folgen:** Serverseitig gelöschte Nachrichten bleiben lokal (`deleted_remote_at`); Löschung ist eine eigene, ausdrückliche Operation (Löschkonzept, S3). Konflikte beim Zustand löst „Server gewinnt", ausgenommen lokale Tags. Das Archiv wächst und braucht Sicherung und ein Löschkonzept.
- **Status:** akzeptiert.

## ADR-004: Operationsregister als Tabelle mit expliziten Adaptern

- **Kontext:** CLI, MCP und später REST müssen dieselben Fähigkeiten mit denselben Regeln bieten. Drei Implementierungen driften auseinander; eine generische Erzeugung per Reflection ist schwer zu verstehen, zu testen und zu kuratieren (MCP soll bewusst nur eine Teilmenge mit eigenen Namen zeigen).
- **Entscheidung:** Jede Operation ist ein **Eintrag als Daten** (Name, Ein-/Ausgabe-JSON-Schema, Scope, Risikostufe R0–R3, Idempotenz, Handler) in `internal/ops`. Ein **Executor** führt die feste Kette Scope → Policy → Freigabe → Audit → Handler aus. CLI, MCP und REST sind **dünne, handgeschriebene Adapter** mit je einer Bindungstabelle. Keine Reflection, kein Code-Generator.
- **Folgen:** Eine neue Operation heißt: Registereintrag, Test, je Adapter eine Bindungszeile. Etwas Schreibarbeit, dafür lesbar und kuratierbar. Ein **Vertragstest** prüft je Operation Schema-Gleichheit über die Oberflächen und die Kette. Der Executor ist fail closed bei Audit-Fehlern.
- **Status:** akzeptiert.

## ADR-005: SQLite ohne CGO

- **Kontext:** Metadaten, Volltextindex, Entwürfe, Freigaben und Audit brauchen eine eingebettete, transaktionale Datenbank. Cross-Compile von Linux nach Windows und macOS ohne C-Compiler ist ein Hauptgrund für Go (ADR-001).
- **Entscheidung:** **`modernc.org/sqlite`** (reines Go; FTS5 und `contentless_delete` enthalten, im Test belegt), **`CGO_ENABLED=0`** überall. WAL-Modus, Fremdschlüssel an, `STRICT`-Tabellen. Rohmails nie in SQLite, sondern im Blob-Store. Volltext über FTS5 (Unicode-Tokenizer plus Trigram) mit app-seitigem deutschem Stemmer; Bleve bleibt Option.
- **Folgen:** Die Version ist an die Go-Version gekoppelt: Mit Go 1.24 ist **v1.46.1** (SQLite 3.51.2) die letzte Version, die `go 1.24` verlangt (v1.46.2 verlangt 1.25, v1.60.1 verlangt 1.26). Bis zur Go-Anhebung (ADR-008) bleibt `go.mod` bei v1.45.0 (geprüft); v1.46.x ist ebenfalls zulässig, höhere Versionen erst nach der Anhebung. Der Spike hat auf v1.46.1 FTS5 (unicode61, trigram, `contentless_delete`) und Cross-Compile bestätigt. Der Treiber ist etwas langsamer als die C-Bibliothek; bei 100.000 Nachrichten wird das mit dem Suchkriterium (unter 300 ms) gemessen. Die Datenbank liegt nie in einem Cloud-synchronisierten Ordner.
- **Status:** akzeptiert.

## ADR-006: Lizenz Apache-2.0

- **Kontext:** Das Projekt soll öffentlich sein, mit anderen Werkzeugen des MCP-Ökosystems kompatibel bleiben und Beiträge nicht durch Lizenzhürden bremsen. Es läuft lokal; die Netzwerkklausel der AGPL wäre weitgehend wirkungslos und würde Beitragende abschrecken.
- **Entscheidung:** **Apache-2.0** (Patentklausel, weit verbreitet). `LICENSE` mit Volltext, `NOTICE` mit Urheberzeile. Beiträge unter derselben Lizenz, mit Developer Certificate of Origin (Sign-off); **keine CLA**.
- **Folgen:** Eine spätere Relizenzierung bräuchte die Zustimmung aller Beitragenden. Abhängigkeiten müssen mit Apache-2.0 vereinbar sein (MIT, BSD, Apache; keine GPL/AGPL-Abhängigkeiten ohne neuen ADR).
- **Status:** akzeptiert.

## ADR-007: go-imap v2 (beta.8) mit Auflagen

- **Kontext:** Der Phase-0-Spike (`spike/s0/REPORT.md`, 14.10.2026) hat `github.com/emersion/go-imap/v2` v2.0.0-beta.8 gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail geprüft: Erstsync in Fenstern, Inkrementalsync, Löschungserkennung, Draft-APPEND, IDLE und Reconnect laufen fehlerfrei. Lücken der Bibliothek: Beta-Stand, kein QRESYNC (Enable wird clientseitig abgelehnt), keine Kommando-Timeouts (Login und IDLE-Close hängen bei schweigendem Server, Blackhole wird nicht erkannt, IDLE-Neustart fest nach 28 Minuten), Standard-`WordDecoder` ohne Zeichensätze (16 % der Betreffzeilen blieben roh), kein Raw-Command-Ausgang (QRESYNC, `X-GM-EXT-1` nur per Fork).
- **Entscheidung:** go-imap v2 wird verwendet, **mit diesen Auflagen**: (1) Zugriff nur über eine dünne eigene Schicht `internal/imapx`; Version `v2.0.0-beta.8` gepinnt; Fork-Bereitschaft (die Schicht kapselt alles, was ein Fork ändern würde). (2) **Watchdog:** Deadline je Kommando, bei Überschreitung Verbindung schließen und mit Backoff neu aufbauen; IDLE selbst alle 5 bis 10 Minuten neu starten; TCP-Keepalive über `net.Dialer.KeepAliveConfig`. (3) **Immer** `WordDecoder` mit `go-message/charset` setzen, per Test abgesichert. (4) **Löschungen per UID-SEARCH-Abgleich** statt QRESYNC; QRESYNC später per Beitrag an die Bibliothek oder per Fork. (5) **Parser: `go-message`**, nicht `enmime` (enmime erzeugte Mojibake bei korrekt deklariertem UTF-8 und war siebenmal langsamer).
- **Folgen:** Der UID-Abgleich kostet linear mit der Ordnergröße und ist für 50.000+ Nachrichten zu messen. Stalwarts kontoweites `HIGHESTMODSEQ` löst bei jeder Änderung leere `CHANGEDSINCE`-Abfragen in allen Ordnern aus (akzeptiert). Die Anwendung fängt fehlendes `Date` (Nullzeit, Ersatz `INTERNALDATE`) und die Zeitzone „0100" selbst ab. `X-GM-EXT-1` (Gmail) ist ohne Fork nicht erreichbar und bleibt als offener Punkt; Gmail wird zunächst als gewöhnlicher IMAP-Server behandelt. Ein Wechsel auf Python ist nicht nötig; ADR-001 bleibt bestehen.
- **Status:** akzeptiert (14.10.2026).

## ADR-008: Go-Version

- **Kontext:** Go 1.24 erhält seit Erscheinen von Go 1.26 (Februar 2026) keine Sicherheitsupdates mehr; aktuell ist 1.27.x. Das neueste `modernc.org/sqlite` (v1.60.1) verlangt Go 1.26. `go get` hebt die `go`-Zeile in `go.mod` still an, wenn eine Abhängigkeit mehr verlangt (im Spike und im Gerüst passiert).
- **Entscheidung:** **Zielversion Go 1.27.** Die Anhebung ist **Schritt 0** von `auftraege/A01_phase0.md`. Die CI setzt `GOTOOLCHAIN=local` und installiert die Zielversion ausdrücklich; Entwicklungsumgebungen setzen ebenfalls `GOTOOLCHAIN=local`. Bis zur Anhebung bleibt `go.mod` bei `go 1.24.0` und `modernc.org/sqlite` bei v1.45.0 (v1.46.x zulässig, nichts darüber).
- **Folgen:** Nach der Anhebung: `go`-Zeile in `go.mod`, `modernc.org/sqlite` auf die dann aktuelle Version, CLAUDE.md, CONTRIBUTING.md, README und CI nachziehen; Cross-Compile-Check wiederholen. Die Abhängigkeiten (go-imap, go-message, cobra) sind davon nicht betroffen. Kein Sicherheitsrisiko durch eine veraltete Toolchain im Release-Build.
- **Status:** akzeptiert (14.10.2026); Umsetzung offen (A01, Schritt 0).
