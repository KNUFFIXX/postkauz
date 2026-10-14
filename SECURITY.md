# Sicherheit

## Probleme melden

Bitte melde Sicherheitslücken **nicht** als öffentliches Issue.

> Meldeweg: **Platzhalter.** Sobald das öffentliche Repository besteht, steht hier die Adresse für vertrauliche Meldungen (z. B. GitHub „Report a vulnerability" bzw. eine Kontaktadresse). Bis dahin gilt: Das Projekt ist pre-alpha, hat keine Releases und sollte nicht produktiv eingesetzt werden.

Hilfreich in einer Meldung: betroffene Version bzw. Commit, Schritte zur Reproduktion, erwartete und tatsächliche Wirkung. **Keine echten Mails, Zugangsdaten oder personenbezogenen Daten anhängen**, nur synthetische Beispiele.

## Grundsätze

- **Standardmäßig verschlossen:** Agenten arbeiten mit Schlüsseln, die nur die nötigen Scopes tragen; verweigert wird im Zweifel. Im MVP gibt es keinen Versandweg.
- **Mailinhalt ist feindliche Eingabe.** Er wird als „untrusted" gekennzeichnet, HTML wird nie gerendert, nichts wird nachgeladen, Anhänge werden nur in Subprozessen mit Limits verarbeitet.
- **Geheimnisse** liegen im Schlüsselbund oder in der Umgebung, nie im Repository, in Logs oder im Audit.
- **Nachvollziehbarkeit:** Audit-Log append-only, mit täglichem Export samt Prüfsumme an einen zweiten Ort.
- **Lieferkette:** wenige Abhängigkeiten, `go.sum` im Repository, Dependabot-Hinweise werden zeitnah bearbeitet.

Das vollständige Modell, einschließlich der ehrlich benannten Restrisiken: [`docs/sicherheit.md`](docs/sicherheit.md).

## Unterstützte Versionen

Es gibt noch keine Releases. Sicherheitskorrekturen erscheinen auf dem Hauptzweig.
