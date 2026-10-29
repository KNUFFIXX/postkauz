#!/bin/sh
# Abnahme D8, Teil C (docs/abnahme_D8.md): HTTP-MCP aus einer
# Claude-Code-Cloud-Sitzung über das private Netz (Tailscale).
# Läuft in der Cloud-Sitzung der Prüfsitzung (Linux, ohne root-Rechte für
# Tailscale: Userspace-Modus). Braucht tailscale und tailscaled im PATH,
# curl, openssl und python3. Nur Python-Standardbibliothek (Probe
# abnahme_D8_probe.py neben diesem Skript).
#
# Eingaben nur über die Umgebung (nie als Argument, nie ins Repository):
#   TS_AUTHKEY     ephemerer, zeitlich begrenzter Auth-Key mit Tag (Geheimnis);
#                  geht nur über eine Datei mit Rechten 600 an tailscale up
#                  (--auth-key=file:…), nie in eine Prozessliste
#   D8_NODE        MagicDNS-Name des PostKauz-Knotens, z. B. postkauz.<tailnet>.ts.net
#   D8_MODE        key (Standard) oder oauth
#   D8_KEY_FILE    Datei mit dem Schlüssel cloud-reader (Rechte 600); im Modus
#                  oauth nur für die Gegenprobe (Schlüssel als Bearer -> 401)
#   D8_WORK        Arbeitsordner, wird neu angelegt; Standard: ein neuer
#                  Ordner unter $TMPDIR bzw. /tmp (mktemp), nie im Repo-Klon
#   D8_PRINCIPAL   Schlüsselname (Standard cloud-reader)
#   D8_TOOLS       Zahl der Tools der Rolle (Standard 12 = reader)
#   D8_OTHER_TOKEN_FILE  optional: Token eines zweiten Clients für eine
#                  andere Resource (Modus oauth, Schritt 3)
#   D8_WAIT        Sekunden, die C5 (und im Modus oauth die Anmeldung) auf
#                  das Weiter-Signal warten (Standard 1800)
#
# Ohne Terminal (Bash-Werkzeug einer Cloud-Sitzung): Skript im Hintergrund
# starten; es wartet nie auf eine Eingabe, sondern auf Dateien im
# Arbeitsordner (Pfad steht in der ersten Zeile des Protokolls):
#   go         C5: nach dem Widerruf auf dem PostKauz-Knoten anlegen
#              (Inhalt "s" überspringt C5)
#   callback   Modus oauth: Rückleitungs-URL aus dem Browser, eine Zeile mit
#              Zeilenende (erst in callback.tmp schreiben, dann umbenennen)
#   login-url.txt  Modus oauth: Anmelde-URL (ungefiltert, Rechte 600; mit
#              Terminal zusätzlich auf /dev/tty, nie ins Protokoll)
#
# Jede Prüfung ist eine Zeile "OK <Nr> …", "FAIL <Nr> …" oder "SKIP <Nr> …";
# letzte Zeile "D8 Teil C: <n> OK, <m> FAIL, <k> SKIP". Protokoll
# $D8_WORK/D8_cloud.txt; Knotenname, Tailnet-Namen und -Adressen, Schlüssel
# und Token sind darin maskiert; vom Tailnet steht nur die Zahl der Knoten
# darin (keine Namen, kein Login). Rohe Ausgaben von tailscaled, tailscale up
# und openssl löscht das Skript am Ende. Exit 0 nur ohne FAIL.
set -u
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/.." && pwd)
MODE=${D8_MODE:-key}
PRINCIPAL=${D8_PRINCIPAL:-cloud-reader}
TOOLS=${D8_TOOLS:-12}
WAIT=${D8_WAIT:-1800}
NOK=0 NFAIL=0 NSKIP=0

die() { echo "ABBRUCH: $*" >&2; exit 2; }
[ -n "${TS_AUTHKEY:-}" ] || die "TS_AUTHKEY fehlt (ephemerer Auth-Key als Geheimnis der Sitzung)"
[ -n "${D8_NODE:-}" ] || die "D8_NODE fehlt (MagicDNS-Name des PostKauz-Knotens)"
case "$MODE" in key|oauth) ;; *) die "D8_MODE muss key oder oauth sein";; esac
case "$WAIT" in ''|*[!0-9]*) die "D8_WAIT muss eine Zahl (Sekunden) sein";; esac
if [ "$MODE" = key ] && [ ! -f "${D8_KEY_FILE:-}" ]; then die "D8_KEY_FILE fehlt (Datei mit dem Schlüssel $PRINCIPAL)"; fi
for b in tailscale tailscaled curl openssl python3 mktemp; do
  command -v "$b" >/dev/null 2>&1 || die "$b fehlt im PATH (Tailscale: statisches Paket von pkgs.tailscale.com, siehe docs/abnahme_D8.md)"
