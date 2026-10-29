# Abnahme D8: HTTP-MCP mit Schlüssel und OAuth über das private Netz

Stand: 29.10.2026 (A03 Teil T5 Phase 2 mit zweiter Nachbesserung, Build `0.3.0-alpha.8c`). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Teil T5, Haltepunkt „Abnahme D8“, und zu [auftraege/A03_s3.md](../auftraege/A03_s3.md), Teil D, Abschnitt D8, **vorläufig mit einem PC des Betreibers als Knoten im privaten Netz**, bis über einen VPS entschieden ist. Skripte: [abnahme_D8.ps1](abnahme_D8.ps1) (Windows, Teile A, B, D), [abnahme_D8_cloud.sh](abnahme_D8_cloud.sh) (Cloud-Sitzung, Teil C), gemeinsame Probe [abnahme_D8_probe.py](abnahme_D8_probe.py) (nur Python-Standardbibliothek). Ausgeführt von der Prüfsitzung; der Betreiber übernimmt nur Tailnet, Auth-Key und die Anmeldung im Browser.

## Zweck

T5 bringt `postkauz mcp --http`: dieselben MCP-Werkzeuge wie über stdio, aber über HTTP, zustandslos, für Agenten außerhalb des Rechners (zuerst eine Claude-Code-Cloud-Sitzung im Tailnet). Zwei Arten der Anmeldung ([docs/betrieb.md](betrieb.md), Abschnitt 4):

- **Modus `key`:** Der Client schickt einen PostKauz-Schlüssel als `Authorization: Bearer …`. Nur Loopback und Tailnet.
- **Modus `oauth`:** Der Client meldet sich bei einem eigenen Anmeldedienst (Authelia) an; PostKauz prüft nur dessen Token und ordnet das Subjekt einem Schlüssel zu.

D8 prüft beides: lokal ohne Netz (Teil A), über Tailscale vom PC aus (Teil B), aus der Cloud-Sitzung (Teil C) und im Modus `oauth` mit Anmeldung im Browser (Teil D).

## Grundregeln

