# Abnahme D9: Microsoft-Postfach per OAuth2, nur lesend

Stand: 27.10.2026 (A03 Teil T4, S3.7, Build `0.3.0-ms.1`). Gehört zu `auftraege/A03_s3.md`, Teil D, Abschnitt D9. Skript: [abnahme_D9.ps1](abnahme_D9.ps1). App-Registrierung: [betrieb_microsoft.md](betrieb_microsoft.md).

**Worum es geht:** PostKauz meldet sich an einem Microsoft-Postfach (Outlook.com oder Microsoft 365) mit OAuth2 an, spiegelt den Posteingang und findet darin. Bisher lief das nur gegen einen nachgebauten Microsoft-Server im Test. D9 prüft es mit **deinem** Konto, **nur lesend**: kein Entwurf, keine Aktion, nichts wird im Postfach verändert.

**Dauer:** etwa 30 Minuten, davon brauchst du selbst etwa 15 (App registrieren, zweimal anmelden).

## Arbeitsteilung

- **[Claude]** (Prüfsitzung) kopiert Programm und Skript auf den Büro-PC, startet das Skript und wertet das Protokoll aus.
- **[Du]** registrierst die App, gibst die Zustimmung, meldest dich zweimal an (einmal im Browser, einmal mit Code) und entscheidest am Ende, ob das Konto bleibt.

## 1. Regeln für diesen Lauf

- **Eigenes Datenverzeichnis, nie das Live-Verzeichnis.** Das Skript verlangt `-DataDir` und `-Config`. Es bricht ab, ohne etwas zu ändern, wenn einer davon im Standardordner (`%LOCALAPPDATA%\postkauz`, `%APPDATA%\postkauz`), in einem mit `-Live` genannten Pfad oder (Datenverzeichnis) in einem nicht leeren Ordner liegt. Der Dauerbetrieb mit 0.2.0-rc8 läuft unberührt weiter.
- **Nur lesend:** `account add`, `account login`, `account test`, `sync` (nur INBOX, Fenster 200, Mails über 1 MiB werden übersprungen), `search`, `doctor`, `audit`, `draft list`. Kein `draft`-Anlegen, keine Aktion.
- **Nur mit deiner Zustimmung:** Ohne `-Zustimmung "ja, <Datum Uhrzeit>"` läuft nur Teil 1 (offline, ohne Microsoft). Die Zeile steht im Protokoll.
- **Keine Geheimnisse im Protokoll:** Adresse, Client-ID, API-Schlüssel und Token maskiert das Skript (`<adresse>`, `<client-id>`, `pk_key_********`). Suchtreffer erscheinen nur als Zahl. Der Anmeldecode erscheint nur auf dem Bildschirm.

## 2. Vorbereitung

1. **[Du]** App registrieren nach [betrieb_microsoft.md](betrieb_microsoft.md), Abschnitt 1. Ergebnis: die Client-ID (GUID). Gib sie Claude im Chat; sie kommt nur als Parameter ins Skript, nie in eine Datei des Repositorys.
2. **[Claude]** Ordner `C:\PostKauz\d9` anlegen, darin `postkauz.exe` (aus `dist/postkauz-windows-amd64.exe`, umbenannt), `SHA256SUMS` und `abnahme_D9.ps1`. Datenverzeichnis `C:\PostKauz\d9\daten` darf es noch nicht geben.
3. **[Du]** Ein Wort aus einer eigenen Mail im Posteingang nennen (für die Suche), z. B. ein Wort aus dem Betreff einer Mail, die du dir selbst geschickt hast.
4. **[Du]** Zustimmung geben, z. B. im Chat: „D9 gegen mein Microsoft-Postfach: ja“. Claude trägt Datum und Uhrzeit ein.

## 3. Lauf

Das Fenster muss sichtbar sein: Bei den zwei Anmeldungen fragt das Skript „Weiter mit Enter“, und bei der Code-Anmeldung steht der Code im Fenster.

```powershell
cd C:\PostKauz\d9
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\abnahme_D9.ps1 `
  -Exe .\postkauz.exe -DataDir C:\PostKauz\d9\daten -Config C:\PostKauz\d9\config.toml `
  -Live <Live-Datenverzeichnis>,<Live-config.toml> `
  -User <deine Adresse> -ClientId <Client-ID> -Tenant consumers `
  -SearchWord <Wort> -Zustimmung "ja, <TT.MM.JJJJ hh:mm>"
```

