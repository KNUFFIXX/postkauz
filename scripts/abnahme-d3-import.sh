#!/bin/sh
# Testbestand für Abnahme D3.4 (docs/abnahme_D3.md): ein synthetisches
# Thunderbird-Profil (Standardkorpus, Samen 42, wie `gencorpus -thunderbird`)
# als Zip, dazu die Probe des Imports gegen eine Kopie der D2-Datenbank
# (Konto demo, derselbe Korpus über Dovecot gespiegelt): erster Lauf,
# zweiter Lauf, dritter Lauf mit --rescan. Die Ausgabe ist das Protokoll.
#
#   sh scripts/abnahme-d3-import.sh [ziel.zip]   # Standard: dist/abnahme-d3-import.zip
#
# Voraussetzungen (werden geprüft): Go 1.27 (GOTOOLCHAIN=local), zip, unzip,
# sha256sum und dist/abnahme-d2-daten.zip (scripts/abnahme-d2-db.sh). Nur
# synthetische Testdaten, kein Server, kein Passwort.
set -eu

for tool in go zip unzip sha256sum; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "abnahme-d3-import: $tool is missing" >&2
		exit 1
	fi
done
D2=dist/abnahme-d2-daten.zip
[ -f "$D2" ] || { echo "abnahme-d3-import: $D2 is missing (sh scripts/abnahme-d2-db.sh)" >&2; exit 1; }

OUT=${1:-dist/abnahme-d3-import.zip}
case "$OUT" in /*) ;; *) OUT="$(pwd)/$OUT" ;; esac
export GOTOOLCHAIN=local CGO_ENABLED=0

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/postkauz"
go build -trimpath -o "$BIN" ./cmd/postkauz
go run ./internal/corpus/cmd/gencorpus -out "$WORK/corpus" -thunderbird "$WORK/Thunderbird" | tail -1

mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
# Feste Zeitstempel und Reihenfolge: dieselbe Zip-Datei bei jedem Lauf.
find "$WORK/Thunderbird" -exec touch -h -t 202610160000.00 {} +
(cd "$WORK" && find Thunderbird | LC_ALL=C sort | zip -q -X -D -@ "$OUT")

export POSTKAUZ_DATA_DIR="$WORK/data" POSTKAUZ_CONFIG="$WORK/keine-config.toml"
unzip -q "$D2" -d "$WORK/data"
echo "== D2 data: $(sha256sum "$D2" | cut -d' ' -f1)"
echo "== before: search Rechnung"
"$BIN" search "Rechnung" -a demo --limit 1 | tail -2
for run in 1 2 3; do
	extra=""
	[ "$run" = 3 ] && extra="--rescan"
	echo "== run $run: import mbox Thunderbird -a demo --mailbox Archive $extra"
	"$BIN" import mbox "$WORK/Thunderbird" -a demo --mailbox Archive --quiet $extra
done
echo "== after: search Rechnung, in:Archive/Entwürfe"
"$BIN" search "Rechnung" -a demo --limit 1 | tail -2
"$BIN" search "in:Archive/Entwürfe" -a demo --limit 10
echo "== zip"
ls -l "$OUT" | awk '{print $5, $NF}'
sha256sum "$OUT"