- **Nie die Live-Datenbank** (Rahmen 5): `-DataDir`, `-Config` und `-LivePath` sind Pflicht (wie bei D7); das Skript legt Datenverzeichnis und Konfiguration neu an und bricht ohne Änderung ab, wenn sie oder `-WorkDir` im Standard-Datenverzeichnis, im Standard-Konfigurationsordner oder in einem mit `-LivePath` genannten Pfad liegen (oder umgekehrt). Ein Datei-Eintrag in `-LivePath` (etwa `C:\PostKauz\config.toml`) sperrt auch seinen Ordner. Dazu eine Markerdatei gegen Junction und `subst` (für `-DataDir`, `-WorkDir` und den Ordner von `-Config`) und ein Abbruch, wenn neben `-Config` schon eine `policy.toml` liegt. Kein Mailkonto, kein Passwort: `mcp --http` kennt keins.
- **Port:** `-Port` (Standard 18787) auf `127.0.0.1`, nie der Standardport 8787 (ein laufender Dienst bleibt ungestört).
- **`tailscale serve` bleibt nicht verändert:** Vor dem Einschalten liest das Skript `tailscale serve status --json`. Ist HTTPS auf Port 443 schon belegt (oder der Zustand nicht lesbar), bricht es mit Hinweis ab und überschreibt nichts. Eingeschaltet wird nur mit „ja“; am Ende von Teil B und D fragt es vor `tailscale serve --https=443 off` und prüft danach, dass 443 wieder frei ist. `tailscale serve reset` nutzt das Skript nie (es löschte auch andere Ports, etwa einen Anmeldedienst auf 8443).
- **Aufräumen auch bei Abbruch:** Teile A, B und D laufen in `try`/`finally`. Bei einem Fehler oder Strg+C schaltet das Skript `tailscale serve` ohne weitere Frage aus (das sagt die Frage vor dem Einschalten), beendet `mcp --http`, widerruft die angelegten Schlüssel und löscht Schlüssel- und Refresh-Dateien. `mcp --http` gilt gleich nach dem Start als laufend (nicht erst nach dem Warten auf `/healthz`), `tailscale serve` schon vor dem Aufruf von `serve --bg` als an; beim Aufräumen nach einem Abbruch zählt dann nur, dass HTTPS 443 danach frei ist (ein Fehler von `off`, weil nichts an war, steht nur im Detail).
- **Geheimnisse:** Schlüssel liegen nur während des Laufs in `<WorkDir>\lauf-…\keys\` und werden danach gelöscht und widerrufen. Der Auth-Key von Tailscale und der Schlüssel für Teil C kommen als **Geheimnis der Cloud-Sitzung**, nie ins Repository, nie in ein Protokoll.
- **Protokolle** `D8_windows.txt` (Windows) und `D8_cloud.txt` (Cloud-Sitzung): exklusiv neu angelegt; Schlüssel (`pk_key_********`), Token (`<token>`), Auth-Keys (`tskey-********`), Pfade (`<DataDir>` …), der Knotenname (`<knoten>`), der Aussteller (`<issuer>`, sein Host `<issuer-host>`), jeder andere Name unter `ts.net` (`<name>.ts.net`) und Tailnet-Adressen (`<tailnet-ip>` für 100.64.0.0/10, `<tailnet-ip6>` für `fd7a:115c:a1e0:…`) sind maskiert. Knotenname und Aussteller maskiert das Windows-Skript wörtlich (nicht als Pfad), auch in `resource`, `authorization_servers` und `aud`. Jede Zeile `OK`, `FAIL` oder `SKIP` mit Nummer; letzte Zeile `D8: …` bzw. `D8 Teil C: …`. Die Probe muss mit ihrer Schlusszeile `probe: <n> OK, <m> FAIL, <k> SKIP` (passend zu ihren Zeilen) und Exit 0 bzw. 1 enden; sonst (Abbruch, falsche Argumente, Syntax- oder Importfehler) zählen beide Skripte `FAIL <Nr>.ende` (z. B. `A3.ende`, `C3.ende`). Exit 0 nur ohne `FAIL`, Exit 2 bei Abbruch (vor dem ersten Schritt oder mittendrin, dann nach dem Aufräumen).
- **Abweichung zu A03 (Festlegung, Einspruch möglich):** A03 Teil D nennt für `reader` 9 Tools; seit T2 sind es **12** (dazu `mail_get_action`, `mail_list_actions`, `mail_list_tags`). Die Skripte erwarten 12.

## Voraussetzungen

| Datei | Herkunft |
|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` dieses Zweigs, umbenannt; Prüfsumme gegen `dist/SHA256SUMS` |
| `abnahme_D8.ps1`, `abnahme_D8_probe.py` | `docs/` (nebeneinander) |
| `abnahme_D8_cloud.sh`, `abnahme_D8_probe.py` | `docs/`, in der Cloud-Sitzung |

- Python 3.8 oder neuer (Windows: `-Python` oder `python.exe`/`py.exe` im `PATH`). Ohne Python laufen die HTTP-Prüfungen von Teil A trotzdem (in PowerShell), die MCP-Sitzung (A3) steht als `SKIP`.
- **Teil B:** Tailscale für Windows auf dem PC, angemeldet, MagicDNS und „HTTPS Certificates“ in der Admin-Konsole an. Name des PCs im Tailnet (`-NodeName`, z. B. `pc.<tailnet>.ts.net`).
- **Teil C:** in der Cloud-Sitzung `tailscale` und `tailscaled` (statisches Paket von `pkgs.tailscale.com`, ungeprüft, ob die Netzregel der Sitzung es erlaubt), `curl`, `openssl`, `python3`; vom Betreiber ein **ephemerer, zeitlich begrenzter Auth-Key mit Tag** (z. B. `tag:cloud`) und eine ACL, die diesem Tag nur Port 443 des PCs erlaubt.
- **Teil D:** Authelia im Tailnet nach [docs/betrieb.md](betrieb.md) 4.6 (braucht Linux: VPS oder WSL2 auf einem PC), mit einem Testkonto und dem Client `claude-code` (Rückleitung `http://localhost:33418/callback`, `audience` = `https://<NodeName>/mcp`). Für Schritt C4/D3.9 optional ein Token eines zweiten Clients für eine andere Resource.