`-Tenant consumers` gilt für private Konten (outlook.com, hotmail.com); für ein Firmenkonto `organizations`.

### Was du an den zwei Stellen tust

- **Nach 2.4 („Gleich öffnet sich der Browser“)** [Du]: Enter drücken. Der Browser zeigt die Anmeldeseite von Microsoft. Anmelden, beim ersten Mal den Berechtigungen zustimmen („Auf Ihre Postfächer zugreifen“, „Zugriff auf Daten beibehalten“). Der Browser zeigt danach „Sign-in received. You can close this window and return to the terminal.“ Geht kein Browser auf: Die Adresse steht im Fenster (Zeilen mit `|`); in den Browser kopieren. Frist 5 Minuten.
- **Nach 2.11 („Anmeldung mit Code“)** [Du]: Enter drücken. Im Fenster steht `To sign in, open https://microsoft.com/devicelogin in a browser and enter the code XXXX-XXXX`. Die Adresse auf einem beliebigen Gerät öffnen (auch Handy), Code eingeben, anmelden.

## 4. Erwartet

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.1 | Konfiguration | angelegt mit `max_message_bytes = 1048576` |
| 0.2 | `version` | nennt `0.3.0-ms.1` |
| 0.3 | Prüfsumme | passt zu `SHA256SUMS` |
| 0.4 | `doctor --json` | `keyring: available` |
| 1.1 | `account add … --auth oauth2` ohne `--client-id` | Exit 2, Meldung nennt `client_id` |
| 1.2 | `account add … --auth oauth2 --password-stdin` | Exit 2 |
| 1.3 | `account login` auf Passwort-Konto | Exit 2 |
| 1.4, 1.5 | Probekonto weg, Schlüssel `test-agent` angelegt | Exit 0 |
| 2.1 | `account add outlook … --auth oauth2 --client-id … --tenant …` | `no token yet, sign in with: account login outlook` |
| 2.2 | `doctor --json` | `token: missing` |
| 2.3 | `account login outlook --key test-agent` | Exit 3, `human_only`, kein Browser |
| 2.4 | `account login outlook --dry-run --json` | `refresh_token: would_store`, kein Browser |
| 2.5 | `account login outlook` **[Du]** | `account outlook: signed in; refresh token stored in the OS keyring as account/…/outlook; access token valid until …` |
| 2.6 | `account test outlook` | `login: ok (XOAUTH2)`, Ordnerliste |
| 2.7 | `sync -a outlook --mailbox INBOX --window 200` | Exit 0 |
| 2.8 | `search <Wort> -a outlook` | mindestens 1 Treffer |
| 2.9 | `doctor --json` | `token: stored`, `token_expiry` und `token_updated_at` gesetzt |
| 2.10 | `audit --since 2h --json` | `accounts.login` ok, Ablehnung `human_only`, Systemereignis `oauth.refresh` ok |
| 2.11 | Audit | kein Token, kein JWT |
| 2.12 | `account login outlook --device-code` **[Du]** | Code im Fenster, danach `signed in` |
| 2.13 | `account test outlook` | `login: ok (XOAUTH2)` |
| 2.14 | `doctor --secrets` | kein `FAIL` (das Refresh-Token steht weder in Log noch Konfiguration noch Datenbank) |
| 2.15 | `draft list -a outlook` | leer |

Letzte Zeile des Protokolls: `D9: <n> OK, 0 FAIL`. Protokoll: `C:\PostKauz\d9\D9_windows.txt`; die Prüfsitzung legt es (nachgesehen, ohne Adressen) als `docs/protokolle/D9_windows.txt` ab.

## 5. Danach (deine Entscheidung)

