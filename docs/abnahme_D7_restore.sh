#!/bin/sh
# Abnahme D7, Schritt 5 im Linux-Container (A03 Teil D, D7.5; Teil T3):
# Wiederherstellung einer unter Windows erzeugten Sicherung auf einer
# sauberen Maschine und Vergleich mit dem Windows-Stand (Akzeptanzkriterium 3).
#
# Eingabe: D7_sicherung.tar aus abnahme_D7.ps1 (Schritt 5.13; enthält den
# Ordner sicherung/ und D7_reference.json) und das Linux-Programm aus dist/.
# Ohne Go, ohne Docker, ohne Mailserver. Schreibt nur unter <arbeitsordner>.
#
#   sh docs/abnahme_D7_restore.sh <postkauz-linux-amd64> <D7_sicherung.tar> <arbeitsordner>
#
# <arbeitsordner> muss neu sein. Jede Prüfung ist eine Zeile OK/FAIL mit
# Nummer; letzte Zeile "D7 Linux: <n> OK, <m> FAIL". Exit 0 nur ohne FAIL.
# Braucht python3 (nur zum Lesen von JSON).
set -u

exe=${1:?usage: abnahme_D7_restore.sh <postkauz> <D7_sicherung.tar> <workdir>}
pkg=${2:?usage: abnahme_D7_restore.sh <postkauz> <D7_sicherung.tar> <workdir>}
work=${3:?usage: abnahme_D7_restore.sh <postkauz> <D7_sicherung.tar> <workdir>}

if [ -e "$work" ]; then echo "ABBRUCH: $work existiert schon; bitte einen neuen Arbeitsordner nennen"; exit 2; fi
case "$work" in
  "$HOME/.local/share/postkauz"*|"$HOME/.config/postkauz"*) echo "ABBRUCH: Arbeitsordner im Standardpfad"; exit 2 ;;
esac
mkdir -p "$work" || exit 2
log="$work/D7_linux.txt"
ok=0; fail=0
say() { printf '%s\n' "$*" | tee -a "$log"; }
check() { # check <nr> <beschreibung> <0|1> [detail]
  if [ "$3" = 0 ]; then ok=$((ok+1)); say "OK $1 $2"; else fail=$((fail+1)); say "FAIL $1 $2"; fi
  [ -n "${4:-}" ] && printf '%s\n' "$4" | head -40 | sed 's/^/      /' | tee -a "$log" >/dev/null
  return 0
}

cfg="$work/config.toml"
printf "# Abnahme D7 Linux\n[operator]\nname = 'D7-Linux'\n" > "$cfg"
data="$work/wiederhergestellt"
pk() { "$exe" --config "$cfg" "$@"; }
pkd() { "$exe" --data-dir "$data" --config "$cfg" "$@"; }

say "Abnahme D7, Restore im Linux-Container, $(date -u +%Y-%m-%dT%H:%M:%SZ)"
out=$("$exe" version 2>&1); check 0.1 "version" $? "$out
sha256 $(sha256sum "$exe" | cut -d' ' -f1)"
mkdir -p "$work/paket" && tar -xf "$pkg" -C "$work/paket"
[ -f "$work/paket/sicherung/manifest.json" ] && [ -f "$work/paket/D7_reference.json" ]
check 0.2 "Paket entpackt: sicherung/manifest.json und D7_reference.json" $? "$(ls "$work/paket")"
ref="$work/paket/D7_reference.json"

out=$(pk restore --from "$work/paket/sicherung" --data-dir "$data" --dry-run 2>&1)
echo "$out" | grep -q 'is complete and unchanged'; check L.1 "restore --dry-run: Sicherung vollständig und unverändert" $? "$out"
out=$(pk restore --from "$work/paket/sicherung" --data-dir "$data" 2>&1); rc=$?
[ $rc = 0 ] && echo "$out" | grep -q '^restored:' && echo "$out" | grep -q 'identity: *new data directory id' && echo "$out" | grep -Eq '^ +result: +ok *$'
check L.2 "restore unter Linux: restored, neue Identität, doctor --blobs=all result ok" $? "$out"

# Suche und IDs gegen die Referenz.
res=$(python3 -I - "$ref" "$exe" "$data" "$cfg" <<'EOF'
import json, subprocess, sys
ref = json.load(open(sys.argv[1], encoding="utf-8-sig"))
exe, data, cfg = sys.argv[2:5]
bad = []
for q, want in ref["searches"].items():
    want = sorted(int(x) for x in (want if isinstance(want, list) else ([] if want is None else [want])))
    p = subprocess.run([exe, "--data-dir", data, "--config", cfg, "search", q, "--json", "--limit", "100"], capture_output=True, text=True)
    got = sorted(json.loads(l)["id"] for l in p.stdout.splitlines() if l.startswith("{") and '"id"' in l and "id" in json.loads(l))
    print(f"{q}: Windows {want}, Linux {got}")
    if p.returncode != 0 or got != want:
        bad.append(q)
for mid, want in ref["messages"].items():
    p = subprocess.run([exe, "--data-dir", data, "--config", cfg, "show", mid, "-a", "demo", "--json"], capture_output=True, text=True)
    got = json.loads(p.stdout).get("id") if p.returncode == 0 else None
    print(f"{mid}: Windows {want}, Linux {got}")
    if got != want:
        bad.append(mid)
print("BAD" if bad else "GOOD")
EOF
)
echo "$res" | tail -1 | grep -q GOOD; check L.3 "Suche und Nachrichten-IDs wie unter Windows (Akzeptanzkriterium 3)" $? "$res"

want=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1], encoding="utf-8-sig"))["blobs_verify"])' "$ref")
out=$(pkd doctor --blobs=all 2>&1)
got=$(echo "$out" | sed -n 's/^ *verify: *//p')
[ "$got" = "$want" ] && echo "$out" | grep -Eq '^ +result: +ok *$'
check L.4 "doctor --blobs=all wie unter Windows ($want)" $? "$out"
out=$(pkd audit verify --json 2>&1)
echo "$out" | python3 -I -c 'import json,sys; j=json.loads(sys.stdin.read()); sys.exit(0 if j["status"]=="ok" and not j["problems"] else 1)'
check L.5 "audit verify der Kopie -> ok, ohne Befund" $? "$out"

say ""
say "D7 Linux: $ok OK, $fail FAIL"
[ $fail = 0 ]
