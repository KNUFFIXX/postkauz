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

## Stand und bekannte Einschränkungen (Phase 0)

- **Geprüft** (14.10.2026): Compose-Datei ist gültig; Dovecot und GreenMail starten und akzeptieren IMAP-Logins mit den obigen Zugangsdaten.
- **Stalwart** startet, hat aber nach dem ersten Start **keine Domain und kein Konto**; IMAP-Verbindungen werden bis dahin abgewiesen. Die automatische Bereitstellung (Domain, Konto, Passwort per Verwaltungs-API) ist Aufgabe des Spikes (`auftraege/A01_phase0.md`). Das Image ist auf die Reihe 0.11 festgelegt, weil `stalwartlabs/stalwart:latest` (0.16) im Bootstrap-Modus startet und die Ersteinrichtung über eine Web-Oberfläche im Internet verlangt.
- **Befüllen** mit dem synthetischen Korpus (IMAP `APPEND`) kommt mit dem Generator in Schnitt 1 (`docs/testkorpus.md`).
- Image-Versionen sind bewusst nicht für alle Server festgenagelt (`latest`); sobald der Spike läuft, werden sie auf geprüfte Versionen festgelegt.