- **Behalten:** Konto und Token bleiben im Datenverzeichnis `C:\PostKauz\d9\daten` und im Schlüsselbund. Für den Dauerbetrieb wird das Konto später im Live-Datenverzeichnis neu angelegt (nach dem Merge von T4 in eine freigegebene Version), nicht von hier übernommen.
- **Entfernen:** Skript erneut mit `-Entfernen` starten geht nicht (das Datenverzeichnis ist dann nicht leer). Stattdessen: [Claude] führt aus
  `.\postkauz.exe --data-dir C:\PostKauz\d9\daten --config C:\PostKauz\d9\config.toml account remove outlook` (löscht das Konto und im Schlüsselbund Haupteintrag und alle Teile des Tokens; erwartet: `account outlook removed; stored token deleted`, bis 0.3.0-alpha.3 stand dort fälschlich `stored password deleted`), zur Kontrolle `cmdkey /list | findstr /e /c:"/<kennung>/outlook"` (keine Zeile mehr, weder Haupteintrag noch Teile), danach `Remove-Item C:\PostKauz\d9\daten -Recurse`. Nicht `cmdkey /delete` statt `account remove`: Die Teile eines langen Tokens heißen `oauthpart<n>g<generation>/…`, die Generation steht nur im Haupteintrag. `account remove` löscht den Haupteintrag, die Teile der Generation, die er nennt, die der davor abgelösten Generation und Teile ohne Generation. Wer gleich beim ersten Lauf abbauen will, hängt `-Entfernen` an; das Skript ruft dann am Ende `account remove` auf (Schritte 3.1 bis 3.3) und löscht das Datenverzeichnis.
- **Wenn Prüfung 3.2 FAIL meldet** (oder die Kontrolle von Hand noch Zeilen zeigt): Unter der FAIL-Zeile im Protokoll stehen die Einträge, die noch im Schlüsselbund liegen.
  - Zeilen mit `oauthpart<n>g<generation>/<kennung>/outlook`: Teile eines älteren, schon abgelösten Refresh-Tokens, die kein Haupteintrag mehr nennt. Das ist der bekannte seltene Rest (der Schlüsselbund antwortete beim Speichern nicht binnen 10 Sekunden und das Schreiben kam nie an, oder ein Build vor `0.3.0-ms.3` ohne Token-Sperre speicherte gleichzeitig mit einem anderen Prozess; Festlegung 5 in `docs/protokolle/T4_stand.md`). `account remove` findet sie nicht, weil der Schlüsselbund nicht auflisten kann. Das Token darin kann noch gelten, also löschen.
  - Eine Zeile mit `account/<kennung>/outlook` (Haupteintrag): `account remove` hat den Schlüsselbund nicht erreicht. Das ist ein Fehler; die Prüfsitzung meldet ihn.
  - Abhilfe in beiden Fällen: [Claude] löscht jede genannte Zeile einzeln mit `cmdkey /delete:"<Ziel>"`, wobei `<Ziel>` der Text hinter `Ziel:` (englisches Windows: `Target:`) ist, z. B. `cmdkey /delete:"LegacyGeneric:target=postkauz:oauthpart1g9f2c01ab/1a2b3c4d/outlook"` (ungeprüft; meldet `cmdkey` „nicht gefunden“, dasselbe ohne `LegacyGeneric:target=`). Danach nochmals `cmdkey /list | findstr /e /c:"/<kennung>/outlook"`: keine Zeile mehr. [Du] entziehst anschließend die Zustimmung (nächster Punkt). Bewertung: Waren nur `oauthpart`-Zeilen übrig und sind sie jetzt weg, zählt D9 als bestanden mit Anmerkung im Protokoll.
- **Zustimmung entziehen** [Du]: <https://account.live.com/consent/Manage> → die App „PostKauz (privat)“ → „Diese Berechtigungen entfernen“. Die App-Registrierung selbst kannst du behalten (sie enthält kein Geheimnis) oder im Entra-Portal unter **Identität** → **Anwendungen** → **App-Registrierungen** → App → **Löschen** entfernen.
- Den Schlüssel `test-agent` gibt es nur in diesem Datenverzeichnis; er läuft nach einem Tag ab.

## 6. Bekannte Grenzen

- Die Portal-Schritte sind **ungeprüft** (Stand Oktober 2026). Weicht ein Menü ab, notiert Claude die tatsächlichen Namen im Protokoll, und die Anleitung wird danach korrigiert.
- Ob ein privates Microsoft-Konto ohne Azure-Verzeichnis überhaupt eine App registrieren darf, ist offen (Frage in `docs/protokolle/T4_stand.md`).
- Nur Windows mit Schlüsselbund (Rückfrage R2); auf einem Server ohne Schlüsselbund lehnt PostKauz OAuth2-Konten ab.
