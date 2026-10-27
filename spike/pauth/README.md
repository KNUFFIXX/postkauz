# Spike P-AUTH (A03 S3.0, Teil T5 Phase 1)

Wegwerfcode in einem eigenen Go-Modul, nicht Teil von `./...` des Hauptmoduls und **nicht für `main`** (A03_teile.md, T5). Ergebnis: [`docs/protokolle/S30_pauth.md`](../../docs/protokolle/S30_pauth.md), Entwurf [`docs/protokolle/ADR-010_entwurf.md`](../../docs/protokolle/ADR-010_entwurf.md).

| Pfad | Inhalt |
|---|---|
| `rs/` | Prototyp `mcp --http`: Streamable HTTP zustandslos, `auth.RequireBearerToken`, JWT-Prüfung über JWKS (`go-oidc`), `aud` gegen die Resource-URL, `sub` → Schlüsselname, Host-Allowlist, PRM. Tests gegen einen Fake-AS (`httptest`, ohne Docker). |
| `cmd/probe/` | Misst einen echten Autorisierungsserver gegen die Prüfliste: PKCE, `resource`/`aud`, Token-Form, SDK-Client mit OAuth Ende zu Ende, Refresh, Negativfälle, Device-Code, URL als `client_id`. Gibt nie ein Token aus. |
| `cmd/bcrypt/` | Hash für das Dex-Testkonto. |
| `authelia/`, `dex/` | Konfigurationsvorlagen; Geheimnisse setzt `setup.sh` zur Laufzeit. |
| `setup.sh` | Test-CA, Zertifikate, Zufallsgeheimnisse, Testkonto in `$WORK` (außerhalb des Repositorys). |

## Nachstellen (nur im Container der Sitzung)

```sh
export GOTOOLCHAIN=local WORK=<scratch>/pauth
go test ./rs                               # ohne Docker
sh setup.sh && . $WORK/env                 # setzt /etc/hosts, SSL_CERT_FILE, NO_PROXY, PAUTH_PASSWORD
docker run -d --name pauth-authelia --network host -v $WORK:/work \
  authelia/authelia:4.39.28 authelia --config /work/configuration.yml
docker run -d --name pauth-dex --network host -v $WORK:$WORK:ro \
  dexidp/dex:v2.46.1 dex serve $WORK/dex.yaml
PAUTH_REQUIRE_ATJWT=1 go run ./cmd/probe -as authelia -issuer https://auth.pauth.test:9091 -tls $WORK/tls
go run ./cmd/probe -as dex -issuer https://dex.pauth.test:5556/dex -tls $WORK/tls \
  -scopes "openid profile offline_access audience:server:client_id:https://mcp.pauth.test:8443/mcp"
```

Abbau: `docker rm -f pauth-authelia pauth-dex`, `$WORK` löschen, Zeile `pauth.test` aus `/etc/hosts` entfernen.
