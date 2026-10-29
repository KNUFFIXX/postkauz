#!/bin/sh
# Abnahme D8, Teil C (docs/abnahme_D8.md): HTTP-MCP aus einer
# Claude-Code-Cloud-Sitzung über das private Netz (Tailscale).
# Läuft in der Cloud-Sitzung der Prüfsitzung (Linux, ohne root-Rechte für
# Tailscale: Userspace-Modus). Braucht tailscale und tailscaled im PATH,
# curl, openssl und python3. Nur Python-Standardbibliothek (Probe
# abnahme_D8_probe.py neben diesem Skript).
#
# Eingaben nur über die Umgebung (nie als Argument, nie ins Repository):
#   TS_AUTHKEY     ephemerer, zeitlich begrenzter Auth-Key mit Tag (Geheimnis)
#   D8_NODE        MagicDNS-Name des PostKauz-Knotens, z. B. postkauz.<tailnet>.ts.net
#   D8_MODE        key (Standard) oder oauth
#   D8_KEY_FILE    Datei mit dem Schlüssel cloud-reader (Rechte 600); im Modus
#                  oauth nur für die Gegenprobe (Schlüssel als Bearer -> 401)
#   D8_WORK        Arbeitsordner (wird neu angelegt; Standard ./d8-cloud-<zeit>)
#   D8_PRINCIPAL   Schlüsselname (Standard cloud-reader)
#   D8_TOOLS       Zahl der Tools der Rolle (Standard 12 = reader)
#   D8_OTHER_TOKEN_FILE  optional: Token eines zweiten Clients für eine
#                  andere Resource (Modus oauth, Schritt 3)
#
# Jede Prüfung ist eine Zeile "OK <Nr> …", "FAIL <Nr> …" oder "SKIP <Nr> …";
# letzte Zeile "D8 Teil C: <n> OK, <m> FAIL, <k> SKIP". Protokoll
# $D8_WORK/D8_cloud.txt; Knotenname, Tailnet-Adressen, Schlüssel und Token
# sind darin maskiert. Exit 0 nur ohne FAIL.
set -u
here=$(cd "$(dirname "$0")" && pwd)
MODE=${D8_MODE:-key}
PRINCIPAL=${D8_PRINCIPAL:-cloud-reader}
TOOLS=${D8_TOOLS:-12}
WORK=${D8_WORK:-$PWD/d8-cloud-$(date +%Y%m%d-%H%M%S)}
NOK=0 NFAIL=0 NSKIP=0

die() { echo "ABBRUCH: $*" >&2; exit 2; }
[ -n "${TS_AUTHKEY:-}" ] || die "TS_AUTHKEY fehlt (ephemerer Auth-Key als Geheimnis der Sitzung)"
[ -n "${D8_NODE:-}" ] || die "D8_NODE fehlt (MagicDNS-Name des PostKauz-Knotens)"
case "$MODE" in key|oauth) ;; *) die "D8_MODE muss key oder oauth sein";; esac
if [ "$MODE" = key ] && [ ! -f "${D8_KEY_FILE:-}" ]; then die "D8_KEY_FILE fehlt (Datei mit dem Schlüssel $PRINCIPAL)"; fi
for b in tailscale tailscaled curl openssl python3; do
  command -v "$b" >/dev/null 2>&1 || die "$b fehlt im PATH (Tailscale: statisches Paket von pkgs.tailscale.com, siehe docs/abnahme_D8.md)"
done
[ -e "$WORK" ] && die "$WORK gibt es schon; neuen D8_WORK wählen"
umask 077
mkdir -p "$WORK" || die "kann $WORK nicht anlegen"
LOG="$WORK/D8_cloud.txt"
SOCK="$WORK/tailscaled.sock"
PROXY=http://127.0.0.1:1055

