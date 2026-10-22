#!/bin/sh
# Trockenfahrt der Abnahme D4 unter Linux (docs/abnahme_D4.md): dieselben
# Kommandos, Testdaten und Pruefmuster wie docs/abnahme_D4.ps1, gegen ein
# Linux-Programm, mit dem Beispiel-Hook check-example.sh statt .ps1 und mit
# der MCP-Probe docs/abnahme_D4_mcp.py. Zweck: Die erwarteten Ausgaben des
# Windows-Skripts sind am echten Programm belegt, nicht geraten. Was nur
# unter Windows pruefbar ist (PowerShell 5.1, Quoting, Konsolen-Kodierung,
# Hook ueber powershell.exe, Fenster), deckt sie nicht ab.
#
#   make build && sh scripts/abnahme-d4-trocken.sh [bin/postkauz]
#
# Ausgabe wie das Windows-Protokoll: je Pruefung "OK <Nr> ..." oder
# "FAIL <Nr> ...", am Ende "D4: <n> OK, <m> FAIL"; Exit 0 nur ohne FAIL.
# Nur Testdaten (zwei synthetische Mails, example.org), kein Server.
set -u

EXE=$(cd "$(dirname "${1:-bin/postkauz}")" && pwd)/$(basename "${1:-bin/postkauz}")
REPO=$(cd "$(dirname "$0")/.." && pwd)
HOOK="$REPO/deploy/hooks/check-example.sh"
PROBE="$REPO/docs/abnahme_D4_mcp.py"
RUN=$(mktemp -d)
trap 'rm -rf "$RUN"' EXIT
DATA="$RUN/data"
CFG="$RUN/config.toml"
INP="$RUN/input"
ATT="$RUN/attach"
KEYS="$RUN/keys"
mkdir -p "$DATA" "$INP" "$ATT" "$KEYS"
unset POSTKAUZ_KEY POSTKAUZ_KEY_FILE POSTKAUZ_DATA_DIR POSTKAUZ_CONFIG POSTKAUZ_POLICY POSTKAUZ_PASSWORD_DEMO
NOK=0
NFAIL=0
OUT=""
EXIT=0

check() { # nr beschreibung ok(0/1) [detail]
	if [ "$3" = 0 ]; then NOK=$((NOK + 1)); echo "OK $1 $2"; else NFAIL=$((NFAIL + 1)); echo "FAIL $1 $2"; fi
	if [ -n "${4:-}" ]; then printf '%s\n' "$4" | head -n 25 | sed -e 's/pk_key_[A-Za-z0-9_-]*/pk_key_********/g' -e 's/^/      /'; fi
}
pk() { # stdout in OUT, stdout+stderr in ALL (wie $r.Out und $r.All im Skript), Exit-Code in EXIT
	OUT=$("$EXE" --data-dir "$DATA" --config "$CFG" "$@" 2>"$RUN/stderr")
	EXIT=$?
	ALL="$OUT
$(cat "$RUN/stderr")"
}
has() { printf '%s\n' "$OUT" | grep -Pq -- "$1"; }
ok() { if "$@"; then echo 0; else echo 1; fi; }
json() { printf '%s' "$OUT" | python3 -c "import json,sys; d=json.load(sys.stdin); print($1)" 2>/dev/null; }
status() { pk draft show "$1" --json; json 'd["status"]'; }

printf '[paths]\nattach_dir = '"'"'%s'"'"'\n[operator]\nname = '"'"'D4-Abnahme'"'"'\n' "$ATT" >"$CFG"
printf "[draft]\ncheck_hook = ['sh', '%s', '{meta_json}', '{body_text}']\ncheck_hook_timeout = '30s'\n" "$HOOK" >"$RUN/policy.toml"
printf 'Beispieldatei fuer attach_dir (Abnahme D4)\r\n' >"$ATT/beispiel.txt"
printf 'Viele Gruesse\nDemo Buero (D4-Signatur)\n' >"$INP/signatur.txt"
printf 'Hallo Frau Beispiel,\n\nFreitag um 10 Uhr passt. KANARIENVOGEL\n' >"$INP/antwort.md"
printf 'Hallo Frau Beispiel,\n\nFreitag um 10 Uhr passt leider doch nicht. ANDERSVOGEL\n' >"$INP/anders.md"
# Dieselben zwei Mails wie im Here-String von abnahme_D4.ps1.
tr -d '\r' <"$REPO/docs/abnahme_D4.ps1" | sed -n '/^From MAILER-DAEMON Mon/,/^'"'"'@$/p' | sed '$d' >"$INP/korpus.mbox"

