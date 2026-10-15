# Testkorpus

Stand: Phase 0. Es existiert noch kein Korpus und kein Generator; dieses Dokument legt die Anforderungen fest, damit der Spike und S1 dagegen bauen. Regeln für das Verzeichnis: [`testdata/README.md`](../testdata/README.md).

**Grundregel:** Entwickelt und getestet wird **nie gegen ein echtes Postfach**. Es gibt zwei Korpora: einen **synthetischen** (liegt im Repository bzw. wird deterministisch erzeugt) und einen **anonymisierten** aus echten Mails (liegt **außerhalb** des Repositorys, bis ein Mensch ihn freigegeben hat).

## 1. Synthetischer Korpus

Zweck: Korrektheit von MIME-Parser, Threading, Sync, Suche und Import reproduzierbar prüfen, in CI und im Docker-Testserver.

**Erzeugung:** ein deterministischer Generator (`make corpus`, fester Zufallssamen), der `.eml`-Dateien und ein Manifest `corpus.json` mit den **erwarteten Ergebnissen** schreibt. Das Manifest ist die Wahrheit der Tests; Parser und Suche werden dagegen geprüft, nicht gegeneinander.

**Umfang:** 1.000 Nachrichten als Standardkorpus; ein Lasttest-Modus erzeugt bis 50.000 bzw. 100.000 Nachrichten (nicht im Repository, nur lokal und nachts).

**Pflichtinhalte:**

| Bereich | Fälle |
|---|---|
| Zeichensätze | UTF-8, ISO-8859-1/-15, Windows-1252, UTF-16-Anhänge, Umlaute und ß in Betreff, Namen und Body, Emoji, falsch deklarierte Charsets |
| Header | RFC 2047 (`=?UTF-8?B?…?=`, `?Q?`), mehrzeilige (gefaltete) Header, RFC 2231-Dateinamen, doppelte `Message-ID`, fehlende `Message-ID`, fehlendes/ungültiges `Date`, sehr lange Betreffzeilen, Kommentare in Adressen, Gruppenadressen |
| Struktur | `text/plain`, `text/html`, `multipart/alternative`, `multipart/mixed`, `multipart/related` mit eingebetteten Bildern, verschachtelte `message/rfc822`, fehlende Boundaries, abgeschnittene Nachrichten |
| Threads | lange Ketten, Verzweigungen, fehlende Zwischenglieder, Betreffwechsel mitten im Thread, `Re:`/`AW:`/`WG:`/`Fwd:`-Varianten, Antworten ohne `References` (Rückfall über Betreff und Zeitfenster) |
| Anhänge | PDF, DOCX, XLSX, ZIP, Bilder, ohne Dateinamen, Endung passt nicht zum Inhalt (Magic-Bytes-Test), ausführbare Typen, übergroße Dateien, eine **Zip-Bombe** (klein, entpackt riesig; darf nie entpackt werden) |
| Bösartig/Injektion | HTML mit `display:none`, `font-size:0`, weiß auf weiß und darin Anweisungen an einen Agenten; Mails, die „ignoriere alle Regeln" enthalten; Links mit Punycode/Homoglyphen; `javascript:`-Links; Tracking-Pixel; `Authentication-Results` mit `dkim=fail` |
| Kaputt | falsche Zeilenenden, 8-Bit-Header, NUL-Bytes, doppelte Header, Header ohne Doppelpunkt, riesige HTML-Mails |
| Listen | `List-Id`, `List-Unsubscribe`, Newsletter-typische Struktur |
| Suche | ein **Suchtestsatz**: Anfragen mit erwarteten Treffern — Flexionen („Rechnung"/„Rechnungen"), Umlaute mit und ohne Diakritika („prüfen"/„pruefen"), Teilwörter in Komposita („Vollstreck" ↔ „Zwangsvollstreckung", „Abrechnung" ↔ „Nebenkostenabrechnung"), Operatoren (`from:`, `after:`, `has:attachment`), erwartete **Nicht**treffer |

**Inhalt:** erfundene Personen und Organisationen, Adressen ausschließlich aus reservierten Domains (`example.com`, `example.org`, `example.net`, `*.invalid`), erfundene Nummern. Texte werden aus Bausteinen zusammengesetzt, nicht aus echten Mails kopiert.