mask() {
  sed -e "s/$(printf '%s' "$D8_NODE" | sed 's/[.[\*^$/]/\\&/g')/<knoten>/g" \
      -e 's/[A-Za-z0-9-]*\.[A-Za-z0-9-]*\.ts\.net/<name>.ts.net/g' \
      -e 's/100\.[0-9]\{1,3\}\.[0-9]\{1,3\}\.[0-9]\{1,3\}/<tailnet-ip>/g' \
      -e 's/fd7a:115c:a1e0:[0-9a-f:]*/<tailnet-ip6>/g' \
      -e 's/pk_key_[A-Za-z0-9_-]*/pk_key_********/g' \
      -e 's/tskey-[A-Za-z0-9_-]*/tskey-********/g' \
      -e 's/eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*/<token>/g'
}
log() { printf '%s\n' "$*" | mask | tee -a "$LOG"; }
detail() { mask | head -n 20 | sed 's/^/      /' | tee -a "$LOG"; }
ok() { NOK=$((NOK+1)); log "OK $*"; }
fail() { NFAIL=$((NFAIL+1)); log "FAIL $*"; }
skip() { NSKIP=$((NSKIP+1)); log "SKIP $*"; }

cleanup() {
  if [ -S "$SOCK" ]; then
    tailscale --socket="$SOCK" logout >/dev/null 2>&1 || true
  fi
  [ -n "${TSD_PID:-}" ] && kill "$TSD_PID" 2>/dev/null || true
  rm -f "$WORK/tok.json" "$SOCK"
}
trap cleanup EXIT INT TERM

log "Abnahme D8 Teil C, Modus $MODE, $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# ---------------------------------------------------------------- 1. Tailnet beitreten
# Userspace-Modus ohne root; ephemer (Zustand nur im Speicher), der Knoten
# verschwindet nach dem Abmelden. Programme erreichen das Tailnet über den
# HTTP-Proxy von tailscaled auf 127.0.0.1:1055.
tailscaled --tun=userspace-networking --state=mem: --socket="$SOCK" \
  --outbound-http-proxy-listen=127.0.0.1:1055 >"$WORK/tailscaled.log" 2>&1 &
TSD_PID=$!
i=0; while [ ! -S "$SOCK" ] && [ $i -lt 30 ]; do i=$((i+1)); sleep 1; done
if tailscale --socket="$SOCK" up --auth-key="$TS_AUTHKEY" --hostname="d8-cloud-$$" --accept-dns=false >"$WORK/up.log" 2>&1; then
  ok "C1.1 Cloud-Knoten tritt dem Tailnet bei (Userspace, ephemer)"
else
  fail "C1.1 Cloud-Knoten tritt dem Tailnet bei"; detail <"$WORK/up.log"; log "D8 Teil C: $NOK OK, $NFAIL FAIL, $NSKIP SKIP"; exit 1
fi
unset TS_AUTHKEY
st=$(tailscale --socket="$SOCK" status 2>&1)
peers=$(printf '%s\n' "$st" | grep -v "d8-cloud-$$" | grep -c '^100\.\|^fd7a' || true)
if printf '%s\n' "$st" | grep -q "$(printf '%s' "$D8_NODE" | cut -d. -f1)" && [ "$peers" -eq 1 ]; then
  ok "C1.2 tailscale status: nur der PostKauz-Knoten sichtbar (ACL)"
else
  fail "C1.2 tailscale status: erwartet genau den PostKauz-Knoten, sichtbar: $peers"
fi
printf '%s\n' "$st" | detail

# ---------------------------------------------------------------- 2. Metadaten und Zertifikat
prm=$(curl -sS --max-time 30 --proxy "$PROXY" "https://$D8_NODE/.well-known/oauth-protected-resource" 2>&1)
if printf '%s' "$prm" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d["resource"]' 2>/dev/null; then
  ok "C2.1 Metadaten der Resource mit resource"
  if [ "$MODE" = oauth ]; then
    printf '%s' "$prm" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert len(d["authorization_servers"])==1' 2>/dev/null \
      && ok "C2.2 Metadaten nennen authorization_servers" || fail "C2.2 Metadaten nennen authorization_servers"
  else
    skip "C2.2 authorization_servers nur im Modus oauth"
  fi
