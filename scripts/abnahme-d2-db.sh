#!/bin/sh
# Erzeugt die vorbereitete Datenbank für Abnahme D2 (docs/abnahme_D2.md):
# den synthetischen Standardkorpus (1.000 Nachrichten, Samen 42), über den
# Dovecot-Testserver gespiegelt, mit Suchindex und extrahierten Anhängen,
# als Zip-Datei im Format des Datenverzeichnisses (postkauz.db + blobs/).
#
#   make testmail-up          # Dovecot auf 127.0.0.1:11143 (Demo-Modus: jeder Nutzer, Passwort "pass")
#   sh scripts/abnahme-d2-db.sh [ziel.zip]      # Standard: dist/abnahme-d2-daten.zip
#
# Nur Testdaten: synthetischer Korpus, Testserver auf diesem Rechner, kein
# Passwort im Ergebnis (Konto mit --password-from-env). Je Lauf ein frischer
# Nutzer auf dem Testserver, damit andere Tests nicht gestört werden.
set -eu

OUT=${1:-dist/abnahme-d2-daten.zip}
case "$OUT" in /*) ;; *) OUT="$(pwd)/$OUT" ;; esac
export GOTOOLCHAIN=local CGO_ENABLED=0

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
DATA="$WORK/data"
USER="abnahme-d2-$(date +%s)"
BIN="$WORK/postkauz"

go build -trimpath -o "$BIN" ./cmd/postkauz
go run ./internal/corpus/cmd/corpusload -imap 127.0.0.1:11143 -user "$USER" -password pass

export POSTKAUZ_DATA_DIR="$DATA" POSTKAUZ_CONFIG="$WORK/keine-config.toml" POSTKAUZ_PASSWORD_DEMO=pass
"$BIN" account add demo --imap 127.0.0.1:11143 --user "$USER" --insecure-test-server --password-from-env
"$BIN" sync --full -a demo --quiet
# Ein zweiter Lauf holt nach, was die Extraktion im ersten Zeitbudget nicht schaffte.
"$BIN" sync -a demo --quiet >/dev/null
"$BIN" mailbox list -a demo

# Nur Datenbank und Blobs gehören ins Paket (kein Log, keine Sperrdateien).
# Nach dem Ende des letzten Prozesses ist das WAL in die Datenbank übernommen.
if [ -e "$DATA/postkauz.db-wal" ] && [ -s "$DATA/postkauz.db-wal" ]; then
	echo "abnahme-d2-db: WAL not empty after the last command" >&2
	exit 1
fi
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
(cd "$DATA" && zip -q -r -X "$OUT" postkauz.db blobs)
ls -l "$OUT"
sha256sum "$OUT"
