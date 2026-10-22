package ops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// adminEnv: Store im Speicher, Executor mit SQLite-Audit (IDs) und den
// Schlüssel-, Audit- und Policy-Operationen.
type adminEnv struct {
	st  *store.SQLite
	x   *Executor
	dir string
}

func newAdminEnv(t *testing.T) *adminEnv {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	e := &adminEnv{st: st, dir: filepath.Join(t.TempDir(), "export")}
	reg := NewRegistry()
	for _, op := range []Operation{KeysCreate(st), KeysList(st), KeysRevoke(st), AuditList(st), AuditExport(st, e.dir),
		AuditVerify(st), PolicyGet(0)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	e.x = &Executor{Registry: reg, Audit: st.AuditWriter()}
	return e
}

func (e *adminEnv) run(t *testing.T, key policy.Key, op string, in any) (Result, error) {
	t.Helper()
	b, _ := json.Marshal(in)
	return e.x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorHuman, Operation: op, Input: b})
}

func TestKeysCreateAuthenticateRevoke(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	res, err := e.run(t, adminKey, "keys.create", map[string]any{"name": "agent-1", "role": "reader"})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(KeysCreateOutput)
	if !strings.HasPrefix(out.Secret, KeyPrefix) || len(out.Secret) != len(KeyPrefix)+43 || out.Key.Status != "active" ||
		out.Key.ExpiresAt == "" || !out.Key.DryRunDefault || res.AuditID == 0 {
		t.Fatalf("create = %+v (audit %d)", out, res.AuditID)
	}
	// Gespeichert ist nur der Hash.
	k, _ := e.st.APIKeyByName(ctx, "agent-1")
	if k.KeyHash != HashKey(out.Secret) || strings.Contains(k.KeyHash, out.Secret) {
		t.Fatal("stored value is not the hash of the key")
	}
	// Audit ohne Klartext.
	recs, _ := e.st.AuditQuery(ctx, store.AuditFilter{})
	for _, r := range recs {
		if b, _ := json.Marshal(r); strings.Contains(string(b), out.Secret) {
			t.Fatalf("key in audit: %s", b)
		}
	}
	// Anmeldung: richtig, falsch, leer; last_used_at wird gesetzt.
	now := time.Now()
	pk, err := AuthenticateKey(ctx, e.st, " "+out.Secret+"\n", now)
	if err != nil || pk.ID != "key:agent-1" || pk.Name != "agent-1" || pk.Role != "reader" || !pk.DryRunDefault ||
		!slices.Equal(pk.Scopes, policy.Roles["reader"]) {
		t.Fatalf("authenticate = %+v %v", pk, err)
	}
	if k, _ := e.st.APIKeyByName(ctx, "agent-1"); k.LastUsedAt.IsZero() {
		t.Fatal("last_used_at not set")
	}
	if _, err := AuthenticateKey(ctx, e.st, out.Secret+"x", now); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err := AuthenticateKey(ctx, e.st, "", now); !errors.Is(err, ErrKeyMissing) {
		t.Fatalf("empty key: %v", err)
	}
	if _, err := AuthenticateKey(ctx, e.st, out.Secret, now.Add(91*24*time.Hour)); !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("expired key: %v", err)
	}
	// Verengen ja, erweitern nein; Trockenlauf legt nichts an.
	res, err = e.run(t, adminKey, "keys.create", map[string]any{"name": "triage", "role": "assistant", "scopes": []string{"mail:search"},
		"expires_in_days": 0, "dry_run_default": false})
	if o := res.Output.(KeysCreateOutput); err != nil || strings.Join(o.Key.Scopes, ",") != "mail:search" || o.Key.ExpiresAt != "" || o.Key.DryRunDefault {
		t.Fatalf("narrowed = %+v %v", res.Output, err)
	}
	var ie *InputError
	if _, err := e.run(t, adminKey, "keys.create", map[string]any{"name": "x", "role": "reader", "scopes": []string{"mail:draft"}}); !errors.As(err, &ie) {
		t.Fatalf("widen: %v", err)
	}
	if _, err := e.run(t, adminKey, "keys.create", map[string]any{"name": "x", "role": "chef"}); !errors.As(err, &ie) {
		t.Fatalf("unknown role: %v", err)
	}
	if _, err := e.run(t, adminKey, "keys.create", map[string]any{"name": "agent-1"}); !errors.Is(err, store.ErrKeyExists) {
		t.Fatalf("duplicate: %v", err)
	}
	dry := true
	b, _ := json.Marshal(map[string]any{"name": "trocken"})
	r, err := e.x.Execute(ctx, Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "keys.create", Input: b, DryRun: &dry})
	if err != nil || r.Output.(KeysCreateOutput).Secret != "" {
		t.Fatalf("dry run = %+v %v", r.Output, err)
	}
	if _, err := e.st.APIKeyByName(ctx, "trocken"); !errors.Is(err, store.ErrKeyNotFound) {
		t.Fatal("dry run created a key")
	}
	// Liste ohne Hash, Widerruf, danach keine Anmeldung.
	res, _ = e.run(t, adminKey, "keys.list", map[string]any{})
	if lb, _ := json.Marshal(res.Output); strings.Contains(string(lb), k.KeyHash) || len(res.Output.(KeysListOutput).Keys) != 2 {
		t.Fatalf("list = %s", lb)
	}
	res, err = e.run(t, adminKey, "keys.revoke", map[string]any{"name": "agent-1"})
	if err != nil || !res.Output.(KeysRevokeOutput).Revoked {
		t.Fatalf("revoke = %+v %v", res.Output, err)
	}
	if _, err := AuthenticateKey(ctx, e.st, out.Secret, now); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("revoked key: %v", err)
	}
	if _, err := KeyByName(ctx, e.st, "agent-1", now); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("KeyByName revoked: %v", err)
	}
	// Schlüsselverwaltung nur mit admin.
	if _, err := e.run(t, pk, "keys.list", map[string]any{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader lists keys: %v", err)
	}
}

