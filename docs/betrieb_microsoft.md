# Betrieb: Microsoft-Postfach per OAuth2 (S3.7)

**Stand Oktober 2026, ungeprüft.** Die Schritte im Microsoft-Portal stammen aus der Doku von Microsoft und aus Anleitungen anderer Programme, nicht aus einem eigenen Durchlauf. Microsoft benennt Menüs häufig um. Die Abnahme D9 prüft die Schritte am echten Konto; danach wird diese Anleitung korrigiert und der Vermerk entfernt.

Für: den Betreiber. Diese Anleitung kommt beim Zusammenführen von Teil T4 als Abschnitt „Microsoft“ nach `docs/betrieb.md` (die Datei legt Teil T2 an).

## Warum eine eigene App-Registrierung

Microsoft lässt IMAP nur noch mit OAuth2 zu, nicht mehr mit Passwort. PostKauz bringt keine eigene App mit („bring your own client“, Entscheid E11): Du registrierst einmal eine App in deinem Microsoft-Konto und gibst PostKauz nur deren **Client-ID** (eine Kennung, kein Geheimnis). Ein Client-Geheimnis (Secret) brauchst du nicht und legst du nicht an.

Grenze (Rückfrage R2): OAuth2-Konten laufen nur auf Rechnern mit Schlüsselbund, also auf deinen Windows-PCs, nicht auf einem Server ohne Schlüsselbund.

## 1. App registrieren (einmalig, im Browser)

1. Öffne <https://entra.microsoft.com> und melde dich mit dem Microsoft-Konto an, dem das Postfach gehört.
   - Meldet das Portal, dass zu deinem Konto kein Verzeichnis gehört, braucht ein privates Konto zuerst ein (kostenloses) Azure-Konto. Das ist ungeprüft; in dem Fall bei der Abnahme D9 melden, dann suchen wir den Weg gemeinsam.
2. Klicke links **Identität** → **Anwendungen** → **App-Registrierungen** → oben **Neue Registrierung**.
3. Fülle aus:
   - **Name:** `PostKauz (privat)` (frei wählbar).
   - **Unterstützte Kontotypen:** „Konten in einem beliebigen Organisationsverzeichnis und persönliche Microsoft-Konten“ (für outlook.com/hotmail.com nötig).
   - **Umleitungs-URI:** Plattform „Öffentlicher Client/nativ (mobil und Desktop)“, Adresse `http://localhost` (genau so, ohne Port und ohne Schrägstrich am Ende).
4. Klicke **Registrieren**.
5. Auf der Seite **Übersicht** steht **Anwendungs-ID (Client-ID)**, eine Kennung der Form `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`. Kopieren und aufheben (nicht ins Repository, nicht in Protokolle).
6. Klicke links **Authentifizierung** → ganz unten **Öffentliche Clientflows zulassen** → **Ja** → oben **Speichern**. (Nötig für `account login --device-code`.)
7. Klicke links **API-Berechtigungen** → **Berechtigung hinzufügen** → **Microsoft Graph** → **Delegierte Berechtigungen**. Suche und hake an:
   - `IMAP.AccessAsUser.All`
   - `offline_access`

   Dann **Berechtigungen hinzufügen**. Eine Administratorzustimmung ist für ein privates Konto nicht nötig; du stimmst bei der ersten Anmeldung selbst zu.
8. **Nicht** anlegen: Zertifikate und Geheimnisse. PostKauz nutzt PKCE statt eines Geheimnisses.

## 2. Konto in PostKauz anlegen und anmelden

In PowerShell, Programm aus `dist/`. Für die Abnahme D9 immer ein **eigenes, frisches Datenverzeichnis** (`--data-dir`), nie das Live-Verzeichnis des Dauerbetriebs.

```powershell
.\postkauz.exe --data-dir C:\PostKauz-D9 account add outlook --imap outlook.office365.com:993 --user <deine Adresse> --auth oauth2 --client-id <Client-ID> --tenant consumers
```

Erwartet: `account outlook added (outlook.office365.com:993, tls tls, user …); OAuth2 client …, tenant consumers; no token yet, sign in with: account login outlook`

- `--tenant consumers` gilt für private Konten (outlook.com, hotmail.com, live.com). Für ein Firmen- oder Schulkonto `organizations` oder die Mandanten-ID; ohne Angabe gilt `common`.

```powershell
.\postkauz.exe --data-dir C:\PostKauz-D9 account login outlook
```

Erwartet: Der Browser öffnet die Anmeldeseite von Microsoft. Du meldest dich an und stimmst beim ersten Mal den Berechtigungen zu („Auf Ihre Postfächer zugreifen“, „Zugriff auf Daten beibehalten“). Danach zeigt der Browser „Sign-in received. You can close this window and return to the terminal.“, und im Terminal steht:

`account outlook: signed in; refresh token stored in the OS keyring as account/<kennung>/outlook; access token valid until … (kept in memory only, renewed automatically)`

Ohne Browser auf diesem Rechner (oder wenn er nicht aufgeht):

```powershell
.\postkauz.exe --data-dir C:\PostKauz-D9 account login outlook --device-code
```

Erwartet: `To sign in, open https://microsoft.com/devicelogin in a browser and enter the code XXXX-XXXX (valid until hh:mm).` Die Adresse auf einem beliebigen Gerät öffnen (auch Handy), Code eingeben, anmelden; das Terminal meldet danach dasselbe wie oben.

## 3. Prüfen

```powershell
.\postkauz.exe --data-dir C:\PostKauz-D9 account test outlook
.\postkauz.exe --data-dir C:\PostKauz-D9 doctor
```

Erwartet: `login:         ok (XOAUTH2)` und die Ordnerliste; `doctor` zeigt `outlook: OAuth2 token stored, last access token valid until …, last renewed …`.

## 4. Was im Betrieb passiert

- Gespeichert ist nur das **Refresh-Token** (im Windows-Anmeldeinformationsspeicher, Dienst `postkauz`). Das Access-Token (etwa 1 Stunde gültig) bleibt im Speicher und wird 2 Minuten vor Ablauf erneuert; Microsoft gibt dabei jedes Mal ein neues Refresh-Token aus, das PostKauz sofort speichert.
- Jede Erneuerung steht im Audit (`oauth.refresh`, Ergebnis `ok` oder `error` mit Code), ohne Token.
- **Lehnt Microsoft die Erneuerung ab** (Passwort geändert, Zustimmung widerrufen, 90 Tage nicht benutzt): Fehler `oauth_refresh`, `sync --watch` beendet sich. Abhilfe: `account login outlook` erneut ausführen.
- **Kann PostKauz das neue Token nicht speichern** (Schlüsselbund gesperrt): Fehler `token_not_stored`, das Programm hält an, statt still weiterzulaufen. Abhilfe: `doctor` prüfen, Windows neu anmelden, Kommando wiederholen.
- Zugriff entziehen: <https://account.live.com/consent/Manage> (privates Konto) bzw. im Entra-Portal die App löschen; danach `account remove outlook` oder `account login outlook` neu.

## 5. Grenzen

- Nur lesen und Entwürfe ablegen wie bei jedem Konto; in der Abnahme D9 ausdrücklich **nur lesen** (kein Entwurf, keine Aktion).
- Kein Server-Betrieb ohne Schlüsselbund (R2); `POSTKAUZ_PASSWORD_<NAME>` gilt für OAuth2-Konten nicht, der unsichere Testspeicher `insecure-file` auch nicht.
- Google-Konten bleiben bei App-Passwörtern (keine eigene Google-OAuth-App, Nicht-Ziel).