**Docker-Testserver:** Der Korpus wird per Skript (IMAP `APPEND`) in Dovecot und Stalwart geladen (`deploy/testmail`, `make testmail-up`). Ein Ladevorgang erzeugt dieselbe Ordnerstruktur und dieselben UIDs wie beim letzten Mal (frische Volumes).

## 2. Anonymisierter Korpus aus echten Mails

Zweck: Fälle, die ein Generator nicht erfindet (echte Absender-Eigenheiten, Clients, Kodierungsfehler). Er kommt vom Betreiber und ist **optional**.

**Grundsätze:**

1. **Nicht im öffentlichen Repository.** Der anonymisierte Korpus liegt zunächst außerhalb des Repositorys (oder in einem privaten Verzeichnis, das `.gitignore` ausschließt). Er wird erst übernommen, wenn der Betreiber ihn nach Stichprobenprüfung freigibt und das PII-Gate (unten) leer ist.
2. **Strukturerhaltend anonymisieren:** Header-Struktur, Charsets, MIME-Baum, Größen, Reihenfolge und Thread-Beziehungen bleiben; Inhalte werden ersetzt.
3. **Ersetzungsregeln:** Personen- und Firmennamen → erfundene Namen (konsistent je Original, damit Threads erhalten bleiben); Mailadressen → `nutzerNNN@example.org`; Domains → `example.*`; Telefon-, Konto-, Steuer- und Ausweisnummern, IBAN/BIC, Kartennummern, Adressen, Aktenzeichen, Kennwörter, Links mit Token → Platzhalter gleicher Form; Anhänge → **erzeugte Stellvertreter** gleichen Typs und ähnlicher Größe (kein Originalinhalt); Freitext → durch zufälligen Text gleicher Länge ersetzt, wo er nicht für einen Test gebraucht wird.
4. **Besondere Kategorien** (Gesundheit, Religion, Gewerkschaft u. a.) werden nie übernommen; solche Mails werden ausgesondert.
5. **Herkunftsprotokoll:** Welche Mails wurden mit welchem Werkzeug und welcher Regelversion anonymisiert; ohne Rückverweis auf Originalpostfächer.
6. **Menschliche Stichprobe:** Mindestens 5 % der Mails, mindestens aber 50, werden von einem Menschen gelesen, bevor der Korpus freigegeben wird. Findet sich ein Treffer, wird der gesamte Lauf verworfen und die Regeln werden verschärft.

## 3. PII-Gate

Ein automatischer Schutz vor versehentlich eingechecktem Personenbezug. Er blockiert den Commit bzw. den CI-Lauf.

- **Was geprüft wird:** alle versionierten Textdateien (Quelle, Doku, Testdaten, `.eml`). Muster: E-Mail-Adressen außerhalb der erlaubten Domains (`example.com`, `example.org`, `example.net`, `*.invalid`, `localhost`), IBAN-Muster, Telefonnummernmuster, Kartennummern (Luhn), und eine **lokale Sperrliste** mit Namen und Begriffen, die nie im Repository stehen dürfen (die Liste selbst liegt außerhalb des Repositorys und wird per Umgebungsvariable `POSTKAUZ_PII_DENYLIST` eingebunden).
- **Wo es läuft:** lokal als `make pii-check` (vor jedem Commit, siehe CLAUDE.md) und in der CI. Im Gerüst ist `scripts/pii-check.sh` eine einfache, aber funktionierende Erstfassung für E-Mail-Adressen, IBAN-Muster und die Sperrliste.
- **Ein Treffer ist ein Fehler**, kein Hinweis. Ausnahmen gibt es nur als ausdrücklich begründeten Eintrag in `scripts/pii-allow.txt`, den ein Mensch prüft.
- **Grenzen:** Muster finden kein Wissen („der Mieter aus Haus 3"). Deshalb bleibt die menschliche Stichprobe unverzichtbar; das Gate ist ein Netz, keine Garantie.
- **Nach einem Fehlgriff:** Ein versehentlich eingecheckter echter Inhalt wird nicht nur gelöscht, sondern aus der Git-Historie entfernt, bevor das Repository veröffentlicht oder gepusht wird. Solange das Repository nur lokal liegt, genügt ein Umschreiben der Historie; danach gilt der Inhalt als kompromittiert.
