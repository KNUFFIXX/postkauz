# Abnahme D4: Entwürfe, Freigabe, Prüf-Hook, MCP (Schnitt 2)

Stand: 22.10.2026. Gehört zu `auftraege/A02_s2.md`, Teil D, Abschnitt D4 (nach S2.8).

**Kurz:** Claude startet auf deinem Büro-PC ein Skript, das ohne sichtbares Fenster läuft, alles prüft und ein Protokoll schreibt (Entscheid 35). Du tippst nichts ein. Du bekommst eine Kurzfassung und **entscheidest nur, wenn eine Zeile `FAIL` heißt**. Selbst machst du nur Punkt 8: ein kurzes Gespräch mit deinem Claude (etwa 5 Minuten).

Das Skript braucht kein Docker, kein Go und keinen Mailserver. Es legt eigene Testdaten an (zwei erfundene Mails, Adressen nur `example.org`) und berührt dein echtes PostKauz-Datenverzeichnis (`C:\Claude\postkauz`) nie. Weil es kein Postfach gibt, bleibt jeder Entwurf `remote: pending`: Er würde erst beim nächsten `sync` in den Entwürfe-Ordner gelegt. Das ist hier erwartet; gesendet wird nie etwas.

## 1. Was das Skript prüft

Jede Prüfung ist eine Zeile `OK <Nr> <Beschreibung>` oder `FAIL <Nr> <Beschreibung>`, darunter die tatsächliche Ausgabe (gekürzt). Die letzte Zeile lautet `D4: <n> OK, <m> FAIL`; davor stehen alle Abweichungen noch einmal gesammelt.

| Nr | Was passiert | Erwartet |
|---|---|---|
| 0.1–0.8 | Vorbereitung: frischer Ordner `lauf-<Datum-Uhrzeit>`, Konto `demo` ohne Server, zwei Testmails per `import mbox`, Identität `demo` mit Signatur, Policy-Datei mit dem Beispiel-Hook `check-example.ps1`, Ordner `attach` mit `beispiel.txt`, Schlüssel `test-agent` (nur lesen) und `test-assistant` (Entwürfe) | alles `OK`; `doctor` zeigt den Laufordner, `policy: … (found …)` und `attach:` |
| 1.1–1.6 | `draft reply <Mail> --body-file antwort.md --attach beispiel.txt`, dann `draft show 1 --eml` und `draft show 1` | `draft 1 created (status draft, remote pending)`; in der Nachricht `In-Reply-To` und `References` der Ausgangsmail, Betreff `Re: Termin Heizungswartung`, Zitat mit `> `, Signatur, Anhang; Umlaute korrekt |
| 2.1 | `draft request 1` | `hook: pass`, Status `pending_approval`, `approval 1` |
| 3.1–3.4 | Betreff mit `TODO`, dann `draft request 1` | `hook: fail` mit der Begründung des Beispiel-Hooks (`subject contains TODO`), Entwurf bleibt `draft`; danach kein Temp-Ordner `postkauz-hook-*` übrig |
| 4.1–4.5 | Betreff zurück, `draft request 1`, `approve 2`, dann Text ändern, `approve 2` | erst `ready`; nach der Änderung `draft`, Freigabe `superseded`; das zweite `approve 2` → `denied (approval_stale)`, Exit 3 |
| 5.1–5.4 | `policy show --key test-agent`, `draft request 1`, `approve 3 --key test-agent` | `drafts.approve: human_only`; `denied (human_only)`, Exit 3; Freigabe und Entwurf unverändert |
| 6.1–6.4 | `audit --since 10m` (Text und `--json`), Log-Datei | je Übergang eine Zeile mit Akteur `human`, Draft-ID, Status vorher/nachher, Hook-Ergebnis (`pass` und `fail`); kein Wort aus dem Entwurf (Text, Betreff, Adresse, Signatur, Anhangname) in Audit und Log |
| 7.0–7.8 | MCP-Probe in Python (`abnahme_D4_mcp.py`, spricht mit `postkauz mcp` wie dein Claude) | `test-agent`: **9** Tools; `test-assistant`: **15** Tools, darunter `mail_create_draft` und `mail_request_ready`, keines mit approve, reject, send, delete, move; `mail_create_draft` mit `dry_run: true` speichert nichts, mit `dry_run: false` liefert es `draft_id`, `message_id`, `remote: pending`; `draft list` zeigt ihn; Audit: Akteur `agent`, Schlüssel `key:test-assistant`, kein Text |
| 8.0 | MCP-Eintrag für Punkt 8 vorbereiten | Datei `mcp-eintrag-d4.json` im Laufordner (gültiges JSON, ohne Schlüssel; der steht nur in `keys\test-assistant.key`) |
| 9.1–9.4 | `draft abandon <Entwurf aus 7>`, danach `mail_update_draft` über MCP | `abandoned`; danach `denied (draft_closed)`, auch im Audit |

Das Skript prüft an festen Merkmalen (Status, Exit-Code, Zählungen, Wörter wie `human_only`, `approval_stale`, `hook: pass`), nicht an ganzen Hinweissätzen. So bleibt es gültig, wenn sich ein Hinweistext ändert.

## 2. Wie Claude es startet