else
  fail "C2.1 Metadaten der Resource"; skip "C2.2 (ohne Metadaten)"
fi
printf '%s\n' "$prm" | detail
cert=$(echo | openssl s_client -proxy 127.0.0.1:1055 -connect "$D8_NODE:443" -servername "$D8_NODE" -verify_return_error 2>"$WORK/sclient.err")
if printf '%s' "$cert" | grep -q 'Verify return code: 0 (ok)'; then
  ok "C2.3 Zertifikatskette gültig"
else
  fail "C2.3 Zertifikatskette gültig"; detail <"$WORK/sclient.err"
fi
if printf '%s' "$cert" | openssl x509 -noout -checkend 1209600 >/dev/null 2>&1; then
  ok "C2.4 Zertifikat läuft frühestens in 14 Tagen ab"
else
  fail "C2.4 Zertifikat läuft frühestens in 14 Tagen ab"
fi
printf '%s' "$cert" | openssl x509 -noout -issuer -enddate 2>&1 | detail

# ---------------------------------------------------------------- 3. bis 5. Probe
export HTTPS_PROXY=$PROXY https_proxy=$PROXY
unset NO_PROXY no_proxy
run_probe() {
  python3 -I "$here/abnahme_D8_probe.py" "$@" 2>&1 | mask | tee -a "$LOG" | tee "$WORK/probe.out"
  n=$(grep -c '^OK ' "$WORK/probe.out"); f=$(grep -c '^FAIL ' "$WORK/probe.out"); s=$(grep -c '^SKIP ' "$WORK/probe.out")
  NOK=$((NOK+n)); NFAIL=$((NFAIL+f)); NSKIP=$((NSKIP+s))
}
if [ "$MODE" = key ]; then
  run_probe key --url "https://$D8_NODE/mcp" --key-file "$D8_KEY_FILE" --nr C3 --principal "$PRINCIPAL" --expect-tools "$TOOLS"
else
  set -- oauth --url "https://$D8_NODE/mcp" --nr C4 --principal "$PRINCIPAL" --expect-tools "$TOOLS" --paste --token-file "$WORK/tok.json" --wait-expiry
  [ -f "${D8_KEY_FILE:-}" ] && set -- "$@" --key-file "$D8_KEY_FILE"
  [ -f "${D8_OTHER_TOKEN_FILE:-}" ] && set -- "$@" --other-token-file "$D8_OTHER_TOKEN_FILE"
  run_probe "$@"
fi

log ""
log "[Du] C5 Jetzt auf dem PostKauz-Knoten den Schlüssel widerrufen: postkauz key revoke $PRINCIPAL"
log "     Danach hier Enter drücken (leer + Enter überspringt mit \"s\")."
read -r ans || ans=s
if [ "$ans" = s ]; then
  skip "C5 Widerruf nicht geprüft"
else
  sleep 31  # Zwischenspeicher der Anmeldung (30 s)
  if [ "$MODE" = key ]; then
    run_probe revoked --url "https://$D8_NODE/mcp" --key-file "$D8_KEY_FILE" --nr C5
  elif [ -f "$WORK/tok.json" ]; then
    run_probe revoked --url "https://$D8_NODE/mcp" --token-file "$WORK/tok.json" --nr C5
  else
    skip "C5 kein Refresh-Token aus Schritt 4"
  fi
fi

# ---------------------------------------------------------------- 6. Abbau
tailscale --socket="$SOCK" logout >/dev/null 2>&1 && ok "C6.1 Cloud-Knoten hat das Tailnet verlassen (logout, ephemer)" || fail "C6.1 logout"
kill "$TSD_PID" 2>/dev/null; TSD_PID=
rm -f "$WORK/tok.json"
log "     Danach von Hand: Auth-Key in der Tailscale-Konsole widerrufen (falls nicht abgelaufen), Testkonto im Autorisierungsserver deaktivieren."
log "D8 Teil C: $NOK OK, $NFAIL FAIL, $NSKIP SKIP"
[ "$NFAIL" -eq 0 ]
