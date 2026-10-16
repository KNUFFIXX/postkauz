#!/bin/sh
# Abnahme D3.1 im Container: die Punkte D0 bis D2 gegen einen zweiten bzw.
# dritten Testserver (Stalwart 0.11.8 über IMAPS, GreenMail), mit der
# Programmdatei wie beim Betreiber. Die Ausgabe ist das Protokoll
# (docs/protokolle/D3.1_<server>.txt).
#
#   make testmail-down testmail-up testmail-setup   # frische Server (löscht auch das Stalwart-Volume)
#   sh scripts/abnahme-d3-server.sh stalwart  > docs/protokolle/D3.1_stalwart.txt
#   sh scripts/abnahme-d3-server.sh greenmail > docs/protokolle/D3.1_greenmail.txt
#
# Nur lokale Testserver mit öffentlich bekannten Testzugangsdaten (test/pass,
# deploy/testmail/README.md); erwartet ein leeres Konto (frisch gestartete
# Server). Schlüssel und Pfade werden im Protokoll unkenntlich gemacht.
set -u

case "${1:-}" in
stalwart) IMAP=127.0.0.1:12993 TLS=tls ;;
greenmail) IMAP=127.0.0.1:13143 TLS=none ;;
*)
	echo "usage: sh scripts/abnahme-d3-server.sh stalwart|greenmail" >&2
	exit 2
	;;
esac
export GOTOOLCHAIN=local CGO_ENABLED=0
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/postkauz"
go build -trimpath -ldflags "-X main.version=0.1.0-rc1" -o "$BIN" ./cmd/postkauz || exit 1
go run ./internal/corpus/cmd/gencorpus -out "$WORK/corpus" >/dev/null || exit 1
export POSTKAUZ_DATA_DIR="$WORK/data" POSTKAUZ_CONFIG="$WORK/keine-config.toml" POSTKAUZ_SECRETS_BACKEND=insecure-file \
	POSTKAUZ_PASSWORD_DEMO=pass

# Ausgabe ohne Pfade des Containers, ohne Schlüssel im Klartext und ohne
# abgeschnittene Adressen (die Tabelle kürzt sie; das PII-Gate erkennt eine
# gekürzte Testdomain nicht mehr als solche).
clean() {
	sed -e "s#$WORK#<tmp>#g" -e 's/pk_key_[A-Za-z0-9_-]\{43\}/pk_key_<43 characters, shown once>/g' \
		-e 's/<[^<> ]*@[^<> ]*…/<…/g'
}
# step NAME CMD...: Kommando zeigen, ausführen, Exit-Code zeigen.
step() {
	name=$1
	shift
	printf '\n### %s\n$ postkauz %s\n' "$name" "$*" | clean
	"$BIN" "$@" 2>&1 | clean
	echo "(exit $?)"
}
# Exit-Code des Programms, nicht der Pipe: über eine Datei.
stepx() {
	name=$1
	shift
	printf '\n### %s\n$ postkauz %s\n' "$name" "$*" | clean
	"$BIN" "$@" >"$WORK/out" 2>&1
	rc=$?
	clean <"$WORK/out"
	echo "(exit $rc)"
}

echo "# Abnahme D3.1: D0 bis D2 gegen $1 ($IMAP, TLS $TLS)"
echo "# $(date -u +%Y-%m-%dT%H:%MZ), Programm $("$BIN" version), Commit $(git rev-parse --short HEAD)"

echo
echo "## D0"
stepx "D0.2 version" version
stepx "D0.3 accounts list" accounts list
stepx "D0.4 accounts list --json" accounts list --json
printf '\n### D0.5 doctor (Auszug)\n$ postkauz doctor\n'
"$BIN" doctor 2>&1 | grep -E '^(database|schema|keyring|search|extract|audit):' | clean
stepx "D0.6 unknown command" bogus

