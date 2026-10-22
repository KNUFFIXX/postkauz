#!/bin/sh
# Example check hook for PostKauz drafts (Linux, macOS).
#
# PostKauz runs the check hook before a draft is submitted for approval
# ("postkauz draft request <id>"). Exit 0 = pass; any other exit code = fail,
# the draft stays "draft" and the first 4 KB of the output are shown as the
# reason. The output is never written to the audit log.
#
# This example fails if
#   1. the subject contains "TODO" (upper case, anywhere), or
#   2. the greeting is missing: the first non-empty line of the text (leading
#      spaces ignored) must start with one of
#        Hallo | Guten Tag | Sehr geehrte | Liebe | Lieber | Dear | Hi
#      ("Sehr geehrte" also covers "Sehr geehrter"). This is a plain prefix
#      check with exact upper/lower case, so "Hilfe" counts as "Hi" and
#      "hallo" does not count. It is an example, not a style guide.
#
# Configuration in policy.toml (next to config.toml):
#
#   [draft]
#   check_hook = ["sh", "/opt/postkauz/hooks/check-example.sh", "{meta_json}", "{body_text}"]
#   check_hook_timeout = "30s"
#
# PostKauz replaces {meta_json} and {body_text} with paths of temporary files
# (also available as POSTKAUZ_DRAFT_META_JSON and POSTKAUZ_DRAFT_BODY_TEXT).
# The draft content comes only as files, never as arguments. meta.json has
# one top-level key per line, so the subject line can be found without a
# JSON tool.

set -u

meta="${1:-${POSTKAUZ_DRAFT_META_JSON:-}}"
text="${2:-${POSTKAUZ_DRAFT_BODY_TEXT:-}}"
if [ ! -r "$meta" ] || [ ! -r "$text" ]; then
	echo "check-example: give the paths of {meta_json} and {body_text}"
	exit 2
fi

problems=0

# 1. Subject: the line '  "subject": "...",' of meta.json (JSON-escaped).
subject_line="$(sed -n '/^  "subject": /{p;q;}' "$meta")"
case "$subject_line" in
*TODO*)
	echo "subject contains TODO"
	problems=1
	;;
esac

# 2. Greeting: first non-empty line of the text, leading spaces removed.
first="$(tr -d '\r' <"$text" | sed -n '/[^[:space:]]/{s/^[[:space:]]*//;p;q;}')"
case "$first" in
Hallo* | "Guten Tag"* | "Sehr geehrte"* | Liebe* | Lieber* | Dear* | Hi*) ;;
*)
	echo "greeting missing: the first line of the text must start with Hallo, Guten Tag, Sehr geehrte, Liebe, Lieber, Dear or Hi"
	problems=1
	;;
esac

exit "$problems"
