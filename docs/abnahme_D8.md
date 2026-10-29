# Abnahme D8: HTTP-MCP mit Schlüssel und OAuth über das private Netz

Stand: 29.10.2026 (A03 Teil T5 Phase 2, Build `0.3.0-alpha.8`). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Teil T5, Haltepunkt „Abnahme D8“, und zu [auftraege/A03_s3.md](../auftraege/A03_s3.md), Teil D, Abschnitt D8, **vorläufig mit einem PC des Betreibers als Knoten im privaten Netz**, bis über einen VPS entschieden ist. Skripte: [abnahme_D8.ps1](abnahme_D8.ps1) (Windows, Teile A, B, D), [abnahme_D8_cloud.sh](abnahme_D8_cloud.sh) (Cloud-Sitzung, Teil C), gemeinsame Probe [abnahme_D8_probe.py](abnahme_D8_probe.py) (nur Python-Standardbibliothek). Ausgeführt von der Prüfsitzung; der Betreiber übernimmt nur Tailnet, Auth-Key und die Anmeldung im Browser.

## Zweck

T5 bringt `postkauz mcp --http`: dieselben MCP-Werkzeuge wie über stdio, aber über HTTP, zustandslos, für Agenten außerhalb des Rechners (zuerst eine Claude-Code-Cloud-Sitzung im Tailnet). Zwei Arten der Anmeldung ([docs/betrieb.md](betrieb.md), Abschnitt 4):

- **Modus `key`:** Der Client schickt einen PostKauz-Schlüssel als `Authorization: Bearer …`. Nur Loopback und Tailnet.
- **Modus `oauth`:** Der Client meldet sich bei einem eigenen Anmeldedienst (Authelia) an; PostKauz prüft nur dessen Token und ordnet das Subjekt einem Schlüssel zu.

D8 prüft beides: lokal ohne Netz (Teil A), über Tailscale vom PC aus (Teil B), aus der Cloud-Sitzung (Teil C) und im Modus `oauth` mit Anmeldung im Browser (Teil D).

## Grundregeln

- **Nie die Live-Datenbank** (Rahmen 5): `-DataDir` und `-Config` sind Pflicht; das Skript legt beide neu an und bricht ohne Änderung ab, wenn sie oder `-WorkDir` im Standard-Datenverzeichnis, im Standard-Konfigurationsordner oder in einem mit `-LivePath` genannten Pfad liegen (Markerdatei gegen Junction und `subst`). Kein Mailkonto, kein Passwort: `mcp --http` kennt keins.
- **Port:** `-Port` (Standard 18787) auf `127.0.0.1`, nie der Standardport 8787 (ein laufender Dienst bleibt ungestört).
- **Geheimnisse:** Schlüssel liegen nur während des Laufs in `<WorkDir>\lauf-…\keys\` und werden danach gelöscht und widerrufen. Der Auth-Key von Tailscale und der Schlüssel für Teil C kommen als **Geheimnis der Cloud-Sitzung**, nie ins Repository, nie in ein Protokoll.
- **Protokolle** `D8_windows.txt` (Windows) und `D8_cloud.txt` (Cloud-Sitzung): exklusiv neu angelegt; Schlüssel (`pk_key_********`), Token (`<token>`), Auth-Keys (`tskey-********`), Pfade (`<DataDir>` …), der Knotenname (`<knoten>`) und Tailnet-Adressen (`<tailnet-ip>`) sind maskiert. Jede Zeile `OK`, `FAIL` oder `SKIP` mit Nummer; letzte Zeile `D8: …` bzw. `D8 Teil C: …`. Exit 0 nur ohne `FAIL`, Exit 2 bei Abbruch vor dem ersten Schritt.
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
| A0.1 | `version` | `0.3.0-alpha.8` |
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
| B2.1 | `[Du]` `tailscale serve --bg <Port>` | Exit 0 |
| B2.2 | `https://<knoten>/.well-known/oauth-protected-resource/mcp` vom PC | 200, `resource` = `https://<knoten>/mcp`, Zertifikat gültig |
| B3.x | Probe `key` über `https://<knoten>/mcp` | wie A3 |
| B4 | `[Du]` Server läuft, bis du Enter drückst: jetzt Teil C | – |
| B4.1 | `key revoke cloud-reader-c` (nach der ersten Enter-Taste) | Exit 0 |
| B5.1 | `[Du]` `tailscale serve --https=443 off` | Exit 0 |