echo
echo "## D1"
stepx "D1.1 account add" account add demo --imap "$IMAP" --user test --tls "$TLS" --insecure-test-server --password-from-env
stepx "D1.2 account test" account test demo
printf '\n### D1.3 load corpus\n$ go run ./internal/corpus/cmd/corpusload -imap %s -user test -password pass\n' "$IMAP"
go run ./internal/corpus/cmd/corpusload -imap "$IMAP" -user test -password pass 2>&1 | tail -3 | clean
printf '\n### D1.4 sync --full, interrupted after 2 s (Ctrl+C)\n$ postkauz sync --full -a demo   # SIGINT after 2 s\n'
timeout --preserve-status -s INT 2 "$BIN" sync --full -a demo --quiet >"$WORK/out" 2>&1
rc=$?
clean <"$WORK/out" | tail -3
echo "(exit $rc)"
stepx "D1.3/D1.4 sync --full (resumed)" sync --full -a demo --quiet
stepx "D1.4 sync --full again (same count)" sync --full -a demo --quiet
printf '\n### D1.5 mailbox list --json (total/mirrored per folder)\n$ postkauz mailbox list -a demo --json\n'
"$BIN" mailbox list -a demo --json | python3 -c '
import json, sys
for m in json.load(sys.stdin)["mailboxes"]:
    print("%-16s role=%-7s total=%-4s mirrored=%-4s unread=%s" % (m["name"], m.get("role") or "-", m.get("total"), m.get("mirrored"), m.get("unread")))'
printf '\n### D1.6 flag from outside, sync, show --headers\n$ go run ./internal/corpus/cmd/corpusload ... -mark-seen\n'
MID=$(go run ./internal/corpus/cmd/corpusload -imap "$IMAP" -user test -password pass -mark-seen 2>&1 | sed -n 's/^message-id: *<\{0,1\}\([^>]*\)>\{0,1\}$/\1/p' | head -1)
echo "marked as read: <$MID>"
stepx "D1.6 sync" sync -a demo --quiet
printf '\n$ postkauz show %s -a demo --headers   (line flags)\n' "$MID"
"$BIN" show "$MID" -a demo --headers 2>&1 | grep -i '^flags' | clean

echo
echo "## D2"
stepx "D2.1 search Rechnung" search "Rechnung" -a demo --limit 5
printf '\n### D2.2 search prüfen / pruefen (last line)\n'
for q in "prüfen" "pruefen"; do
	printf '$ postkauz search "%s" -a demo\n' "$q"
	"$BIN" search "$q" -a demo --limit 1 | grep -E 'results'
done
stepx "D2.3 search Vollstreck" search "Vollstreck" -a demo --limit 3
printf '\n### D2.4 search has:attachment --json | head -n 3 (cut to 150 characters)\n$ postkauz search "has:attachment" -a demo --json | head -n 3\n'
"$BIN" search "has:attachment" -a demo --json | head -n 3 | cut -c1-150
HID=$(python3 -c 'import json,sys; m=json.load(open(sys.argv[1])); print([x["message_id"] for x in m["messages"] if x["kind"]=="hidden"][2])' "$WORK/corpus/corpus.json")
printf '\n### D2.5 show (hidden text)\n$ postkauz show %s -a demo   (line warning)\n' "$HID"
"$BIN" show "$HID" -a demo | grep -i 'warning' | clean
stepx "D2.6 key create" key create --name test-agent --role reader
stepx "D2.6 key list" key list
printf '\n### D2.7 MCP: tools/list and tools/call (JSON-RPC over stdio, key mcp-agent, role reader)\n'
KEY=$("$BIN" key create --name mcp-agent --role reader --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["secret"])')
{
	echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"d3","version":"1"}}}'
	echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
	echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
	echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"mail_search","arguments":{"query":"Rechnung","limit":2}}}'
	sleep 3
} | POSTKAUZ_KEY="$KEY" "$BIN" mcp 2>/dev/null | python3 -c '
import json, sys
for line in sys.stdin:
    m = json.loads(line)
    if m.get("id") == 2:
        print("tools:", ", ".join(t["name"] for t in m["result"]["tools"]))
    if m.get("id") == 3:
        r = json.loads(m["result"]["content"][0]["text"])
        print("mail_search: untrusted=%s total=%s hits=%d" % (r["untrusted"], r["total"], len(r["hits"])))'
stepx "D2.8 audit (agent calls, no mail text)" audit --since 10m --key-id key:mcp-agent
mkdir -p "$WORK/export"
stepx "D2.9 audit export" audit export --to "$WORK/export"
stepx "D2.9 audit verify" audit verify
F=$(ls "$WORK"/export/postkauz-audit-*.jsonl | tail -1)
sed -i '2s/"result":"ok"/"result":"denied"/' "$F"
printf '\n(line 2 of %s changed: result ok -> denied)\n' "$(basename "$F")"
stepx "D2.9 audit verify (changed line)" audit verify
stepx "S1.11 doctor --secrets (summary)" doctor --secrets
