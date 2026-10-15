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
| Dovecot (`dovecot/dovecot`) | verbreitetster IMAP-Server, steht für viele Hoster | 11143 IMAP, 11993 IMAPS (selbstsigniert) | – | Demo-Modus des Images: beliebiger Nutzername, Passwort `pass` |
| Stalwart (`stalwartlabs/mail-server:v0.11.8`) | moderner Server mit CONDSTORE/QRESYNC und später JMAP | 12143 IMAP, 12993 IMAPS, 12587 Submission, 18080 Verwaltung | – | Verwaltung: Nutzer `admin`, Passwort steht bei jedem Start im Log (`docker compose -f deploy/testmail/docker-compose.yml logs stalwart`) |
| GreenMail (`greenmail/standalone`) | schnell, ohne erweiterte IMAP-Fähigkeiten (Fallback-Pfad `uid-scan`) | 13143 IMAP, 13993 IMAPS, 13025 SMTP, 18081 API | – | Nutzer `test`, Passwort `pass`, Adresse `test@example.org` |

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

`make check` und die CI überspringen diese Tests (ohne `POSTKAUZ_IT`). Ist die Variable gesetzt und ein Server nicht erreichbar, schlägt der Test fehl.

## Bekannte Einschränkungen

- **Stalwart** startet ohne Domain und Konto und lauscht im Container zunächst nur auf IPv6; `make testmail-setup` behebt beides (nach jedem `testmail-up` mit frischem Volume). Auf dem Klartext-Port 12143 erlaubt Stalwart kein LOGIN. Das Image ist auf 0.11.8 festgelegt, weil `stalwartlabs/stalwart:latest` (0.16) im Bootstrap-Modus startet und die Ersteinrichtung über eine Web-Oberfläche im Internet verlangt.
- **Befüllen** mit dem synthetischen Korpus (IMAP `APPEND`) folgt mit dem Sync (S1.4); den Korpus erzeugt `make corpus`.
- Image-Versionen von Dovecot und GreenMail sind nicht festgenagelt (`latest`).
