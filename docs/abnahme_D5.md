# Abnahme D5: Entwürfe mit deinem eigenen Postfach

Stand: 23.10.2026 (S2.9, Release-Kandidat 0.2.0-rc1). Gehört zu `auftraege/A02_s2.md`, Teil D, Abschnitt D5. Ergebnis kommt in die Messtabelle von [versanderkennung.md](versanderkennung.md), Abschnitt 9.

**Worum es geht:** Bisher lief alles nur gegen Testserver. D5 prüft mit **deinem** Postfach und **deinen** Mailprogrammen zwei Dinge (Akzeptanzkriterien 1 und 3 von Schnitt 2):

1. Ein Entwurf von PostKauz erscheint in deinem Mailprogramm im Ordner „Entwürfe" und hängt als Antwort im richtigen Gesprächsverlauf.
2. Nachdem du ihn **im Mailprogramm** gesendet hast, merkt PostKauz das beim nächsten Abgleich (`sent_by_human`).

Wir machen das dreimal: (1) Gmail im Browser, (2) Thunderbird, (3) ein zweites Postfach bei einem anderen Anbieter im Browser. Dauer: etwa 60 bis 90 Minuten, davon brauchst du selbst etwa 30.

**Was PostKauz dabei tut:** Es liest dein Postfach und legt je Runde **genau einen** Entwurf in den Ordner „Entwürfe". Es sendet nie, löscht nie, verschiebt nie. Gesendet wird nur, wenn **du** im Mailprogramm auf „Senden" klickst.

## So ist die Arbeit verteilt

Jeder Schritt ist markiert (Entscheid 35):

- **[Claude]** macht Claude per Skript auf deinem Rechner: Ordner und Dateien anlegen, PostKauz-Kommandos ausführen, Ausgaben mit „Erwartet" vergleichen, die Tabelle ausfüllen. Du musst dafür nichts tippen. Du bekommst eine Kurzfassung.
- **[Du]** machst du, weil es dein Postfach, dein Passwort, dein Mailprogramm oder dein Urteil braucht.

Dein **App-Passwort tippst nur du** ein. Claude sieht es nie, und es steht nie in einer Datei, einem Protokoll oder im Audit.

**Ausnahme von zwei Regeln, nur für D5:** Der Auftrag A02 sagt „Echte Konten prüft nur der Betreiber", und `CLAUDE.md` sagt „Niemals gegen echte Postfächer entwickeln oder testen". In D5 führt Claude trotzdem Kommandos gegen dein echtes Postfach aus, aber eng begrenzt:

- **Nur lesende Kommandos:** `account test`, `sync`, `search`, `show`, `draft show`, `draft list`, `audit` (dazu `version` und `doctor`, die dein Postfach gar nicht berühren).
- **Genau ein `draft reply` je Runde.** Der nächste `sync` legt diesen einen Entwurf in „Entwürfe" ab; das ist das Einzige, was in D5 auf den Server geschrieben wird.
- **Nur mit deiner ausdrücklichen Zustimmung**, die Claude vor Schritt 3.3 einholt (Abschnitt 3.2) und als Zeile ins Protokoll schreibt, z. B. `Zustimmung des Betreibers zu D5 gegen echte Postfächer: ja, 23.10.2026 14:05`. Ohne diese Zeile startet Claude kein Kommando gegen dein Postfach. Gilt für Postfach B genauso (Zustimmung vor 6.2).
- Alles andere (`draft edit`, `draft request`, `approve`, `draft mark-sent`, `account remove` …) führt Claude in D5 **nicht** aus; wo ein solcher Schritt nötig ist, machst du ihn (Abschnitt 7).