## Teil A: lokal, offline (Windows)

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PK\d8\abnahme_D8.ps1 -ExePath C:\PK\d8\postkauz.exe -DataDir C:\PK\d8\daten -Config C:\PK\d8\config.toml -WorkDir C:\PK\d8 -LivePath C:\PostKauz\data,C:\PostKauz\config.toml
```

| Nr | Schritt | Erwartet |
|---|---|---|
| A0.1 | `version` | `0.3.0-alpha.8c` (`-ExpectVersion`; `alpha.8` passt nicht auf `alpha.8b`/`alpha.8c`) |
| A1.1 | `doctor --json` | `http.listen` `127.0.0.1:<Port>`, `auth` `key`, `listen_allowed` true |
| A1.2 | `key create --name cloud-reader --role reader --expires 1d` | Schlüssel in einer Datei im Laufordner |
| A1.3 | `mcp --http --listen 0.0.0.0:<Port>` | Exit 2, Meldung nennt `allow_non_loopback`, kein Server |
| A2.1 | `mcp --http` im Hintergrund, `GET /healthz` | 204 ohne Inhalt |
| A2.2 | `GET /.well-known/oauth-protected-resource/mcp` | JSON, `resource` = `http://127.0.0.1:<Port>/mcp`, ohne `authorization_servers` |
| A2.3 | `POST /mcp` ohne Bearer | 401, `WWW-Authenticate: Bearer resource_metadata="…"` |
| A2.4 | unbekannter Schlüssel | 401 |
| A2.5 | `GET /mcp` | 405 |
| A2.6 | fremder `Host` | 421 |
| A2.7 | Anfrage über 1 MiB | 413 (SKIP, wenn Windows nach dem vorzeitigen Schließen keinen Status liest) |
| A2.8 | `initialize` mit Schlüssel | 200, keine `Mcp-Session-Id`, keine `Server`-Kopfzeile |
| A3.x | Probe `key` | dieselben Prüfungen, dann `tools/list` = 12, kein Tool zum Senden/Löschen/Freigeben, `mail_search` in der Hülle (`untrusted: true`), `mail_audit_log` mit `transport http` und Principal `cloud-reader`, Schlüssel in keiner Antwort |
| A4.1–A4.2 | `key revoke cloud-reader`, 31 s warten, `initialize` | 403 `{"status":"denied","code":"key_revoked"}` |
| A5.1 | Ausgabe von `mcp --http` | `mcp --http: listening on 127.0.0.1:<Port> (auth key, …)` |
| A5.2 | `audit --raw --json` | Einträge mit `params.via.transport` = `http`; `keys.authenticate` `denied` mit `key_invalid` und `key_revoked` |
| A5.3 | Log, Audit, Ausgabe | kein Schlüssel |
| A5.4 | `audit verify` | `ok` |

## Teil B: dieser PC als Knoten im Tailnet (Windows)

Wie Teil A, zusätzlich `-NodeName pc.<tailnet>.ts.net` (neues `-DataDir`, `-Config`, `-WorkDir`). Das Skript fragt vor jedem Eingriff in Tailscale nach „ja“.

| Nr | Schritt | Erwartet |
|---|---|---|
| B1.1–B1.3 | Schlüssel `cloud-reader-c`, Server mit `resource = https://<knoten>/mcp` | läuft; `Host` 127.0.0.1 → 421 (nur der Name des Knotens gilt) |
| B2.0 | `tailscale serve status --json` | HTTPS 443 frei (sonst Abbruch mit Hinweis, Exit 2) |
| B2.1 | `[Du]` `tailscale serve --bg <Port>` | Exit 0 |
| B2.2 | `https://<knoten>/.well-known/oauth-protected-resource/mcp` vom PC | 200, `resource` = `https://<knoten>/mcp`, Zertifikat gültig |
| B3.x | Probe `key` über `https://<knoten>/mcp` | wie A3 |
| B4 | `[Du]` Server läuft, bis du Enter drückst: jetzt Teil C | – |
| B4.1 | `key revoke cloud-reader-c` (nach der ersten Enter-Taste; danach legt die Prüfsitzung in der Cloud-Sitzung die Datei `go` an, siehe C5) | Exit 0 |
| B5.1 | `[Du]` `tailscale serve --https=443 off`, dann `status --json` | Exit 0, HTTPS 443 wieder frei |