## Teil C: aus der Cloud-Sitzung (Linux)

Während Teil B auf Enter wartet. Der Schlüssel `cloud-reader-c` (Datei aus B4, Pfad steht im Protokoll) und der Auth-Key kommen als Geheimnisse in die Sitzung:

```bash
TS_AUTHKEY=<geheim> D8_NODE=pc.<tailnet>.ts.net D8_KEY_FILE=/pfad/cloud-reader-c.txt D8_PRINCIPAL=cloud-reader-c sh docs/abnahme_D8_cloud.sh
```

| Nr | Schritt | Erwartet |
|---|---|---|
| C1.1 | `tailscaled` im Userspace-Modus, `tailscale up` mit dem Auth-Key | beigetreten (ephemer) |
| C1.2 | `tailscale status` | nur der PostKauz-Knoten sichtbar (ACL) |
| C2.1–C2.2 | `curl https://<knoten>/.well-known/oauth-protected-resource` | JSON mit `resource` (im Modus `oauth` auch `authorization_servers`) |
| C2.3–C2.4 | `openssl s_client` | Kette gültig, Ablauf in mehr als 14 Tagen (Akzeptanzkriterium 5) |
| C3.x | Probe `key` | wie A3 (401 ohne Bearer, 405, 12 Tools, Hülle, Audit mit `transport http`) |
| C5 | `[Du]` in Teil B Enter (Widerruf), dann hier Enter | 403 `denied (key_revoked)` |
| C6.1 | `tailscale logout` | Knoten verlässt das Tailnet |

Danach von Hand: Auth-Key in der Tailscale-Konsole widerrufen, falls noch gültig. Im Modus `oauth` (`D8_MODE=oauth`) läuft statt C3 die Probe `oauth` mit `--paste` (die Rückleitungs-URL aus dem Browser einfügen); die Messung des OAuth-Wegs gehört aber an den PC (Teil D, ADR-010 Folgen: aus einer Cloud-Sitzung bleibt OAuth unsicher, Risiko 3).

## Teil D: Modus oauth mit Anmeldung im Browser (Windows)

Wie Teil B mit zusätzlich `-Issuer https://<authelia>` (Authelia nach betrieb.md 4.6). Erster Lauf ohne `-Subject`: Nach der Anmeldung antwortet PostKauz 403 und das Protokoll nennt das Subjekt (`D3.s0`). Zweiter Lauf (neue Pfade) mit `-Subject "<sub>"`:

| Nr | Schritt | Erwartet |
|---|---|---|
| D1.1–D1.3 | Schlüssel `cloud-oauth`, Server im Modus `oauth`, `[Du]` `tailscale serve` | läuft |
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

Danach von Hand ([Du]): Claude Code am PC nach betrieb.md 4.6 Schritt 6 anbinden (`/mcp` → anmelden → `tools/list`), Ergebnis ins Protokoll; Testkonto in Authelia deaktivieren.

## Bewertung

- **Bestanden**, wenn Teil A, B und C ohne `FAIL` laufen (D8 vorläufig, Modus `key`). Teil D gehört dazu, sobald Authelia im Tailnet läuft; bis dahin bleibt D8 „vorläufig ohne OAuth“.
- Bei `FAIL`: Protokoll an die Bausitzung; nichts am Dauerbetrieb ändern (D8 läuft nie gegen das Live-Datenverzeichnis).
- **In der Bausitzung schon geprüft (Container):** Teil A und die Probe gegen `mcp --http` (Linux), der OAuth-Weg der Probe gegen Authelia 4.39.28 im Docker (Anmeldung, 403 bei unbekanntem Subjekt mit Hinweis, Sitzung, Ablauf → 401, Refresh → 200, Widerruf → 403); **nicht geprüft:** PowerShell-Syntax (kein `pwsh` im Container: vor dem Lauf `[System.Management.Automation.Language.Parser]::ParseFile('abnahme_D8.ps1', [ref]$null, [ref]$e); $e` – leer = gut), Tailscale (Teil B, C) und Claude Code mit Authelia.