echo "===== 0 Vorbereitung"
pk version
check 0.2 "postkauz version" "$(ok [ $EXIT = 0 ])" "$OUT"
OUT=$(POSTKAUZ_PASSWORD_DEMO=unbenutzt-kein-server "$EXE" --data-dir "$DATA" --config "$CFG" account add demo --imap 127.0.0.1:1 --user demo@example.org --insecure-test-server --password-from-env 2>/dev/null)
EXIT=$?
check 0.3 "Konto demo ohne Server" "$(ok has 'account demo added')" "$OUT"
pk import mbox "$INP/korpus.mbox" -a demo --mailbox INBOX --quiet
check 0.4 "import mbox: 2 neu" "$(ok has 'new: 2, existing: 0, skipped: 0, errors: 0')" "$OUT"
pk show d4.antwort@corpus.example.org --json
MAILID=$(json 'd["id"]')
check 0.5 "Korpusmail gefunden (id $MAILID)" "$(ok [ "${MAILID:-0}" -gt 0 ])"
pk identity add -a demo --email demo@example.org --name 'Demo Buero' --signature-file "$INP/signatur.txt" --default
check 0.6 "Identitaet" "$(ok has 'identity 1 of account demo')" "$OUT"
pk doctor
check 0.7 "doctor" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '(?m)^policy:.*\\(found' && printf '%s' \"\$0\" | grep -Pq '(?m)^attach:\\s+$ATT' && printf '%s' \"\$0\" | grep -Pq '(?m)^data dir:\\s+$DATA' && ! printf '%s' \"\$0\" | grep -Pq '(?m)^\\s*really:'" "$OUT")" "$OUT"
for kv in test-agent:reader test-assistant:assistant; do
	pk key create --name "${kv%%:*}" --role "${kv#*:}"
	printf '%s' "$OUT" | grep -o 'pk_key_[A-Za-z0-9_-]*' | tr -d '\n' >"$KEYS/${kv%%:*}.key"