## Teil C: aus der Cloud-Sitzung (Linux)

Während Teil B auf Enter wartet. Der Schlüssel `cloud-reader-c` (Datei aus B4, Pfad steht im Protokoll) und der Auth-Key kommen als Geheimnisse in die Sitzung. Das Skript gibt den Auth-Key nur über eine Datei mit Rechten 600 an `tailscale up --auth-key=file:<pfad>` (nie als Argument, nicht in der Umgebung von `tailscaled`) und löscht die Datei gleich danach. Die Schlüsseldatei für Teil C ebenfalls mit Rechten 600 und außerhalb des Repo-Klons ablegen.

Das Bash-Werkzeug einer Cloud-Sitzung hat keine Eingabe. Darum: Skript **im Hintergrund** starten, das Protokoll mitlesen und die Weiter-Signale als Dateien im Arbeitsordner anlegen (der Pfad steht in der zweiten Zeile des Protokolls):

```bash
# im Hintergrund (&, oder die Hintergrund-Option des Bash-Werkzeugs); die
# Ausgabe ist dasselbe maskierte Protokoll, dazu ABBRUCH-Meldungen
TS_AUTHKEY=<geheim> D8_NODE=pc.<tailnet>.ts.net D8_KEY_FILE=/pfad/cloud-reader-c.txt D8_PRINCIPAL=cloud-reader-c \
  sh docs/abnahme_D8_cloud.sh >"${TMPDIR:-/tmp}/d8-cloud.out" 2>&1 &
# mitlesen (zweite Zeile: Arbeitsordner)
cat "${TMPDIR:-/tmp}/d8-cloud.out"
# später, nach B4.1 (Widerruf) auf dem PC:
touch <Arbeitsordner>/go
```

- **Arbeitsordner:** Standard ist ein neuer Ordner unter `$TMPDIR` bzw. `/tmp` (`mktemp -d`, Name `d8-cloud-…`), nie im Repo-Klon; ein `D8_WORK` im Klon bricht ab (zusätzlich steht `/d8-cloud-*/` in `.gitignore`). Am Ende bleibt dort nur das maskierte `D8_cloud.txt`; rohe Ausgaben von `tailscaled`, `tailscale up` und `openssl` (Peers, Endpunkte) löscht das Skript.
- **Vom Tailnet** steht im Protokoll nur, wie viele andere Knoten sichtbar sind und ob der PostKauz-Knoten darunter ist (aus `tailscale status --json`), keine Namen, kein Login.
- **Warten:** C5 wartet höchstens `D8_WAIT` Sekunden (Standard 1800) auf die Datei `go`; Inhalt `s` überspringt C5.

| Nr | Schritt | Erwartet |
|---|---|---|
| C1.1 | `tailscaled` im Userspace-Modus, `tailscale up` mit dem Auth-Key | beigetreten (ephemer) |
| C1.2 | `tailscale status` | nur der PostKauz-Knoten sichtbar (ACL) |
| C2.1–C2.2 | `curl https://<knoten>/.well-known/oauth-protected-resource` | JSON mit `resource` (im Modus `oauth` auch `authorization_servers`) |
| C2.3–C2.4 | `openssl s_client` | Kette gültig, Ablauf in mehr als 14 Tagen (Akzeptanzkriterium 5) |
| C3.x | Probe `key` | wie A3 (401 ohne Bearer, 405, 12 Tools, Hülle, Audit mit `transport http`) |
| C5 | `[Du]` in Teil B Enter (Widerruf, B4.1), dann Datei `go` im Arbeitsordner anlegen | 403 `denied (key_revoked)` |
| C6.1 | `tailscale logout` | Knoten verlässt das Tailnet |

