# Versanderkennung: Wie PostKauz merkt, dass ein Entwurf gesendet wurde

Stand: S2.6 (A02 E5, E6, E7; Entscheide 26, 38, 43). PostKauz sendet nie (ADR-002). Ein Entwurf liegt als Kopie im Entwürfe-Ordner; der Mensch öffnet ihn in seinem Mailprogramm und drückt dort auf „Senden". Dieses Dokument beschreibt, woran PostKauz das danach erkennt, was es nicht erkennen kann und wie sich die Mailprogramme verhalten (Messtabelle unten, gefüllt vom Betreiber in Abnahme D5).

## 1. Wann geprüft wird

Nach **jedem** Sync-Lauf, also bei `postkauz sync` und in jeder Runde von `postkauz sync --watch` (alle 5 Minuten, bei jeder Meldung der INBOX per IDLE, stündlich mit vollem Abgleich), unter der Kontosperre und **nur gegen den lokalen Spiegel**: Der Server wird dafür nicht eigens gefragt. Gesehen wird also, was der Lauf gespiegelt hat. Eine IDLE-Runde spiegelt nur die INBOX; Gesendet sieht spätestens die nächste Runde über alle Ordner (höchstens 5 Minuten).

Die Prüfung macht drei Dinge, in dieser Reihenfolge (`drafts.Service.CheckAfterSync`; die Versanderkennung kommt seit der Gegenprüfung S2.6–S2.8, W1, **vor** dem Ablauf: Hat der Mensch einen freigegebenen Entwurf vor Ablauf der Freigabe gesendet und sieht der Sync die Nachricht erst danach, ist der Entwurf gesendet, nicht abgelaufen):

1. **Versanderkennung** (E5, Abschnitt 2 und 3): offene Entwürfe (`draft`, `pending_approval`, `ready`) werden `sent_by_human`.
2. **Ablauf der Freigabe** (E7, Entscheid 38): Ein Entwurf in `ready` oder `pending_approval`, dessen Freigabe älter als `[draft] approval_ttl` ist (Standard 168 h, ab Vorlage bzw. ab `approve`), fällt auf `draft` zurück; die Freigabe wird `expired`; Audit `drafts.expire`, Grund `approval_expired`, Akteur `system`. Dasselbe geschah schon seit S2.1 beim Lesen (`draft show`, `draft list`, MCP); seit S2.6 auch beim Sync, ohne dass jemand liest.
3. **Stand der Serverkopie** (E6): Ist die zuletzt abgelegte Kopie eines noch offenen Entwurfs aus dem Entwürfe-Ordner verschwunden, wird `remote_state` `missing`; hat ein Mailprogramm eine bearbeitete Fassung gespeichert, `taken_over` (Audit `drafts.remote_check`). Vorher stellte das nur `drafts.update` bzw. der nächste Push fest; jetzt zeigt `draft list` es nach dem nächsten Sync.

Scheitert die Prüfung (etwa weil die Datenbank gerade gesperrt ist), zählt das als Fehler des Laufs (`errors`, Log ohne Mailinhalt); der Rest des Laufs (Nacharbeit, Extraktion, Suchindex) läuft weiter, und der nächste Lauf prüft erneut (Gegenprüfung S2.6–S2.8, K5). Nur Abbruch, Verbindungs- oder Anmeldefehler und ein interner Fehler (Panic) beenden den Lauf.

Der Sync nennt das Ergebnis in einer Zeile vor den Schlusszeilen, z. B. `drafts: 1 sent in the mail client, now sent_by_human (1 by Message-ID; draft 3)`, mit `--json` im Feld `draft_check` (`sent_by_message_id`, `sent_by_fallback`, `approval_expired`, `missing`, `taken_over`, `sent_draft_ids`). Ohne Änderung erscheint nichts.

## 2. Erster Weg: die Message-ID