done
chmod 600 "$KEYS"/*.key
pk key list
check 0.8 "Schluessel" "$(ok sh -c "[ -s '$KEYS/test-agent.key' ] && [ -s '$KEYS/test-assistant.key' ] && ! printf '%s' \"\$0\" | grep -q pk_key_" "$OUT")" "$OUT"

echo "===== D4.1"
pk draft reply "$MAILID" --body-file "$INP/antwort.md" --attach beispiel.txt
check 1.1 'draft 1 created (status draft, remote pending)' "$(ok has '^draft 1 created \(status draft, remote pending\)')" "$OUT"
pk draft show 1 --eml
check 1.2 "In-Reply-To, References" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '^In-Reply-To: <d4\\.antwort@corpus\\.example\\.org>\\r?$' && printf '%s' \"\$0\" | grep -Pq '^References: <d4\\.anfrage@corpus\\.example\\.org> <d4\\.antwort@corpus\\.example\\.org>\\r?$'" "$OUT")" "$(printf '%s' "$OUT" | grep -E '^(In-Reply-To|References):')"
check 1.3 "Betreff Re:, Message-ID" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '^Subject: Re: Termin Heizungswartung\\r?$' && printf '%s' \"\$0\" | grep -Piq '^Message-Id: <pk\\.[0-9a-f]{20}@example\\.org>'" "$OUT")"
check 1.4 "Zitat" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq 'On Tue, 20 Oct 2026 10:15 \\+0200, Erika Beispiel <erika@example\\.org> wrote:' && printf '%s' \"\$0\" | grep -Pq '^> Der Schl=C3=BCssel liegt beim Hausmeister\\.'" "$OUT")"
check 1.5 "Signatur, Anhang" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '^Demo Buero \\(D4-Signatur\\)' && printf '%s' \"\$0\" | grep -Pq 'filename=beispiel\\.txt'" "$OUT")"
pk draft show 1
check 1.6 "Umlaute, status draft, remote pending" "$(ok sh -c "printf '%s' \"\$0\" | grep -q 'Der Schlüssel liegt beim Hausmeister' && printf '%s' \"\$0\" | grep -q 'Wäre Freitag' && printf '%s' \"\$0\" | grep -Pq '^remote:\\s+pending' && printf '%s' \"\$0\" | grep -q 'status draft'" "$OUT")" "$OUT"

echo "===== D4.2"
pk draft request 1
check 2.1 "hook: pass, approval 1" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '^hook: pass' && printf '%s' \"\$0\" | grep -q 'status pending_approval, approval 1 '" "$OUT")" "$OUT"

echo "===== D4.3"
pk draft edit 1 --subject 'TODO Re: Termin Heizungswartung'
check 3.1 "edit TODO" "$(ok sh -c "printf '%s' \"\$0\" | grep -q 'status pending_approval -> draft' && printf '%s' \"\$0\" | grep -q 'approval 1 superseded'" "$OUT")" "$(printf '%s' "$OUT" | head -3)"
pk draft request 1
check 3.2 "hook: fail" "$(ok sh -c "[ $EXIT = 0 ] && printf '%s' \"\$0\" | grep -Pq '^hook: fail' && printf '%s' \"\$0\" | grep -q 'subject contains TODO' && printf '%s' \"\$0\" | grep -q 'status stays draft' && ! printf '%s' \"\$0\" | grep -q pending_approval" "$OUT")" "$OUT"
ST=$(status 1)
check 3.3 "status draft" "$(ok [ "$ST" = draft ])" "status $ST"
LEFT=$(find "${TMPDIR:-/tmp}" -maxdepth 1 -type d -name 'postkauz-hook-*' -newer "$CFG" | wc -l)
check 3.4 "kein Temp-Ordner postkauz-hook-* uebrig" "$(ok [ "$LEFT" = 0 ])" "neue Ordner: $LEFT"

echo "===== D4.4"
pk draft edit 1 --subject 'Re: Termin Heizungswartung'
check 4.1 "edit Re:" "$(ok has 'draft 1 changed \(status draft -> draft')" "$(printf '%s' "$OUT" | head -2)"
pk draft request 1
check 4.2 "approval 2" "$(ok sh -c "printf '%s' \"\$0\" | grep -Pq '^hook: pass' && printf '%s' \"\$0\" | grep -q 'status pending_approval, approval 2 '" "$OUT")" "$OUT"
pk approve 2
A="$OUT"; E=$EXIT; ST=$(status 1)
check 4.3 "approve 2 -> ready" "$(ok sh -c "[ $E = 0 ] && printf '%s' \"\$0\" | grep -q 'is ready' && [ '$ST' = ready ]" "$A")" "$A"
pk draft edit 1 --body-file "$INP/anders.md"
A="$OUT"; E=$EXIT; ST=$(status 1)
check 4.4 "edit body -> draft, superseded" "$(ok sh -c "[ $E = 0 ] && printf '%s' \"\$0\" | grep -q 'status ready -> draft' && printf '%s' \"\$0\" | grep -q 'approval 2 superseded' && [ '$ST' = draft ]" "$A")" "$(printf '%s' "$A" | head -2)"
pk approve 2
A="$ALL"; E=$EXIT; ST=$(status 1)
check 4.5 "approve 2 -> approval_stale, Exit 3" "$(ok sh -c "[ $E = 3 ] && printf '%s' \"\$0\" | grep -q approval_stale && [ '$ST' = draft ]" "$A")" "$A
[exit $E]"

echo "===== D4.5"
pk policy show --key test-agent
check 5.1 "human_only" "$(ok has '^\s*drafts\.approve: human_only\s*$')" "$(printf '%s' "$OUT" | tail -10)"
pk draft request 1
check 5.2 "approval 3" "$(ok has 'status pending_approval, approval 3 ')" "$OUT"
pk approve 3 --key test-agent
check 5.3 "approve --key -> human_only, Exit 3" "$(ok sh -c "[ $EXIT = 3 ] && printf '%s' \"\$0\" | grep -q 'denied (human_only)'" "$ALL")" "$ALL
[exit $EXIT]"
pk approval show 3 --json
APS=$(json 'd["status"]+" / "+d["draft_status"]')
check 5.4 "unveraendert" "$(ok [ "$APS" = 'pending / pending_approval' ])" "$APS"

echo "===== D4.6"
pk audit --since 10m
check 6.1 "audit text" "$(ok sh -c "[ $EXIT = 0 ] && printf '%s' \"\$0\" | grep -q transition" "$OUT")"
pk audit --since 10m --limit 1000 --json
AUD="$OUT"
R=$(printf '%s' "$AUD" | python3 -c '
import json, sys
ev = [json.loads(l) for l in sys.stdin if l.startswith("{")]
tr = [e for e in ev if e.get("reason") == "transition" and e.get("params", {}).get("draft_id") == 1]
hooks = {e["params"].get("hook") for e in tr} - {None}
tos = {e["params"].get("to") for e in tr}
human = [e for e in tr if e["actor"] == "human"]
ok = len(tr) >= 9 and len(human) == len(tr) and {"pass", "fail"} <= hooks and {"pending_approval", "ready", "draft"} <= tos
print(("0" if ok else "1") + " transitions %d human %d hooks %s to %s" % (len(tr), len(human), sorted(hooks), sorted(tos)))')
check 6.2 "Uebergaenge" "${R%% *}" "${R#* }"
HIT=$(printf '%s' "$AUD" | grep -oi 'KANARIENVOGEL\|ANDERSVOGEL\|Heizungswartung\|erika@example.org\|Erika Beispiel\|beispiel.txt\|D4-Signatur\|Hausmeister' | sort -u | tr '\n' ' ')
check 6.3 "Audit ohne Inhalt" "$(ok [ -z "$HIT" ])" "Treffer: [$HIT]"
HIT=$(grep -oi 'KANARIENVOGEL\|ANDERSVOGEL\|Heizungswartung\|erika@example.org\|Erika Beispiel\|beispiel.txt\|D4-Signatur\|Hausmeister\|subject contains' "$DATA/postkauz.log" | sort -u | tr '\n' ' ')
check 6.4 "Log ohne Inhalt" "$(ok [ -z "$HIT" ] && [ -s "$DATA/postkauz.log" ])" "Treffer: [$HIT]"

echo "===== D4.7"
POUT=$(python3 -I "$PROBE" tools --exe "$EXE" --data-dir "$DATA" --config "$CFG" --reader-key-file "$KEYS/test-agent.key" --assistant-key-file "$KEYS/test-assistant.key" --reply-to "$MAILID" --canary MCPVOGEL)
PEXIT=$?
printf '%s\n' "$POUT" | grep -v '^VALUE '
NOK=$((NOK + $(printf '%s\n' "$POUT" | grep -c '^OK ')))
NFAIL=$((NFAIL + $(printf '%s\n' "$POUT" | grep -c '^FAIL ')))
MD=$(printf '%s\n' "$POUT" | sed -n 's/^VALUE draft_id=//p')
check 7.0 "Probe vollstaendig" "$(ok [ $PEXIT = 0 ] && [ "$(printf '%s\n' "$POUT" | grep -c '^\(OK\|FAIL\) ')" = 6 ] && [ -n "$MD" ])"
pk draft list
check 7.7 "draft list zeigt $MD" "$(ok has "^$MD\s+demo\s+reply\s+draft\s+-\s+pending\s")" "$OUT"
pk audit --since 1h --limit 1000 --json
R=$(printf '%s' "$OUT" | python3 -c '
import json, sys
md = int(sys.argv[1]); raw = sys.stdin.read()
ev = [json.loads(l) for l in raw.splitlines() if l.startswith("{")]
ag = [e for e in ev if e.get("key") == "key:test-assistant"]
non = [e for e in ag if e["actor"] != "agent"]
cr = [e for e in ag if e["operation"] == "drafts.create" and e.get("reason") == "transition" and e["params"].get("draft_id") == md]
dry = [e for e in ag if e["operation"] == "drafts.create" and e.get("dry_run") is True]
hit = [w for w in ("MCPVOGEL", "Hausmeister", "erika@example.org") if w.lower() in raw.lower()]
ok = ag and not non and len(cr) == 1 and dry and not hit
print(("0" if ok else "1") + " agent %d non-agent %d create %d dry %d hit %s" % (len(ag), len(non), len(cr), len(dry), hit))' "$MD")
check 7.8 "Audit agent" "${R%% *}" "${R#* }"

echo "===== D4.9"
pk draft abandon "$MD"
check 9.1 "abandon" "$(ok has "draft $MD: abandoned")" "$OUT"
POUT=$(python3 -I "$PROBE" closed --exe "$EXE" --data-dir "$DATA" --config "$CFG" --reader-key-file "$KEYS/test-agent.key" --assistant-key-file "$KEYS/test-assistant.key" --draft "$MD")
PEXIT=$?
printf '%s\n' "$POUT"
NOK=$((NOK + $(printf '%s\n' "$POUT" | grep -c '^OK ')))
NFAIL=$((NFAIL + $(printf '%s\n' "$POUT" | grep -c '^FAIL ')))
check 9.0 "Probe closed" "$(ok [ $PEXIT = 0 ])"
pk audit --since 1h --limit 1000 --json
N=$(printf '%s\n' "$OUT" | grep '"key":"key:test-assistant"' | grep '"actor":"agent"' | grep '"operation":"drafts.update"' | grep '"result":"denied"' | grep -c '"reason":"draft_closed"')
check 9.3 "Audit draft_closed" "$(ok [ "$N" -ge 1 ])" "Treffer: $N"
ST=$(status "$MD")
check 9.4 "bleibt abandoned" "$(ok [ "$ST" = abandoned ])" "status $ST"

echo "D4: $NOK OK, $NFAIL FAIL"
[ "$NFAIL" = 0 ]