func TestAuditListExportVerify(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	reader := policy.Key{ID: "key:leser", Scopes: policy.Roles["reader"]}
	for i := 0; i < 3; i++ {
		_, _ = e.run(t, reader, "policy.get", map[string]any{})
	}
	_, _ = e.run(t, reader, "keys.list", map[string]any{}) // verweigert, aber auditiert
	res, err := e.run(t, reader, "audit.list", map[string]any{"since": "10m", "key": "key:leser", "limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	o := res.Output.(AuditListOutput)
	if len(o.Events) != 2 || o.NextAfterID != o.Events[1].ID || o.Events[0].Key != "key:leser" || o.Events[0].Operation != "policy.get" {
		t.Fatalf("page 1 = %+v", o)
	}
	res, _ = e.run(t, reader, "audit.list", map[string]any{"key": "key:leser", "result": "denied"})
	if o := res.Output.(AuditListOutput); len(o.Events) != 1 || o.Events[0].Reason != "missing_scope" || o.Events[0].Operation != "keys.list" {
		t.Fatalf("denied = %+v", o)
	}
	var ie *InputError
	if _, err := e.run(t, reader, "audit.list", map[string]any{"since": "gestern"}); !errors.As(err, &ie) {
		t.Fatalf("bad since: %v", err)
	}
	for _, s := range []string{"10m", "7d", "2026-01-02", "2026-01-02T03:04:05Z"} {
		if _, err := ParseSince(s, time.Now()); err != nil {
			t.Errorf("ParseSince(%q): %v", s, err)
		}
	}

	// Export: nur mit admin; Datei, Prüfsumme, Nachweis in der Datenbank.
	if _, err := e.run(t, reader, "audit.export", map[string]any{}); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader exports: %v", err)
	}
	res, err = e.run(t, adminKey, "audit.export", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	ex := res.Output.(AuditExportOutput)
	if len(ex.Files) != 1 || ex.Files[0].State != "written" || ex.Location != e.dir {
		t.Fatalf("export = %+v", ex)
	}
	b, _ := os.ReadFile(ex.Files[0].File)
	if sha256Hex(b) != ex.Files[0].SHA256 || strings.Count(string(b), "\n") != ex.Files[0].Events {
		t.Fatalf("file does not match: %s", b)
	}
	res, _ = e.run(t, reader, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "ok" || v.DaysChecked != 1 {
		t.Fatalf("verify = %+v", v)
	}
	// Eine Datenbankzeile wird verändert (Sperre per Hand entfernt): database_changed.
	if err := e.st.ExecForTest(`DROP TRIGGER audit_events_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE audit_events SET result = 'ok' WHERE result = 'denied'`); err != nil {
		t.Fatal(err)
	}
	res, _ = e.run(t, reader, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "mismatch" || len(v.Problems) != 1 || v.Problems[0].Problem != "database_changed" {
		t.Fatalf("verify after db change = %+v", v)
	}
	// Datei gelöscht: file_missing.
	_ = os.Remove(ex.Files[0].File)
	res, _ = e.run(t, reader, "audit.verify", map[string]any{})
	if v := res.Output.(AuditVerifyOutput); v.Status != "mismatch" || v.Problems[0].Problem != "file_missing" {
		t.Fatalf("verify after delete = %+v", v)
	}
	_ = ctx
}

func TestPolicyGetDescribesKey(t *testing.T) {
	e := newAdminEnv(t)
	k := policy.Key{ID: "key:triage", Name: "triage", Role: "reader", Scopes: []policy.Scope{policy.ScopeMailSearch}, DryRunDefault: true,
		ExpiresAt: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)}
	res, err := e.run(t, k, "policy.get", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	o := res.Output.(PolicyOutput)
	var names []string
	for _, op := range o.Operations {
		names = append(names, op.Name)
	}
	if o.Key != "key:triage" || o.MaxChars != DefaultMaxChars || !o.DraftOnly || !o.DryRunDefault || o.ExpiresAt != "2027-01-01T00:00:00Z" ||
		!slices.Contains(names, "messages.search") || slices.Contains(names, "messages.get") || !slices.Contains(names, "policy.get") {
		t.Fatalf("policy = %+v", o)
	}
	// Auch ein Schlüssel ganz ohne Scopes darf seine Richtlinie lesen (ScopeAny).
	if _, err := e.run(t, policy.Key{ID: "leer"}, "policy.get", map[string]any{}); err != nil {
		t.Fatal(err)
	}
}

// Untrusted-Hülle in den Ausgaben mit Mailinhalt; max_chars kürzt.
func TestEnvelopeAndBudgets(t *testing.T) {
	e := newMirrorEnv(t)
	id := e.seed(t)
	out, err := e.run("messages.get", map[string]any{"id": id, "max_chars": 2}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(MessageOutput)
	if !m.Untrusted || m.Source != "email" || m.Notice != UntrustedNotice || *m.Text != "Te" || !m.Truncated ||
		len(m.Attachments) != 3 || m.Auth.SPF != "none" {
		t.Fatalf("messages.get = %+v", m)
	}
	out, _ = e.run("threads.get", map[string]any{"message": id, "text": true, "max_chars_per_message": 1, "max_messages": 1}, "", false)
	if th := out.(ThreadOutput); !th.Untrusted || *th.Messages[0].Text != "T" || !th.Messages[0].Truncated || th.MessagesOmitted != 0 {
		t.Fatalf("threads.get = %+v", th)
	}
	out, _ = e.run("attachments.list", map[string]any{"message": id}, "", false)
	if al := out.(AttachmentsListOutput); !al.Untrusted || al.Attachments[0].SHA256 != "sha-att" {
		t.Fatalf("attachments.list = %+v", al)
	}
	atts, _ := e.st.AttachmentsByMessage(context.Background(), id)
	out, _ = e.run("attachments.text", map[string]any{"id": atts[0].ID, "max_chars": 5}, "", false)
	if at := out.(AttachmentTextOutput); !at.Untrusted || at.Text != "Notiz" || !at.Truncated {
		t.Fatalf("attachments.text = %+v", at)
	}
	out, _ = e.run("messages.search", map[string]any{"query": "is:read", "mailbox": "inbox"}, "", false)
	if so := out.(SearchOutput); !so.Untrusted || so.Source != "email" {
		t.Fatalf("messages.search = %+v", so)
	}
	// Eingaben außerhalb des Schemas weist der Executor ab (vor dem Handler).
	var ie *InputError
	if _, err := e.run("messages.get", map[string]any{"id": id, "max_chars": 0}, "", false); !errors.As(err, &ie) {
		t.Fatalf("max_chars 0: %v", err)
	}
	if _, err := e.run("messages.get", map[string]any{"id": "eins"}, "", false); !errors.As(err, &ie) {
		t.Fatalf("id as string: %v", err)
	}
	if ev := e.au.Events(); ev[len(ev)-1].Reason != "invalid_input" || ev[len(ev)-1].Result != audit.ResultError {
		t.Fatalf("invalid input audit = %+v", ev[len(ev)-1])
	}
}

func TestParseAuthResults(t *testing.T) {
	got := parseAuthResults("mx.example.org; spf=pass smtp.mailfrom=example.org; dkim=fail header.d=example.org; dmarc=none\n" +
		"evil.example.net; spf=pass; dkim=pass; dmarc=pass")
	if got != (AuthOutput{SPF: "pass", DKIM: "fail", DMARC: "none"}) {
		t.Fatalf("auth = %+v (only the topmost header counts)", got)
	}
	if got := parseAuthResults(""); got != (AuthOutput{SPF: "none", DKIM: "none", DMARC: "none"}) {
		t.Fatalf("empty = %+v", got)
	}
}

// Ein engeres Schema der Oberfläche (MCP: limit ≤ 100) prüft der Executor
// wie das Schema der Operation: abgelehnt, aber auditiert.
func TestExecutorNarrowSchema(t *testing.T) {
	e := newMirrorEnv(t)
	narrow, err := ResolveSchema(json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","maximum":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"query": "Hallo", "limit": 2})
	_, err = e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorAgent, Operation: "messages.search", Input: b, Narrow: narrow})
	var ie *InputError
	if !errors.As(err, &ie) {
		t.Fatalf("narrow schema not applied: %v", err)
	}
	if ev := e.au.Events(); ev[len(ev)-1].Reason != "invalid_input" {
		t.Fatalf("not audited: %+v", ev[len(ev)-1])
	}
	b, _ = json.Marshal(map[string]any{"query": "Hallo", "limit": 1})
	if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorAgent, Operation: "messages.search", Input: b, Narrow: narrow}); err != nil {
		t.Fatal(err)
	}
}