Jeder Entwurf bekommt beim Anlegen eine feste Message-ID (`pk.<20 Hex>@<Domain des Absenders>`, E11), die auch in der Kopie im Entwürfe-Ordner steht. Viele Mailprogramme behalten sie beim Senden. Liegt nach dem Sync in einem Ordner mit Rolle **`sent`** (Gesendet) oder **`all`** (Gmail „Alle Nachrichten") eine Nachricht mit dieser Message-ID, ist der Entwurf gesendet: `sent_by_human`, `sent_message_id` = lokale ID der gesendeten Nachricht, `closed_at` gesetzt, Audit `drafts.sent_detected` mit Grund **`message_id`**.

## 3. Zweiter Weg: der Rückfall-Abgleich

Manche Mailprogramme (erwartet: Gmail im Browser) bauen die Nachricht beim Senden neu und vergeben eine **neue** Message-ID. Für Entwürfe ohne Treffer über die Message-ID gilt deshalb der Rückfall; er trifft nur, wenn **alle** Bedingungen zugleich erfüllt sind:

| Bedingung | genau |
|---|---|
| Empfänger | Die Menge aus To und Cc ist gleich: nur die Adressen, klein geschrieben, ohne Namen, Reihenfolge und Dubletten egal. Bcc zählt nicht (steht nicht in der gesendeten Nachricht). |
| Betreff | gleich, nachdem vorn alle Präfixe `Re:`, `AW:`, `Antw:`, `Fwd:`, `WG:`, `FW:` (beliebig oft, Groß-/Kleinschreibung egal) entfernt und Leerraum zusammengefasst ist; unsichtbare Zeichen fallen weg. Der Rest wird genau verglichen (auch Groß-/Kleinschreibung). |
| Zeit | Versandzeit (Date-Kopfzeile, sonst Eingang im Ordner) zwischen `content_changed_at − 1 h` und `content_changed_at + 14 Tage` des Entwurfs, beide Grenzen eingeschlossen. `content_changed_at` ist die letzte Änderung des **Inhalts** (Anlegen, `drafts.update`/`draft edit`; Migration 0015). Vorlage, Freigabe, Ablehnung, Push und vor allem der Ablauf der Freigabe (beim Sync oder beim Lesen) verschieben das Fenster nicht (Gegenprüfung S2.6–S2.8, W1; vorher hing es an `updated_at`, und ein am Tag 6 gesendeter, erst am Tag 8 gespiegelter Entwurf fiel durch). |
| Text | Jaccard-Ähnlichkeit der **Wortmengen** ≥ 0,9. Links: der eigene Text des Entwurfs (`body_markdown` bzw. `body_html`, umgesetzt wie beim Nachrichtenbau, ohne Zitat, ohne Signatur, ohne Weiterleitungsteil). Rechts: der zitatfreie Text der gesendeten Nachricht (wie ihn der Spiegel für `snippet` und Suche bildet: ohne `>`-Zeilen, ohne Einleitung „On … wrote:" / „Am … schrieb …:", ohne Originalnachricht im Outlook-Stil), abgeschnitten vor einer Weiterleitung (`---------- Forwarded message ----------`, `-------- Weitergeleitete Nachricht --------`). Die Wörter der Signatur der Identität fallen auf **beiden** Seiten weg (sonst drückte eine lange Signatur einen unverändert gesendeten Entwurf unter 0,9). Wörter sind Folgen aus Buchstaben und Ziffern, klein geschrieben; Jaccard = gemeinsame Wörter / alle Wörter beider Seiten. |

Treffer → `sent_by_human` wie oben, Grund **`fallback`**.

Ein paar Folgen, die man kennen sollte:

- **Kleine Korrekturen beim Senden** (ein, zwei Wörter) fallen meist noch über 0,9: Bei zehn Wörtern ist ein zusätzliches Wort 10/11 ≈ 0,91, zwei zusätzliche 10/12 ≈ 0,83. Je kürzer der Text, desto eher kippt der Rückfall. Wer viel umschreibt, schließt den Entwurf selbst (Abschnitt 5).
- **Leerer Text:** Ein Entwurf ohne eigenen Text (oder nur mit Signatur) und ein Entwurf ohne Empfänger in To/Cc werden im Rückfall nie geschlossen. Zwei leere Texte gelten nicht als gleich.
- **Eine gesendete Nachricht schließt höchstens einen Entwurf.** Passen mehrere Entwürfe zu derselben Nachricht (gleiche Empfänger, Betreff und Text), gewinnt die höchste Ähnlichkeit, dann die älteste Nachricht, dann der älteste Entwurf. Eine Nachricht, die schon einem Entwurf zugeordnet ist (`sent_message_id`), zählt nie wieder.
- **Änderung während der Prüfung:** Der Rückfall vergleicht den Stand, den er zu Beginn gelesen hat. Ändert ein Agent oder der Mensch den Inhalt, bevor der Entwurf geschlossen ist, wird er nicht geschlossen (der Vergleich gilt nicht mehr); die nächste Prüfung vergleicht den neuen Stand (Gegenprüfung S2.6–S2.8, K4).
- **„Alle Nachrichten" (Rolle `all`):** Dort liegen auch empfangene Mails. Im Rückfall zählt eine Nachricht, die **nur** dort liegt, deshalb nur, wenn ihr Absender eine eigene Adresse ist: Kontoadresse, Anmeldename, Identitäten samt Reply-To und Aliasse (`account set --aliases`, Entscheid 26). Für Gesendet (Rolle `sent`) gilt diese Bedingung nicht. Für die Message-ID gilt sie nie (die ID ist eindeutig).

## 4. Was nie als gesendet zählt

- Nachrichten mit dem Flag **`\Draft`** in irgendeinem Ordner.
- Nachrichten, die (auch) in einem **Entwürfe-Ordner** liegen: Rolle `drafts` oder ein Ordner, in den PostKauz Entwürfe abgelegt hat (`[draft] mailbox`).
- Nachrichten, die nur in anderen Ordnern liegen (Posteingang, eigene Ordner, Papierkorb).

Grund (Befund K8 der Gegenprüfung von S2.4/S2.5): **Gmail führt jede Entwurfskopie zusätzlich in `[Gmail]/All Mail`**, mit der Message-ID des Entwurfs. Ohne diesen Ausschluss gälte jeder abgelegte Entwurf sofort als gesendet. Sendet der Mensch, entfernt Gmail die Kopie aus den Entwürfen; die gesendete Nachricht trägt kein `\Draft` und zählt dann.

## 5. Kein Treffer ist kein Fehler

Findet die Prüfung nichts, bleibt der Entwurf in seinem Zustand (`ready` bleibt `ready`). Ist seine Kopie aus dem Entwürfe-Ordner verschwunden, zeigt `draft list` `remote: missing` und `draft show` den Hinweis `the copy in the Drafts folder is gone; close the draft with draft mark-sent or draft abandon`. Änderungen sind dann `draft_taken_over`. Der Mensch schließt ihn selbst:

- `postkauz draft mark-sent <id>` → `sent_by_human` (er hat gesendet),
- `postkauz draft abandon <id>` → `abandoned` (er hat verworfen).

Beides darf **nur der Mensch** (ohne `--key`; mit jedem Schlüssel Exit 3 `human_only`, E2). Ein Agent kann einen Entwurf nie für gesendet erklären.

## 6. Audit

Jede Erkennung steht als Systemereignis im Audit, **ohne Mailinhalt**: Akteur `system`, Operation `drafts.sent_detected`, Grund `message_id` oder `fallback`, Parameter `draft_id`, `sent_message_id` (lokale ID), `approval_id` (falls vorhanden), `from`/`to` (Zustand vorher und nachher). Ablauf: `drafts.expire` mit Grund `approval_expired`; verschwundene oder bearbeitete Kopie: `drafts.remote_check` mit Grund `missing` bzw. `taken_over`. Betreff, Adressen und Text stehen nie im Audit oder Log.

## 7. Ablage geänderter Entwürfe (Entscheid 43)

Damit nicht jeder Zwischenstand eine weitere Kopie im Entwürfe-Ordner hinterlässt (PostKauz löscht keine alten Kopien), legt der Push einen **geänderten** Entwurf (er hat schon eine Kopie im Ordner) erst ab, wenn er **30 Sekunden unverändert** ist (gemessen an `updated_at`). Die **erste** Ablage eines neuen Entwurfs geschieht sofort (Festlegung S2.6: Entscheid 43 spricht nur von geänderten Entwürfen; ein neuer Entwurf hat noch keine Kopie, die sich vervielfachen könnte). `sync --watch` prüft alle 10 s und legt den Entwurf ab, sobald die 30 s um sind (also 30 bis 40 s nach der letzten Änderung). Ein einmaliges `postkauz sync` innerhalb der 30 s legt ihn nicht ab und sagt das: `drafts: … 1 changed less than 30 s ago, not put yet (a running sync --watch puts it once it has been unchanged for 30 s; otherwise the next sync)`, mit `--json` im Feld `drafts.waiting`.

## 8. Gmail und andere Anbieter (nur dokumentiert, gemessen in D5)

- **Testserver (Integrationstest `TestIntegrationSentDetection`, `make integration`):** gegen Dovecot, Stalwart und GreenMail geprüft, je mit einem zweiten IMAP-Client als Mailprogramm, das in Gesendet ablegt und die Kopie aus den Entwürfen entfernt: (A) Kopie mit derselben Message-ID → `sent_by_human` (`message_id`); (B) neue Message-ID, gleicher Text, Betreff mit `Re:` → `sent_by_human` (`fallback`); (C) neue Message-ID, stark geänderter Text → bleibt `ready`, `remote: missing`; `ready` älter als `approval_ttl` → `draft`; `draft mark-sent`/`draft abandon` mit Schlüssel → `human_only`. Dovecot und Stalwart melden Gesendet per SPECIAL-USE, GreenMail hat keinen Ordner Gesendet (der Test legt `Sent` an, Rolle per Name).
- Entwürfe-Ordner: `[Gmail]/Drafts` bzw. lokalisiert (`[Gmail]/Entwürfe`), Rolle `drafts` aus SPECIAL-USE `\Drafts`.
- Gesendet: `[Gmail]/Sent Mail` (lokalisiert `[Gmail]/Gesendet`), Rolle `sent`; zugleich in `[Gmail]/All Mail` (Rolle `all`). Beide Rollen gelten für die Erkennung; dieselbe Nachricht in beiden Ordnern ist im Spiegel **eine** Nachricht (gleiche Message-ID und Größe) und schließt höchstens einen Entwurf.
- Erwartung (nicht gemessen): Gmail im Browser vergibt beim Senden eine neue Message-ID → Rückfall; Thunderbird behält sie wahrscheinlich → Message-ID. **Messen, nicht annehmen**: Tabelle unten.
- Server, die Nachrichten beim Ablegen umschreiben (Exchange, Archivlösungen), sind für die Versanderkennung unkritisch (sie vergleicht Message-ID bzw. Text, nicht Bytes), wohl aber für die Übernahme-Erkennung (docs/architektur.md, Abschnitt 3, Regel 5).
- Ein Mailprogramm, das beim Senden **keine** Kopie in Gesendet ablegt (manche Mobil-Apps, SMTP ohne „Kopie speichern"), lässt beide Wege leer: Der Entwurf bleibt offen, `remote: missing`, der Mensch schließt ihn.

## 9. Messtabelle (Abnahme D5, füllt der Betreiber)

Ablauf je Zeile (Anleitung in `docs/abnahme_D5.md`, S2.9): `draft reply` auf eine eigene Mail, `sync` (Ablage), im Mailprogramm den Entwurf öffnen und **unverändert** senden, danach `sync` und `draft list`. Spalten:

- **Message-ID erhalten?** Steht in der gesendeten Nachricht dieselbe Message-ID wie im Entwurf (`draft show`)? ja/nein
- **Entwurf gelöscht?** Ist die Kopie danach aus dem Entwürfe-Ordner verschwunden? ja/nein
- **Kopie in Gesendet?** Liegt die gesendete Nachricht im Ordner Gesendet (bzw. „Alle Nachrichten")? ja/nein
- **Ergebnis:** was `draft list` danach zeigt (`sent_by_human` mit `message_id` / `fallback`, oder offen mit `remote: missing`).

| Mailprogramm (Version) | Konto/Anbieter | Message-ID erhalten? | Entwurf gelöscht? | Kopie in Gesendet? | Ergebnis | Datum, Bemerkung |
|---|---|---|---|---|---|---|
| Webmail: | | | | | | |
| Desktop-Client: | | | | | | |
| zweites Postfach, anderer Anbieter: | | | | | | |