**Gut zu wissen:** `sync` spiegelt den **ganzen Posteingang** samt den Mails Dritter (dazu Entwürfe und Gesendet) nach `C:\PostKauz\d5\daten`. Das bleibt auf deinem Rechner, nichts davon geht ins Repository oder ins Protokoll. Der Abbau (Abschnitt 9.1, „Entfernen") löscht den Ordner wieder.

Wenn du einen Schritt selbst machen willst statt Claude: Die Kommandos stehen vollständig da. Jedes Kommando beginnt mit `.\postkauz.exe` und läuft in einem PowerShell-Fenster, das wie in Schritt 3.1 vorbereitet ist.

## 0. Was du brauchst

| Was | Wofür |
|---|---|
| Postfach A, z. B. dein Gmail-Konto | Runden 1 und 2 |
| Thunderbird mit Postfach A, schon eingerichtet (IMAP) | Runde 2 |
| Postfach B bei einem **anderen** Anbieter, z. B. GMX, web.de, IONOS oder Posteo, mit Webmail im Browser | Runde 3. Outlook.com und Hotmail erlauben IMAP mit Passwort meist nicht mehr (dort nur OAuth, das PostKauz noch nicht kann); nimm lieber einen anderen Anbieter |
| für jedes Postfach ein **App-Passwort** | Abschnitt 1 |
| den Release-Kandidaten 0.2.0-rc1 (`postkauz-windows-amd64.exe` und `SHA256SUMS` aus `dist/`) | bringt Claude mit |

## 1. App-Passwort anlegen [Du]

Ein App-Passwort ist ein eigenes Passwort nur für ein Programm. Dein normales Passwort bleibt geheim, und du kannst das App-Passwort jederzeit wieder löschen.

**Gmail:**

1. Ein App-Passwort gibt es bei Google **nur mit Bestätigung in zwei Schritten** (2FA). Ist sie aus: im Browser `myaccount.google.com` → „Sicherheit" → „Bestätigung in zwei Schritten" → einschalten.
2. Im Browser `myaccount.google.com/apppasswords` öffnen (eventuell neu anmelden).
3. Als Name `PostKauz D5` eintippen → „Erstellen".
4. Google zeigt 16 Buchstaben in vier Gruppen. **Dieses Fenster offen lassen**, bis Schritt 3.2 erledigt ist. Nicht abschreiben, nicht in eine Datei kopieren.

IMAP ist bei Gmail heute immer eingeschaltet. Falls `account test` (Schritt 3.3) trotzdem `auth` meldet: in Gmail → Zahnrad → „Alle Einstellungen aufrufen" → „Weiterleitung und POP/IMAP" prüfen.

**Postfach B** (anderer Anbieter): In den Einstellungen des Anbieters zwei Dinge suchen: „POP3/IMAP-Abruf erlauben" (bei GMX und web.de ausdrücklich einschalten) und „App-Passwort" bzw. „anwendungsspezifisches Passwort" (meist nur mit 2FA). Den Servernamen für IMAP nennt die Hilfeseite des Anbieters (z. B. `imap.gmx.net`, `imap.web.de`, `imap.ionos.de`, `posteo.de`, jeweils Port 993). Erst in Runde 3 nötig.

## 2. Vorbereitung [Claude]

Claude legt den Ordner `C:\PostKauz\d5` an (getrennt von allem anderen; dein sonstiges PostKauz-Datenverzeichnis bleibt unberührt) und darin:

| Datei | Inhalt |
|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` aus dem Release-Kandidaten, umbenannt; Prüfsumme gegen `SHA256SUMS` verglichen, `Unblock-File` |
| `config.toml` | siehe unten |
| `runde1.md`, `runde2.md`, `runde3.md` | der Text der drei Entwürfe |

`config.toml`:

```toml
data_dir = 'C:\PostKauz\d5\daten'

[draft]
quote_line = 'Am {date_numeric} schrieb {from}:'
```

Die Zeile `quote_line` ist die **empfohlene Einstellung** für Deutsch: Ohne sie beginnt das Zitat in der Antwort englisch (`On <Wochentag>, <Tag> <Monat> <Jahr> <Uhrzeit> <Zeitzone>, … wrote:`, z. B. `On Fri, 23 Oct 2026 10:15 +0200, … wrote:`), mit ihr deutsch (`Am <TT.MM.JJJJ> <Uhrzeit> schrieb …:`, z. B. `Am 23.10.2026 10:15 schrieb …:`). Datum und Uhrzeit sind die der Mail aus 4.1. Du kannst sie später in deine normale `config.toml` übernehmen.

`runde1.md` (Runde 2 und 3 entsprechend mit „Runde 2 (Thunderbird)" bzw. „Runde 3 (zweites Postfach)"):

```markdown
Hallo,

das ist Runde 1 der Abnahme D5 (Webmail). Diesen Entwurf hat PostKauz angelegt. Bitte unverändert senden.

Viele Grüße
```

Danach prüft Claude:

```powershell
cd C:\PostKauz\d5
$env:POSTKAUZ_CONFIG = "C:\PostKauz\d5\config.toml"
.\postkauz.exe version
.\postkauz.exe doctor
```

**Protokoll:** Claude schreibt jeden [Claude]-Schritt mit Kommando und Ausgabe in `docs/protokolle/D5_windows.txt` (wie bei D4). Dabei gilt:

- **Maskieren:** Echte Adressen, Domains und Message-IDs stehen nie im Protokoll, auch nicht in Ausgaben, die Claude hineinkopiert. Claude ersetzt sie einheitlich: Adressen durch `<adresse-A>` bzw. `<adresse-B>` (Postfach A bzw. B), Domains durch `<domain-A>` bzw. `<domain-B>`, Message-IDs durch `<msgid-1>`, `<msgid-2>` … (dieselbe ID bekommt immer denselben Platzhalter, damit „gleich/ungleich" lesbar bleibt). Der Anbieter von Postfach B steht nur als Name (z. B. „GMX"), nie als Servername mit deiner Adresse.
- **`show --headers` nur auswerten:** Die vollständigen Kopfzeilen (Schritt 4.4) liest Claude nur, um „Message-ID gleich?" und die Ordner abzulesen; sie kommen **nie** ins Protokoll, auch nicht maskiert. Ins Protokoll kommt nur das Ergebnis (z. B. `Message-ID gleich: ja`).
- **Vor dem Commit** des Protokolls läuft `make pii-check` (findet Adressen außerhalb von `example.com|org|net`); ein Treffer heißt: nachmaskieren, nicht freigeben.

Erwartet: `version` nennt `0.2.0-rc1`; `doctor` zeigt `config: C:\PostKauz\d5\config.toml (exists: true)`, `data dir: C:\PostKauz\d5\daten`, `keyring: available (Windows Credential Manager)`, `policy: … (not found, built-in defaults; …)` und `attach: C:\PostKauz\d5\daten\attach`; `database: missing` ist vor dem ersten Kommando richtig.

## 3. Postfach A anmelden

### 3.1 PowerShell-Fenster vorbereiten [Du]

Startmenü → „PowerShell" → öffnen. Diese zwei Zeilen hineinkopieren und Enter drücken:

```powershell
cd C:\PostKauz\d5
$env:POSTKAUZ_CONFIG = "C:\PostKauz\d5\config.toml"
```

Das Fenster offen lassen; Schritt 6.1 braucht es noch einmal. (Schließt du es, die zwei Zeilen in einem neuen Fenster wiederholen.)

### 3.2 Konto anlegen [Du]

Im selben Fenster, `<deine Gmail-Adresse>` durch deine Adresse ersetzen:

```powershell
.\postkauz.exe account add gmail --imap imap.gmail.com:993 --user <deine Gmail-Adresse> --auth app_password
```

PostKauz fragt `Password for … at imap.gmail.com:`. Jetzt die 16 Buchstaben aus dem Google-Fenster eintippen **ohne Leerzeichen** (beim Tippen erscheint nichts, das ist richtig) → Enter.

Erwartet: `account gmail added (imap.gmail.com:993, tls tls, user …); password stored in the OS keyring as account/…/gmail`.

Danach das Google-Fenster mit dem App-Passwort schließen. Sag Claude Bescheid („Konto angelegt").

**Zustimmung [Du]:** Bevor Claude in 3.3 das erste Kommando gegen dein Postfach startet, fragt es dich, ob es die Kommandos aus „So ist die Arbeit verteilt" (Ausnahme für D5) ausführen darf. Antworte „ja" oder „nein". Claude schreibt die Antwort mit Datum und Uhrzeit als Zeile ins Protokoll; bei „nein" führst du die [Claude]-Schritte selbst aus (die Kommandos stehen vollständig da).

### 3.3 Konto prüfen [Claude]

```powershell
.\postkauz.exe account test gmail
```

Erwartet: Anmeldung gelingt, Liste der Ordner mit Rollen, darunter `inbox`, `drafts` und `sent`. Bei Gmail auf Deutsch meist `[Gmail]/Entwürfe` (Rolle `drafts`) und `[Gmail]/Gesendet` (Rolle `sent`). Claude merkt sich die beiden Ordnernamen für den kurzen Sync. Am Server ändert sich nichts.

Abweichung `auth`: App-Passwort falsch abgetippt oder IMAP aus → Abschnitt 7.

### 3.4 Kurzer Sync [Claude]

Gespiegelt werden nur drei Ordner: Posteingang, Entwürfe, Gesendet. „Alle Nachrichten" (`[Gmail]/Alle Nachrichten`) bleibt bewusst weg, das wäre bei Gmail das ganze Postfach.

```powershell
.\postkauz.exe sync -a gmail --mailbox INBOX --mailbox "[Gmail]/Entwürfe" --mailbox "[Gmail]/Gesendet"
```

(Ordnernamen so, wie `account test` sie zeigte.) Erwartet: je Ordner eine Zeile, am Ende `N messages mirrored` und `0 errors`. Ein großer Posteingang (mehrere zehntausend Mails) braucht beim ersten Mal länger; der Lauf lässt sich mit Strg+C abbrechen und mit demselben Befehl fortsetzen. Weitere `sync`-Aufrufe in dieser Anleitung meinen **genau diesen Befehl** (mit denselben `--mailbox`-Angaben).

## 4. Runde 1: Gmail im Browser

### 4.1 Mail an dich selbst [Du]

In Gmail im Browser eine neue Mail **an deine eigene Adresse**: Betreff `PostKauz D5 Runde 1`, Text `Test`. Senden.

### 4.2 Entwurf anlegen und ablegen [Claude]

```powershell
.\postkauz.exe sync -a gmail --mailbox INBOX --mailbox "[Gmail]/Entwürfe" --mailbox "[Gmail]/Gesendet"
.\postkauz.exe search 'subject:"PostKauz D5 Runde 1"' -a gmail
.\postkauz.exe draft reply <ID aus der Suche> --body-file runde1.md
.\postkauz.exe sync -a gmail --mailbox INBOX --mailbox "[Gmail]/Entwürfe" --mailbox "[Gmail]/Gesendet"
.\postkauz.exe draft show <Entwurfs-ID>
```

Erwartet:

- `draft reply`: `draft 1 created (status draft, remote pending)`, Betreff `Re: PostKauz D5 Runde 1`, An: deine Adresse, Hinweis `nothing is sent: a human sends the draft in the mail client`.
- zweiter `sync`: Zeile `drafts: 1 put into <Pfad des Entwürfe-Ordners> (flags \Draft \Seen; nothing is sent)`, bei Gmail auf Deutsch zum Beispiel `drafts: 1 put into [Gmail]/Entwürfe (flags \Draft \Seen; nothing is sent)` (nur ein Beispiel: der Pfad hängt von Anbieter und Sprache ab, er ist der Ordner mit Rolle `drafts` aus 3.3).
- `draft show 1`: `remote: pushed`, eine Zeile `copies: uid …`, das Zitat beginnt mit `Am … schrieb …:`.

Claude notiert die Message-ID aus `draft show` (Form `<pk.…@…>`) für den Vergleich in 4.4; im Protokoll steht sie nur maskiert (`<msgid-1>`, Abschnitt 2).

### 4.3 Im Mailprogramm senden [Du]

1. Gmail im Browser neu laden (F5). Links „Entwürfe" anklicken.
2. Den Entwurf `Re: PostKauz D5 Runde 1` öffnen.
3. Kurz ansehen (dein Urteil): Empfänger ist deine Adresse, Betreff stimmt, Text von `runde1.md`, darunter das Zitat. **Nichts ändern.**
4. „Senden" klicken.
5. Eine Minute warten. Im Posteingang nachsehen: Steht die Antwort im **selben Gesprächsverlauf** wie die Mail aus 4.1? Notiere ja oder nein und sag es Claude.

### 4.4 Ergebnis ablesen [Claude]

```powershell
.\postkauz.exe sync -a gmail --mailbox INBOX --mailbox "[Gmail]/Entwürfe" --mailbox "[Gmail]/Gesendet"
.\postkauz.exe draft list -a gmail
.\postkauz.exe draft show 1 --json
.\postkauz.exe show <sent_message_id aus draft show --json> --headers
.\postkauz.exe show "<Message-ID des Entwurfs>"
.\postkauz.exe audit --since 1h --operation drafts.sent_detected
```

Erwartet (beides ist bestanden):

- `sync`: `drafts: 1 sent in the mail client, now sent_by_human (1 by Message-ID; draft 1)` → Gmail hat die Message-ID behalten; **oder** `(1 by recipients, subject and text; draft 1)` → Gmail hat eine neue Message-ID vergeben, der Rückfall-Abgleich hat es erkannt. Für Gmail im Browser erwarten wir eher den Rückfall, aber **wir messen, nicht annehmen**.
- `draft list`: Entwurf 1 mit Status `sent_by_human`.

Claude liest daraus die Zeile für die Tabelle ab (Abschnitt 8; die Ausgabe von `show --headers` nur auswerten, nie ins Protokoll, Abschnitt 2): Message-ID gleich? (Kopfzeile `Message-ID` der gesendeten Mail gegen die des Entwurfs), Entwurf im Ordner Entwürfe verschwunden? (`show "<Message-ID des Entwurfs>"` nennt die Ordner bzw. „deleted on server"), Kopie in Gesendet? (Ordner der gesendeten Mail), Grund im Audit (`message_id` oder `fallback`).

**Nicht bestanden** ist: Der Entwurf bleibt offen (`draft`, `REMOTE missing`). Dann Abschnitt 7.

## 5. Runde 2: Thunderbird

### 5.1 Mail an dich selbst [Du]

Wie 4.1, Betreff `PostKauz D5 Runde 2`. Senden (aus Gmail oder aus Thunderbird, egal).

### 5.2 Entwurf anlegen und ablegen [Claude]

Wie 4.2 mit `PostKauz D5 Runde 2` und `runde2.md`. Erwartet wie dort (der Entwurf hat die nächste Nummer, z. B. `draft 2`).

### 5.3 Im Mailprogramm senden [Du]

1. Thunderbird öffnen. Links beim Gmail-Konto den Ordner „Entwürfe" anklicken (Thunderbird holt ihn dann frisch). Erscheint der Entwurf nicht: in der Symbolleiste oben auf „Abrufen" klicken **[Bezeichnung unsicher]**, oder kurz einen anderen Ordner (z. B. Posteingang) und dann wieder „Entwürfe" anklicken; notfalls eine Minute warten.
2. Den Entwurf `Re: PostKauz D5 Runde 2` **doppelt anklicken**. Es öffnet sich das Fenster zum Verfassen.
3. Ansehen wie in 4.3, **nichts ändern**, „Senden".
4. Eine Minute warten. Im Posteingang (Thunderbird oder Gmail im Browser): Steht die Antwort im selben Gesprächsverlauf wie die Mail aus 5.1? Notiere ja oder nein, sag es Claude. Notiere auch die **Version** von Thunderbird: Ab Version 115 ist die Menüleiste meist ausgeblendet; dann einmal die Taste **Alt** drücken (die Menüleiste erscheint) oder rechts oben auf **≡** klicken, jeweils → „Hilfe" → „Über Thunderbird".

### 5.4 Ergebnis ablesen [Claude]

Wie 4.4 mit der neuen Entwurfs-ID. Erwartet: `sent_by_human`, von Thunderbird eher über die Message-ID (`1 by Message-ID`); der Rückfall ist ebenfalls bestanden.

Achtung, bekannt (Entscheid 44): Ein Mailprogramm, das den **unveränderten** Entwurf einfach nach „Gesendet" verschiebt, statt ihn neu zu senden, erkennt PostKauz nicht als Versand. Dann bleibt der Entwurf offen; das ist eine Messung, kein Fehler von dir (Abschnitt 7).

## 6. Runde 3: zweites Postfach, anderer Anbieter

### 6.1 Konto anlegen [Du]

Im PowerShell-Fenster aus 3.1 (Servername und Adresse von Postfach B einsetzen; als Kontoname z. B. `zweit`):

```powershell
.\postkauz.exe account add zweit --imap <IMAP-Server von Postfach B>:993 --user <Adresse von Postfach B> --auth app_password
```

App-Passwort von Postfach B eintippen (ohne Leerzeichen) → Enter. Erwartet: `account zweit added (…); password stored in the OS keyring as account/…/zweit`. Claude Bescheid sagen. Vor 6.2 holt Claude deine Zustimmung für Postfach B ein und schreibt sie ins Protokoll (wie in 3.2).

### 6.2 Prüfen und kurz spiegeln [Claude]

`account test zweit` (Ordner mit Rollen `inbox`, `drafts`, `sent`; die Namen heißen je Anbieter anders, z. B. `Entwürfe` und `Gesendet`), dann der kurze Sync wie 3.4 mit `-a zweit` und den Ordnernamen von Postfach B.

Meldet der Sync `error: no_drafts_mailbox`, erkennt PostKauz den Entwürfe-Ordner dieses Anbieters nicht; Claude trägt dann in `config.toml` unter `[draft]` die Zeile `mailbox = '<Name des Entwürfe-Ordners>'` ein und wiederholt den Sync. **Achtung:** `[draft] mailbox` gilt für **alle** Konten in dieser `config.toml`, also auch für Gmail. Deshalb entfernt Claude die Zeile direkt nach Runde 3 (nach 6.6) wieder; ein `sync -a gmail` mit der Zeile würde Entwürfe in einen Ordner legen wollen, den es bei Gmail so nicht gibt.

### 6.3 Mail an dich selbst [Du]

In der Webmail von Postfach B eine Mail an die **eigene Adresse von Postfach B**: Betreff `PostKauz D5 Runde 3`, Text `Test`. Senden.

### 6.4 Entwurf anlegen und ablegen [Claude]

Wie 4.2, mit `-a zweit` beim Sync, `PostKauz D5 Runde 3` und `runde3.md`.

### 6.5 Im Mailprogramm senden [Du]

In der Webmail von Postfach B: Ordner „Entwürfe" öffnen (Seite neu laden), Entwurf `Re: PostKauz D5 Runde 3` öffnen, ansehen, **nichts ändern**, „Senden". Eine Minute warten; steht die Antwort im selben Gesprächsverlauf (falls die Webmail Gesprächsverläufe zeigt; sonst in Thunderbird, wenn Postfach B dort eingerichtet ist)? Notiere ja, nein oder „nicht sichtbar" und den **Anbieter**.

### 6.6 Ergebnis ablesen [Claude]

Wie 4.4 mit `-a zweit` und der Entwurfs-ID aus 6.4. Erwartet: `sent_by_human`, Grund `message_id` oder `fallback`.

Hat Claude in 6.2 die Zeile `mailbox = '…'` unter `[draft]` eingetragen, entfernt es sie jetzt wieder aus `config.toml` (sie gilt für alle Konten, auch Gmail) und prüft mit `doctor`, dass die Konfiguration gültig ist.

## 7. Wenn etwas anders ist

| Was du siehst | Was es heißt | Was zu tun ist |
|---|---|---|
| `account test` meldet `auth` | App-Passwort falsch abgetippt, Leerzeichen mitgetippt, oder IMAP beim Anbieter aus | [Du] neues App-Passwort anlegen (Abschnitt 1), dann `.\postkauz.exe account set gmail --password-stdin` im Fenster aus 3.1 und neu eintippen; [Claude] `account test` wiederholen |
| `account test` meldet `connection` (Exit 5) | Servername oder Port falsch, oder eine Firewall blockt Port 993 | Servernamen auf der Hilfeseite des Anbieters nachsehen |
| Der Entwurf erscheint nicht in „Entwürfe" | Seite nicht neu geladen, oder `sync` meldete einen Fehler | [Du] Seite neu laden; [Claude] `draft show <ID>` lesen (`remote: pending`, `push: …` nennt den Grund) |
| Nach dem Senden bleibt der Entwurf offen, `draft list` zeigt `REMOTE missing` | Das Mailprogramm hat eine neue Message-ID vergeben **und** Betreff, Empfänger oder Text so verändert (z. B. eigene Signatur angehängt, Zitat umgebaut), dass der Rückfall-Abgleich nicht greift; oder es hat den Entwurf nach „Gesendet" verschoben | [Claude] die gesendete Mail mit dem Entwurf vergleichen (Betreff, Empfänger, Text) und dir den Unterschied nennen; [Du] den Entwurf selbst schließen: `.\postkauz.exe draft mark-sent <ID>` (das darf nur der Mensch). Ergebnis in der Tabelle: „offen, `remote: missing`", mit Grund als Bemerkung |
| `draft show` meldet `remote: taken_over` | Das Mailprogramm hat beim Öffnen eine veränderte Fassung gespeichert (Autospeichern) | Kein Fehler: PostKauz überschreibt deine Fassung nie. Trotzdem senden; Bemerkung in die Tabelle |
| `warning: … older copies in Drafts` | Es liegen zwei Kopien desselben Entwurfs in „Entwürfe" (nur nach einer Änderung) | Kommt in D5 nicht vor, weil nichts geändert wird; falls doch: Bemerkung, die ältere Kopie im Mailprogramm löschen |
| `sync_running` | Ein anderer `sync` desselben Kontos läuft noch | warten, dann wiederholen |

Weicht etwas ab, das hier nicht steht: Claude schreibt es mit Kommando und Ausgabe auf, du entscheidest (Abschnitt 9).

## 8. Ergebnis eintragen [Claude, du bestätigst]

Claude trägt die drei Zeilen in die Tabelle in [versanderkennung.md](versanderkennung.md), Abschnitt 9, ein:

| Spalte | Woher |
|---|---|
| Mailprogramm (Version) | Runde 1: „Gmail im Browser" und dein Browser; Runde 2: Thunderbird mit der Version aus 5.3; Runde 3: „Webmail" des Anbieters |
| Konto/Anbieter | Gmail bzw. der Anbieter von Postfach B (nur der Name des Anbieters, nie deine Adresse) |
| Message-ID erhalten? | Kopfzeile `Message-ID` der gesendeten Mail gleich der des Entwurfs: ja/nein |
| Entwurf gelöscht? | Kopie nach dem Senden nicht mehr in „Entwürfe": ja/nein |
| Kopie in Gesendet? | gesendete Mail liegt im Ordner mit Rolle `sent`: ja/nein |
| Ergebnis | `sent_by_human (message_id)`, `sent_by_human (fallback)` oder `offen, remote: missing` |
| Datum, Bemerkung | Datum; Gesprächsverlauf beim Empfänger richtig (ja/nein, von dir); Auffälligkeiten |

Du liest die Zeilen und sagst „passt" oder was zu ändern ist. Vor dem Commit von Tabelle und Protokoll (`docs/protokolle/D5_windows.txt`) prüft Claude, dass darin keine echte Adresse, Domain oder Message-ID steht (Abschnitt 2), und lässt `make pii-check` laufen; erst wenn es grün ist, wird committet.

## 9. Abschluss

### 9.1 Konto behalten oder entfernen [Du entscheidest]

**Behalten:** Der Ordner `C:\PostKauz\d5` und die beiden Einträge im Windows-Anmeldeinformationsspeicher bleiben. Die drei Entwürfe sind abgeschlossen. Für den Alltag später lieber ein eigenes Datenverzeichnis außerhalb von `d5` einrichten (eigene Entscheidung, nicht Teil von D5).

**Entfernen:** `account remove` geht hier **nicht**: Es verweigert Konten mit gespiegelten Mails, weil PostKauz das Archiv nie nebenbei löscht (ADR-003; Meldung `account gmail has N archived messages`). Entfernt wird deshalb so:

1. [Du] Die App-Passwörter beim Anbieter löschen (Gmail: `myaccount.google.com/apppasswords` → `PostKauz D5` → Papierkorb-Symbol; Postfach B entsprechend). Das ist der wichtigste Schritt: Danach kann niemand mehr mit diesen Passwörtern in dein Postfach.
2. [Claude, mit deinem OK] Die beiden Einträge aus dem Windows-Anmeldeinformationsspeicher löschen. `cmdkey /list` zeigt sie als `LegacyGeneric:target=postkauz:account/<id>/gmail` bzw. `…/zweit`; `<id>` ist die Kennung aus der Ausgabe von `account add` (`password stored in the OS keyring as account/<id>/gmail`). Gelöscht wird mit dem Teil hinter `target=`: `cmdkey /delete:postkauz:account/<id>/gmail` und `cmdkey /delete:postkauz:account/<id>/zweit`. Von Hand: Systemsteuerung → „Anmeldeinformationsverwaltung" → „Windows-Anmeldeinformationen" → „Generische Anmeldeinformationen".
3. [Claude, mit deinem OK] Den Ordner `C:\PostKauz\d5` löschen (darin liegen die gespiegelten Mails).

Die Entwürfe und gesendeten Mails in deinem Postfach bleiben, wie sie sind; PostKauz rührt sie nicht an. Die drei Test-Mails löschst du bei Bedarf selbst im Mailprogramm.

### 9.2 Entscheidung D6 [Du]

Mit dem Ergebnis von D4 (bestanden, 22.10.2026) und D5 entscheidest du (A02, Teil D, D6):

1. **S2 angenommen** → Tag `v0.2.0-rc1`, Release auf GitHub aus `dist/`, danach Schnitt 3.
2. **Nachbesserung** mit Liste (Punkt, Kommando, Ausgabe).
3. **S3 starten** und die offenen Punkte parallel nachbessern.

Als bestanden gilt D5, wenn in jeder Runde der Entwurf in „Entwürfe" erschien, nach dem Senden `sent_by_human` wurde (über Message-ID oder Rückfall) und die Antwort im richtigen Gesprächsverlauf steht. Eine Runde mit „offen, `remote: missing`" ist eine Messung, kein Durchfallen von selbst; Claude schlägt dann vor, ob die Erkennung nachgebessert werden muss.