Danach von Hand: Auth-Key in der Tailscale-Konsole widerrufen, falls noch gültig. Im Modus `oauth` (`D8_MODE=oauth`, dazu `D8_ISSUER=https://<authelia>`: dann steht der Aussteller im Protokoll nur als `<issuer>` bzw. `<issuer-host>`, wichtig bei einem eigenen Domainnamen auf dem VPS) läuft statt C3 die Probe `oauth`; ihre Hinweise erscheinen sofort im Protokoll (Filter zeilenweise): Die Anmelde-URL steht ungefiltert nur in `<Arbeitsordner>/login-url.txt` (Rechte 600) und, falls die Sitzung ein Terminal hat, auf `/dev/tty`, nie im Protokoll. Die Rückleitungs-URL aus dem Browser kommt als eine Zeile mit Zeilenende in `<Arbeitsordner>/callback` (erst `callback.tmp` schreiben, dann umbenennen); die Probe wartet darauf höchstens `D8_WAIT` Sekunden und löscht die Datei nach dem Lesen; die Messung des OAuth-Wegs gehört aber an den PC (Teil D, ADR-010 Folgen: aus einer Cloud-Sitzung bleibt OAuth unsicher, Risiko 3).

## Teil D: Modus oauth mit Anmeldung im Browser (Windows)

Wie Teil B mit zusätzlich `-Issuer https://<authelia>` (Authelia nach betrieb.md 4.6). Erster Lauf ohne `-Subject`: Nach der Anmeldung antwortet PostKauz 403 und das Protokoll nennt das Subjekt (`D3.s0`). Zweiter Lauf (neue Pfade) mit `-Subject "<sub>"`:

| Nr | Schritt | Erwartet |
|---|---|---|
| D1.1–D1.2 | Schlüssel `cloud-oauth`, Server im Modus `oauth` | läuft |
| D1.3–D1.4 | `tailscale serve status --json`, `[Du]` `tailscale serve --bg <Port>` | 443 frei, Exit 0 (wie B2.0–B2.1) |
| D3.1–D3.7 | Metadaten | wie A2, dazu `authorization_servers` und RFC-8414-Metadaten mit PKCE S256 |
| D3.8 | PostKauz-Schlüssel als Bearer | 401 |
| D3.9 | Token für eine andere Audience (optional) | 401 |
| D3.10 | `[Du]` Anmeldung im Browser (Authorization Code mit PKCE S256) | Access- und Refresh-Token |
| D3.11 | Access-Token | `aud` = Resource, Lebensdauer höchstens 1 h (Authelia: 5 min) |
| D3.12 | ID-Token als Bearer | 401 |
| D3.s0, D3s.x | Sitzung | Subjekt zugeordnet; 12 Tools, Hülle, `mail_audit_log` mit `transport http`, Principal, `iss`, `sub`; kein Token in einer Antwort |
| D3.13 | Ablauf abwarten (5 min + 30 s) | 401 |
| D3.14 | Refresh | neues Token → 200 |
| D4.1–D4.2 | `key revoke cloud-oauth`, Refresh, `initialize` | 403 `denied (key_revoked)` |
| D5.1 | `[Du]` `tailscale serve --https=443 off`, dann `status --json` | Exit 0, HTTPS 443 wieder frei |

Danach von Hand ([Du]): Claude Code am PC nach betrieb.md 4.6 Schritt 6 anbinden (`/mcp` → anmelden → `tools/list`), Ergebnis ins Protokoll; Testkonto in Authelia deaktivieren.

## Bewertung

- **Bestanden**, wenn Teil A, B und C ohne `FAIL` laufen (D8 vorläufig, Modus `key`). Teil D gehört dazu, sobald Authelia im Tailnet läuft; bis dahin bleibt D8 „vorläufig ohne OAuth“.
- Bei `FAIL`: Protokoll an die Bausitzung; nichts am Dauerbetrieb ändern (D8 läuft nie gegen das Live-Datenverzeichnis).
- **In der Bausitzung schon geprüft (Container):** Teil A und die Probe gegen `mcp --http` (Linux), der OAuth-Weg der Probe gegen Authelia 4.39.28 im Docker (Anmeldung, 403 bei unbekanntem Subjekt mit Hinweis, Sitzung, Ablauf → 401, Refresh → 200, Widerruf → 403); **nicht geprüft:** PowerShell-Syntax (kein `pwsh` im Container: vor dem Lauf `[System.Management.Automation.Language.Parser]::ParseFile('abnahme_D8.ps1', [ref]$null, [ref]$e); $e` – leer = gut), Tailscale (Teil B, C) und Claude Code mit Authelia.