done
umask 077
if [ -n "${D8_WORK:-}" ]; then
  WORK=$D8_WORK
  [ -e "$WORK" ] && die "$WORK gibt es schon; neuen D8_WORK wählen"
  mkdir -p "$WORK" || die "kann $WORK nicht anlegen"
else
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/d8-cloud-XXXXXX") || die "mktemp -d fehlgeschlagen"
fi
WORK=$(cd "$WORK" && pwd)
# Nie im Repo-Klon: dort lägen rohe Tailnet-Daten neben dem Quelltext.
case "$WORK/" in "$repo"/*) rmdir "$WORK" 2>/dev/null; die "D8_WORK liegt im Repository ($repo); einen Ordner außerhalb wählen (Standard: mktemp)";; esac
LOG="$WORK/D8_cloud.txt"
SOCK="$WORK/tailscaled.sock"
PROXY=http://127.0.0.1:1055

# Auth-Key sofort in eine Datei (Rechte 600 durch umask 077; printf ist in
# sh eingebaut, kein eigener Prozess) und aus der Umgebung entfernen, bevor
# irgendein Kindprozess startet (sonst stünde er in /proc/<pid>/environ).
AUTHKEY_FILE="$WORK/authkey"
printf '%s' "$TS_AUTHKEY" >"$AUTHKEY_FILE" || die "kann $AUTHKEY_FILE nicht schreiben"
unset TS_AUTHKEY

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
  rm -f "$AUTHKEY_FILE"
  if [ -S "$SOCK" ]; then
    tailscale --socket="$SOCK" logout >/dev/null 2>&1 || true
  fi
  [ -n "${TSD_PID:-}" ] && kill "$TSD_PID" 2>/dev/null || true
  # Nur das maskierte Protokoll bleibt; rohe Ausgaben (Peers, Endpunkte),
  # Token, Anmelde- und Rückleitungs-URL kommen weg.
  rm -f "$WORK/tok.json" "$SOCK" "$WORK/tailscaled.log" "$WORK/up.log" "$WORK/sclient.err" \
        "$WORK/probe.out" "$WORK/probe.rc" "$WORK/status.json" "$WORK/login-url.txt" "$WORK/callback" "$WORK/callback.tmp" "$WORK/go"
}
trap cleanup EXIT
trap 'exit 2' INT TERM

log "Abnahme D8 Teil C, Modus $MODE, $(date -u +%Y-%m-%dT%H:%M:%SZ)"
log "Arbeitsordner (Signale go und callback hier anlegen): $WORK"

# ---------------------------------------------------------------- 1. Tailnet beitreten
# Userspace-Modus ohne root; ephemer (Zustand nur im Speicher), der Knoten
# verschwindet nach dem Abmelden. Programme erreichen das Tailnet über den
# HTTP-Proxy von tailscaled auf 127.0.0.1:1055.
tailscaled --tun=userspace-networking --state=mem: --socket="$SOCK" \
  --outbound-http-proxy-listen=127.0.0.1:1055 >"$WORK/tailscaled.log" 2>&1 &
TSD_PID=$!
i=0; while [ ! -S "$SOCK" ] && [ $i -lt 30 ]; do i=$((i+1)); sleep 1; done
# --auth-key=file:<pfad> liest den Schlüssel aus der Datei (tailscale up,
# cmd/tailscale/cli/up.go, mindestens seit v1.30).
if tailscale --socket="$SOCK" up --auth-key="file:$AUTHKEY_FILE" --hostname="d8-cloud-$$" --accept-dns=false >"$WORK/up.log" 2>&1; then
  ok "C1.1 Cloud-Knoten tritt dem Tailnet bei (Userspace, ephemer)"
  rm -f "$AUTHKEY_FILE"
else
  rm -f "$AUTHKEY_FILE"
  fail "C1.1 Cloud-Knoten tritt dem Tailnet bei"; detail <"$WORK/up.log"; log "D8 Teil C: $NOK OK, $NFAIL FAIL, $NSKIP SKIP"; exit 1
fi
# Vom Status nur Zahlen ins Protokoll: wie viele andere Knoten sichtbar sind
# und ob der PostKauz-Knoten darunter ist (keine Namen, kein Login).
tailscale --socket="$SOCK" status --json >"$WORK/status.json" 2>/dev/null
counts=$(D8_NODE="$D8_NODE" python3 -I -c '
import json, os, sys
try:
    d = json.load(open(sys.argv[1], encoding="utf-8"))
except (OSError, ValueError):
    print("x x"); sys.exit(0)
peers = list((d.get("Peer") or {}).values())
node = os.environ["D8_NODE"].rstrip(".").lower()
hit = any((p.get("DNSName") or "").rstrip(".").lower() == node for p in peers)
print(len(peers), "ja" if hit else "nein")
' "$WORK/status.json")
rm -f "$WORK/status.json"
peers=${counts% *}; hit=${counts#* }
if [ "$peers" = 1 ] && [ "$hit" = ja ]; then
  ok "C1.2 tailscale status: nur der PostKauz-Knoten sichtbar (ACL)"
else
  fail "C1.2 tailscale status: erwartet genau den PostKauz-Knoten"
fi
log "      andere Knoten sichtbar: $peers, PostKauz-Knoten darunter: $hit"

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
# Die Probe muss sauber enden: Schlusszeile "probe: <n> OK, <m> FAIL, <k> SKIP"
# passend zu ihren Zeilen und Exit 0 (ohne FAIL) bzw. 1 (mit FAIL); sonst
# (Syntax- oder Importfehler, Absturz, Abbruch) FAIL <Nr>.ende. Den Exit-Status
# der Probe hinter der Pipeline (mask, tee) liefert eine Statusdatei: /bin/sh
# (dash) kennt weder pipefail noch PIPESTATUS.
run_probe() {
  nr=D8; prev=
  for x in "$@"; do [ "$prev" = --nr ] && nr=$x; prev=$x; done
  rm -f "$WORK/probe.rc"
  { python3 -I "$here/abnahme_D8_probe.py" "$@" 2>&1; echo $? >"$WORK/probe.rc"; } | mask | tee -a "$LOG" | tee "$WORK/probe.out"
  rc=$(cat "$WORK/probe.rc" 2>/dev/null); rm -f "$WORK/probe.rc"
  n=$(grep -c '^OK ' "$WORK/probe.out"); f=$(grep -c '^FAIL ' "$WORK/probe.out"); s=$(grep -c '^SKIP ' "$WORK/probe.out")
  NOK=$((NOK+n)); NFAIL=$((NFAIL+f)); NSKIP=$((NSKIP+s))
  last=$(grep '^probe: ' "$WORK/probe.out" | tail -n 1)
  want="probe: $n OK, $f FAIL, $s SKIP"
  wantrc=0; [ "$f" -gt 0 ] && wantrc=1
  if [ "$last" != "$want" ] || [ "${rc:-x}" != "$wantrc" ]; then
    fail "$nr.ende Probe endet sauber (Schlusszeile und Exit-Code)"
    log "      Schlusszeile: ${last:-keine}; erwartet: $want, Exit $wantrc; Exit: ${rc:-unbekannt}"
  fi
}
if [ "$MODE" = key ]; then
  run_probe key --url "https://$D8_NODE/mcp" --key-file "$D8_KEY_FILE" --nr C3 --principal "$PRINCIPAL" --expect-tools "$TOOLS"
else
  # Anmelde-URL ungefiltert nur in login-url.txt und auf /dev/tty (mask
  # machte sie unbrauchbar); die Rückleitungs-URL kommt über die Datei
  # callback, nicht über die Standardeingabe.
  set -- oauth --url "https://$D8_NODE/mcp" --nr C4 --principal "$PRINCIPAL" --expect-tools "$TOOLS" \
    --login-url-file "$WORK/login-url.txt" --paste-file "$WORK/callback" --login-timeout "$WAIT" \
    --token-file "$WORK/tok.json" --wait-expiry
  [ -f "${D8_KEY_FILE:-}" ] && set -- "$@" --key-file "$D8_KEY_FILE"
  [ -f "${D8_OTHER_TOKEN_FILE:-}" ] && set -- "$@" --other-token-file "$D8_OTHER_TOKEN_FILE"
  run_probe "$@"
fi

# ---------------------------------------------------------------- C5 Widerruf
# Weiter-Signal über die Datei go (eine Cloud-Sitzung hat keine Eingabe):
# Die Prüfsitzung legt sie an, sobald der Schlüssel auf dem PostKauz-Knoten
# widerrufen ist (Teil B, B4.1). Inhalt "s" überspringt C5.
log ""
log "[Du] C5 Jetzt auf dem PostKauz-Knoten den Schlüssel widerrufen (Teil B: Enter bei B4), dann im Arbeitsordner die Datei go anlegen."
log "     Warte höchstens $WAIT s auf $WORK/go (Inhalt \"s\" überspringt C5)."
i=0; while [ ! -f "$WORK/go" ] && [ $i -lt "$WAIT" ]; do i=$((i+5)); sleep 5; done
if [ ! -f "$WORK/go" ]; then
  skip "C5 Widerruf nicht geprüft (kein Signal go innerhalb von $WAIT s)"
elif [ "$(head -c 1 "$WORK/go")" = s ]; then
  skip "C5 Widerruf nicht geprüft (Signal go mit \"s\")"
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