Claude legt vier Dateien nach `C:\Users\Admin\AI\Cowork_Temp\PostKauz\d4\`:

| Datei | Herkunft im Repository |
|---|---|
| `postkauz.exe` | der zu prüfende Stand (`dist/postkauz-windows-amd64.exe`, umbenannt) |
| `abnahme_D4.ps1` | `docs/abnahme_D4.ps1` |
| `abnahme_D4_mcp.py` | `docs/abnahme_D4_mcp.py` |
| `check-example.ps1` | `deploy/hooks/check-example.ps1` |

Dann, in PowerShell (eine Zeile; `Unblock-File` entfernt die Markierung „aus dem Internet"):

```powershell
$d='C:\Users\Admin\AI\Cowork_Temp\PostKauz\d4'; Unblock-File "$d\postkauz.exe","$d\abnahme_D4.ps1","$d\check-example.ps1"; powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$d\abnahme_D4.ps1" -Exe "$d\postkauz.exe"
```

Weitere Schalter, nur falls nötig: `-WorkDir <Ordner>` (Standard wie oben), `-Python <python.exe>` (Standard `C:\Users\Admin\AppData\Local\Programs\Python\Python312\python.exe`), `-Hook`, `-Probe`. Der Lauf dauert etwa eine bis zwei Minuten. Alle Programme starten ohne Fenster; du kannst nebenher weiterarbeiten.

**Ergebnis:** `C:\Users\Admin\AI\Cowork_Temp\PostKauz\d4\D4_windows.txt` (dieselbe Datei auch im Laufordner). Exit-Code 0 nur ohne `FAIL`. Schlüssel stehen darin nur maskiert (`pk_key_********`). Claude legt das Protokoll unter `docs/protokolle/D4_windows.txt` ins Repository und schreibt dir die Kurzfassung.

Jeder Lauf bekommt einen neuen Ordner `lauf-<Datum-Uhrzeit>`; ältere Läufe bleiben liegen und können nach der Abnahme gelöscht werden (nur Testdaten und Testschlüssel).

## 3. Was du entscheidest

- **Alles `OK`:** Punkte 1–7 und 9 sind bestanden. Es fehlt nur noch Punkt 8 (unten).
- **Eine oder mehrere Zeilen `FAIL`:** Claude schreibt dir je Abweichung, was erwartet war, was kam und wie schlimm es ist. Du entscheidest: nachbessern lassen oder hinnehmen (Teil D6 des Auftrags).

## 4. Punkt 8: Gespräch mit deinem Claude (du)

Das Skript hat dafür alles vorbereitet; die Angaben stehen im Protokoll im Abschnitt `D4.8`.

1. **Claude trägt den Testserver ein.** Er übernimmt den Eintrag aus `mcp-eintrag-d4.json` (Laufordner) als MCP-Server `postkauz-d4` in deine Claude-App (bei Claude Code: `claude mcp add-json postkauz-d4 '<Inhalt der Datei>' --scope user`). Der Schlüssel steht nicht im Eintrag, sondern in der Datei `keys\test-assistant.key`, auf die er zeigt. Danach die App neu starten.
2. **Du sagst in einem neuen Chat** (die Nummer steht im Protokoll unter D4.8):

   > Beantworte im PostKauz-Testpostfach (Werkzeuge postkauz-d4) Mail 2 kurz, dass der Termin passt.

   Den Server ausdrücklich nennen: Hat dein Claude weitere Mail-Werkzeuge (etwa die Gmail-Integration), nimmt er sonst womöglich die und sucht „Mail 2" in deinem echten Postfach (Befund D4.8 vom 23.10.2026; er fragte nach, statt zu raten).

   Erwartet: Dein Claude ruft `mail_create_draft` auf (vielleicht erst mit `dry_run: true` zur Vorschau, dann mit `dry_run: false`). Seine Antwort nennt eine Entwurfsnummer (`draft_id`), eine `message_id` und `remote: pending`. Er sagt dir, dass er nichts sendet. Reicht er den Entwurf von sich aus zur Freigabe ein (`mail_request_ready`), ist das auch richtig: Freigeben kann er nicht.
3. **Claude prüft nach** (Kommandos mit `--data-dir` und `--config` des Laufordners): `draft list` zeigt den neuen Entwurf; `audit --since 10m --key-id key:test-assistant` zeigt Akteur `agent`, Schlüssel `key:test-assistant` und keinen Text aus dem Entwurf.
4. **Punkt 9 im Chat:** Claude verwirft den Entwurf (`draft abandon <Nummer>`). Dann sagst du:

   > Ändere den Entwurf <Nummer>: schreib noch „Viele Grüße" dazu.

   Erwartet: Dein Claude meldet, dass das nicht geht: `denied (draft_closed)`.
5. Zum Schluss entfernt Claude den Eintrag `postkauz-d4` wieder.

Dein Urteil zählt hier: Wirkt die Antwort deines Claude verständlich und ehrlich („Entwurf liegt bereit, gesendet wird nichts")? Wenn nicht, sag Claude, was dich stört.

## 5. Grenzen und Selbsttest

- **Nicht Teil von D4:** die echte Ablage im Entwürfe-Ordner eines Postfachs und die Versanderkennung. Das misst D5 mit deinem eigenen Postfach.
- **Vorab im Container belegt:** `sh scripts/abnahme-d4-trocken.sh bin/postkauz` führt dieselben Kommandos mit denselben Testdaten und Prüfmustern gegen das Linux-Programm aus (Hook `check-example.sh`, MCP-Probe unverändert): alle Prüfungen `OK`. Nur unter Windows prüfbar sind PowerShell 5.1 selbst (Aufruf, Quoting, Kodierung der Ausgabe), der Hook über `powershell.exe` mit knapper Umgebung, Python unter Windows und der Start ohne Fenster.
