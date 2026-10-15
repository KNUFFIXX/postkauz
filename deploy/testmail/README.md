# Testserver für die Entwicklung

Drei lokale Mailserver für Integrationstests von PostKauz (Arbeitstitel). Start und Stopp:

```sh
make testmail-up        # docker compose -f deploy/testmail/docker-compose.yml up -d
make testmail-down      # stoppt und löscht die Volumes (frischer Zustand beim nächsten Start)
```

> **Nur für lokale Tests. Standard-Zugangsdaten, öffentlich bekannt. Nie im Internet betreiben, nie mit echten Postfächern oder echten Mails befüllen.** Alle Ports sind an `127.0.0.1` gebunden; wer sie ändert, übernimmt die Verantwortung.

## Server und Ports

| Server | Zweck | Host-Port | Inhalt | Zugang |
|---|---|---|---|---|
| Dovecot (`dovecot/dovecot:2.4.5`) | verbreitetster IMAP-Server, steht für viele Hoster | 11143 IMAP, 11993 IMAPS (selbstsigniert) | – | Demo-Modus des Images: beliebiger Nutzername, Passwort `pass` |
| Stalwart (`stalwartlabs/mail-server:v0.11.8`) | moderner Server mit CONDSTORE/QRESYNC und später JMAP | 12143 IMAP, 12993 IMAPS, 12587 Submission, 18080 Verwaltung | – | Verwaltung: Nutzer `admin`, Passwort steht bei jedem Start im Log (`docker compose -f deploy/testmail/docker-compose.yml logs stalwart`) |
| GreenMail (`greenmail/standalone:2.1.14`) | schnell, ohne erweiterte IMAP-Fähigkeiten (Fallback-Pfad `uid-scan`) | 13143 IMAP, 13993 IMAPS, 13025 SMTP, 18081 API | – | Nutzer `test`, Passwort `pass`, Adresse `test@example.org` |

## Einrichten und Integrationstests (Stand S1.3)

```sh
make testmail-up        # Server starten
make testmail-setup     # Stalwart: IPv4-Listener, Domain example.org, Konto test / pass (wiederholbar)
make integration        # Integrationstests gegen alle drei Server (POSTKAUZ_IT=1)
make integration IT=dovecot,greenmail   # Auswahl
```

| Server | Port im Test | Anmeldung | Erwartet (S1.3) |
|---|---|---|---|
| Dovecot 2.4 | 11143, Klartext | `it-user@example.org` / `pass` | CONDSTORE, QRESYNC, SPECIAL-USE, LIST-STATUS; Strategie `condstore`; Rollen INBOX/Sent/Drafts/Trash/Junk |
| Stalwart 0.11.8 | 12993, TLS (selbstsigniert, nur localhost) | `test` / `pass` | wie Dovecot; Rollen nur mit `RETURN (SPECIAL-USE)`: „Sent Items", „Drafts", „Deleted Items", „Junk Mail" |
| GreenMail | 13143, Klartext | `test` / `pass` | kein CONDSTORE, kein SPECIAL-USE, kein LIST-STATUS; Strategie `uid-scan`; der Test legt „Gesendet", „Entwürfe", „Papierkorb", „Spam", „Kunden" an und prüft die Rollen aus den Namen |

Seit S1.4/S1.5 prüft `make integration` zusätzlich den Sync je Server (`internal/mailsync`): Korpus unter einem eigenen Ordner `PKIT…` laden, Erstsync mit simuliertem Absturz und Wiederaufnahme (1.000 Nachrichten, keine Doppelten), Leerlauf-Abgleich, Flag-Änderung, Löschung und neue Nachricht von außen, UIDVALIDITY-Wechsel, IDLE. Der Testordner wird danach gelöscht.

`make check` und die CI überspringen diese Tests (ohne `POSTKAUZ_IT`).

## Korpus laden (Abnahme D1.3)

```sh
make corpus-load                                           # Dovecot, Nutzer tester / pass
make corpus-load LOAD_IMAP=127.0.0.1:13143 LOAD_USER=test  # GreenMail
go run ./internal/corpus/cmd/corpusload -imap 127.0.0.1:12993 -user test -password pass   # Stalwart (IMAPS)
```

Das Werkzeug erzeugt den Standardkorpus (1.000 Nachrichten, Samen 42; identisch mit `make corpus`) und legt ihn mit Flags und Datum in INBOX, Archiv/2024, Archiv/2025 und Drafts ab. Es verweigert andere Hosts als localhost und bricht ab, wenn ein Zielordner schon Nachrichten enthält (`-force` lädt trotzdem, dann gibt es Dubletten auf dem Server). `-mark-seen` markiert die erste ungelesene Nachricht der INBOX als gelesen und nennt ihre Message-ID (Abnahme D1.6). Ist die Variable gesetzt und ein Server nicht erreichbar, schlägt der Test fehl.

## Bekannte Einschränkungen

- **Stalwart** startet ohne Domain und Konto und lauscht im Container zunächst nur auf IPv6; `make testmail-setup` behebt beides (nach jedem `testmail-up` mit frischem Volume). Auf dem Klartext-Port 12143 erlaubt Stalwart kein LOGIN. Das Image ist auf 0.11.8 festgelegt, weil `stalwartlabs/stalwart:latest` (0.16) im Bootstrap-Modus startet und die Ersteinrichtung über eine Web-Oberfläche im Internet verlangt.
- **Stalwart 0.11.8** lässt seine zwischengespeicherte Ordnerliste nach vielen schnellen Schreibvorgängen manchmal veralten, bis zur nächsten Änderung am Konto (Befund S1.4): Neue Sitzungen sehen eben angelegte Ordner dann nicht. Der Korpus-Lader nimmt deshalb nach einer Sekunde Pause eine kleine Änderung vor (Keyword setzen und entfernen). Fehlt nach dem Laden ein Ordner im Sync, `sync` einfach noch einmal aufrufen.
- **GreenMail 2.1** liefert für `BODY.PEEK[HEADER.FIELDS ("Message-ID")]` (go-imap setzt Feldnamen in Anführungszeichen) einen leeren Inhalt; der Sync holt deshalb den ganzen Kopf.
- Alle drei Images sind auf geprüfte Versionen festgelegt (Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14; Betreiber-Entscheid 15.10.2026). Ein Wechsel heißt: Tag ändern, `make testmail-down testmail-up testmail-setup integration`, Befunde in `docs/roadmap.md`.
