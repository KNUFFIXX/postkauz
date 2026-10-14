# Mailwerk – Phase-0-Spike S0

Prüft, ob Go 1.24 mit `github.com/emersion/go-imap/v2` als Protokollbasis für Mailwerk taugt.
Ergebnis und Zahlen: siehe [REPORT.md](REPORT.md). Rohdaten der Läufe: `reports/*.json`.

*English: Phase-0 spike for the Mailwerk agent-first mail client. Tests go-imap v2 against Dovecot, Stalwart and GreenMail in Docker with a synthetic 1,000-mail corpus. See REPORT.md (German).*

Nur synthetische Daten (`*.example.test`), keine echten Konten, kein Mailversand.

## Inhalt

| Pfad | Zweck |
|---|---|
| `testmail/docker-compose.yml` | Dovecot 2.4.5 (Pflicht), Stalwart 0.11.8 (Soll), GreenMail (optional) |
| `testmail/dovecot/zz-spike.conf` | Dovecot-Overrides: nur IPv4, Klartext-Login lokal erlaubt |
| `testmail/stalwart-setup.sh` | Stalwart: IPv4-Patch, Domain + Konto `spike`/`pass` anlegen |
| `testmail/gen_corpus.py` | erzeugt 1.000 `.eml` + `manifest.json` (Sollwerte) |
| `testmail/seed.py` | lädt den Korpus per IMAP APPEND in INBOX, Archiv/2024, Archiv/2025, Drafts |
| `cmd/s0` | Go-Spike: caps, sync, mutate, draft, idle, qresync, scenario, parse, fts |

## Reproduktion

Voraussetzungen: Docker (mit Compose), Go 1.24, Python 3.

```sh
cd spike/s0   # im Repository-Wurzelverzeichnis beginnen
# 0) Docker-Daemon (im Cloud-Container ggf. von Hand)
dockerd &> /tmp/dockerd.log &   # warten bis `docker info` geht

# 1) Testserver starten
cd testmail
docker compose up -d
sh stalwart-setup.sh            # nur Stalwart; Admin-Passwort kommt aus den Logs
cd ..

# 2) Korpus erzeugen und seeden (Dovecot ~55 s, Stalwart ~45 s, GreenMail ~46 s)
python3 testmail/gen_corpus.py --out corpus
python3 testmail/seed.py --port 31143 --user spike --password pass --corpus corpus
python3 testmail/seed.py --port 32993 --tls --user spike --password pass --corpus corpus
python3 testmail/seed.py --port 33143 --user spike@mw.example.test --password pass --corpus corpus

# 3) Bauen
go vet ./...
go build -o s0 ./cmd/s0

# 4) Einzelschritte (Beispiel Dovecot)
./s0 caps   -addr 127.0.0.1:31143
./s0 sync   -addr 127.0.0.1:31143 -data data/dovecot -fresh   # Lauf 1: Erstsync
./s0 mutate -addr 127.0.0.1:31143 -data data/dovecot          # Änderungen von außen
./s0 sync   -addr 127.0.0.1:31143 -data data/dovecot          # Lauf 2: inkrementell + Prüfung
./s0 draft  -addr 127.0.0.1:31143 -data data/dovecot
./s0 idle   -addr 127.0.0.1:31143 -data data/dovecot -kick-cmd "docker exec mw-dovecot doveadm kick spike"

# 5) Gesamtszenario je Server (enthält 4) und schreibt reports/)
./s0 scenario -server dovecot   -addr 127.0.0.1:31143 -data data/dovecot \
   -kick-cmd "docker exec mw-dovecot doveadm kick spike" -out reports/scenario-dovecot.json
./s0 scenario -server stalwart-0.11.8 -addr 127.0.0.1:32993 -tls -insecure -data data/stalwart \
   -out reports/scenario-stalwart.json
./s0 scenario -server greenmail -addr 127.0.0.1:33143 -user spike@mw.example.test -data data/greenmail \
   -out reports/scenario-greenmail.json

# 6) Offline-Tests
./s0 parse -out reports/parse.json   # go-message vs. enmime, Threading
./s0 fts   -out reports/fts.json     # FTS5-Tokenizer, Umlaut-/Teilwortsuche

# 7) Cross-Compile
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/s0.exe ./cmd/s0
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o /tmp/s0-darwin ./cmd/s0
```

`-insecure` schaltet die Zertifikatsprüfung ab und ist **nur** für die selbstsignierten Docker-Testserver gedacht.

Hinweis: Das `scenario` verändert die INBOX der Testserver (Flags, Löschungen, Test-Mails).
Für exakt reproduzierbare Ausgangszahlen Server neu anlegen (`docker compose down -v && docker compose up -d`) und neu seeden.
