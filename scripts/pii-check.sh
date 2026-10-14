#!/bin/sh
# PII-Gate (Erstfassung, docs/testkorpus.md, Abschnitt 3).
# Prüft versionierte Textdateien auf:
#   1. E-Mail-Adressen außerhalb reservierter Domains,
#   2. IBAN-Muster,
#   3. Begriffe aus einer lokalen Sperrliste (MAILWERK_PII_DENYLIST, eine Zeile je Begriff;
#      die Liste liegt außerhalb des Repositorys).
# Ausnahmen: scripts/pii-allow.txt (ein Treffer-Text je Zeile, begründet im Commit).
# Exit 0 = sauber, 1 = Treffer.
set -u
cd "$(dirname "$0")/.." || exit 2

files() {
  if git rev-parse --git-dir >/dev/null 2>&1; then
    git ls-files --cached --others --exclude-standard
  else
    find . -type f -not -path './.git/*'
  fi | grep -Ev '(^|/)(go\.sum|LICENSE)$|\.(png|jpg|gif|ico|zst|gz|zip|pdf|db)$|^scripts/pii-'
}

fail=0
allow=scripts/pii-allow.txt
[ -f "$allow" ] || allow=/dev/null

# $1 = Beschreibung, $2 = Treffer (Datei:Zeile:Text); Ausnahmen werden entfernt.
report() {
  hits="$(printf '%s\n' "$2" | grep -v '^$' | grep -vFf "$allow" || true)"
  if [ -n "$hits" ]; then
    echo "pii-check: $1"; echo "$hits"; fail=1
  fi
}

# 1. E-Mail-Adressen; erlaubt sind example.com/.org/.net, *.invalid, *.test, localhost
hits="$(files | xargs -r grep -InoE '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' 2>/dev/null \
  | grep -Eiv '@([a-z0-9-]+\.)*(example\.(com|org|net)|invalid|test)$' || true)"
report "e-mail addresses outside reserved domains" "$hits"

# 2. IBAN-Muster (Länderkürzel, zwei Prüfziffern, mindestens 11 weitere Zeichen)
hits="$(files | xargs -r grep -InoE '\b[A-Z]{2}[0-9]{2}( ?[0-9A-Z]{4}){3,7}( ?[0-9A-Z]{1,3})?\b' 2>/dev/null || true)"
report "IBAN-like patterns" "$hits"

# 3. Sperrliste
if [ -n "${MAILWERK_PII_DENYLIST:-}" ] && [ -f "$MAILWERK_PII_DENYLIST" ]; then
  hits="$(files | xargs -r grep -InoiFf "$MAILWERK_PII_DENYLIST" 2>/dev/null || true)"
  report "terms from the local denylist" "$hits"
fi

if [ "$fail" -eq 0 ]; then echo "pii-check: ok"; fi
exit "$fail"
