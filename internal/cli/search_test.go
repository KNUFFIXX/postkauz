package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// S1.7 in der CLI (Abnahme D2.1–D2.4 im Kleinen): sync baut den Index,
// search findet Flexionen und Teilwörter, --json liefert NDJSON mit Cursor,
// --explain zeigt die Übersetzung; versteckter Text ist nicht auffindbar.
func TestSearchCommand(t *testing.T) {
	setupEnv(t)
	addr := s16Server(t)
	run := func(want int, args ...string) string {
		t.Helper()
		stdin := ""
		if args[0] == "account" {
			stdin = canary + "\n"
		}
		code, out, errs := runIn(t, stdin, args...)
		if code != want {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
		}
		return out + errs
	}
	run(ExitOK, "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin")
	out := run(ExitOK, "sync", "-a", "demo")
	if !strings.Contains(out, "search index: 3 messages indexed\n3 messages mirrored\n0 errors\n") {
		t.Fatalf("sync:\n%s", out)
	}

	// D2.1: Treffer mit ID, Datum, Absender, Betreff; Flexion (Rechnungen → Rechnung).
	out = run(ExitOK, "search", "Rechnungen", "-a", "demo", "--limit", "5")
	if !strings.Contains(out, "ID") || !strings.Contains(out, "2026-03-04") || !strings.Contains(out, "Portal <noreply@portal.example.net>") ||
		!strings.Contains(out, "Rechnung [attachment]") || !strings.Contains(out, "1 of 1 results (by relevance)") {
		t.Fatalf("search Rechnungen:\n%s", out)
	}
	// D2.2: Umlaut-Varianten liefern dasselbe.
	// („Mönteur" und „Moenteur" falten beide zu „monteur".)
	if a, b := run(ExitOK, "search", "Mönteur"), run(ExitOK, "search", "Moenteur"); a != b || !strings.Contains(a, "AW: Heizung defekt") {
		t.Fatalf("umlaut variants differ:\n%s\n---\n%s", a, b)
	}
	// D2.4: NDJSON, eine Zeile je Treffer; Cursor setzt fort.
	out = run(ExitOK, "search", "Heizung", "--json", "--limit", "1")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var hit struct {
		ID      int64  `json:"id"`
		Subject string `json:"subject"`
		Cursor  string `json:"cursor"`
	}
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &hit) != nil || hit.ID == 0 || hit.Cursor == "" {
		t.Fatalf("ndjson page 1:\n%s", out)
	}
	out = run(ExitOK, "search", "Heizung", "--json", "--limit", "5", "--cursor", hit.Cursor)
	lines = strings.Split(strings.TrimSpace(out), "\n")
	var hit2 struct {
		ID int64 `json:"id"`
	}
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &hit2) != nil || hit2.ID == hit.ID {
		t.Fatalf("ndjson page 2:\n%s", out)
	}
	out = run(ExitOK, "search", "has:attachment", "--json")
	if n := strings.Count(strings.TrimSpace(out), "\n") + 1; n != 1 || !strings.Contains(out, `"has_attachments":true`) {
		t.Fatalf("has:attachment:\n%s", out)
	}
	// --explain: Übersetzung (Mensch und NDJSON-Kopfzeile).
	out = run(ExitOK, "search", `"Bad ist" -Monteur from:anna`, "--explain")
	if !strings.Contains(out, "phrase") || !strings.Contains(out, "{sender}") || !strings.Contains(out, "sql:") ||
		!strings.Contains(out, "1 of 1 results") {
		t.Fatalf("explain:\n%s", out)
	}
	out = run(ExitOK, "search", "Heizung", "--explain", "--json")
	first, _, _ := strings.Cut(out, "\n")
	var head map[string]any
	if json.Unmarshal([]byte(first), &head) != nil || head["explain"] == nil || head["total"] != float64(2) {
		t.Fatalf("explain json head: %s", first)
	}
	// Versteckter Text ist nicht im Index; Anfragefehler sind Exit 2.
	if out := run(ExitOK, "search", "angreifer"); !strings.Contains(out, "No messages found.") {
		t.Fatalf("hidden text found:\n%s", out)
	}
	if out := run(ExitUsage, "search", "has:pdf", "--json"); !strings.Contains(out, `"invalid_input"`) {
		t.Fatalf("bad query: %s", out)
	}
	run(ExitUsage, "search")
}
