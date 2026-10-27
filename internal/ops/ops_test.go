package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

func newTestOp(name string, scope policy.Scope, risk policy.Risk, h Handler) Operation {
	return Operation{
		Name: name, Scope: scope, Risk: risk, Summary: "test",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:      h,
	}
}

func TestRegistryRegisterGetList(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"b.two", "a.one"} {
		if err := r.Register(newTestOp(n, policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Register(newTestOp("a.one", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })); err == nil {
		t.Error("duplicate name must be rejected")
	}
	if _, err := r.Get("nope.nope"); !errors.Is(err, ErrUnknownOperation) {
		t.Errorf("Get unknown = %v", err)
	}
	list := r.List()
	if len(list) != 2 || list[0].Name != "a.one" || list[1].Name != "b.two" {
		t.Errorf("List must be sorted by name, got %v", list)
	}
}

func TestOperationValidate(t *testing.T) {
	ok := newTestOp("a.b", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid op rejected: %v", err)
	}
	bad := map[string]func(o *Operation){
		"name without dot":    func(o *Operation) { o.Name = "list" },
		"upper case name":     func(o *Operation) { o.Name = "Accounts.List" },
		"missing scope":       func(o *Operation) { o.Scope = "" },
		"risk out of range":   func(o *Operation) { o.Risk = 9 },
		"missing handler":     func(o *Operation) { o.Handler = nil },
		"missing summary":     func(o *Operation) { o.Summary = "" },
		"input not an object": func(o *Operation) { o.InputSchema = json.RawMessage(`{"type":"string"}`) },
		"output not json":     func(o *Operation) { o.OutputSchema = json.RawMessage(`nope`) },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			o := ok
			mutate(&o)
			if err := o.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestExecutorChain(t *testing.T) {
	called := 0
	reg := NewRegistry()
	for _, op := range []Operation{
		newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { called++; return map[string]any{"ok": true}, nil }),
		newTestOp("m.move", policy.ScopeMailMove, policy.R2, func(_ context.Context, c Call) (any, error) { called++; return map[string]any{"dry": c.DryRun}, nil }),
		newTestOp("m.trash", policy.ScopeMailTrash, policy.R3, func(context.Context, Call) (any, error) { called++; return nil, nil }),
		newTestOp("m.fail", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, errors.New("boom") }),
	} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au, Now: func() time.Time { return time.Unix(0, 0) }}
	operator := policy.Key{ID: "op", Scopes: policy.Roles["operator"]}
	reader := policy.Key{ID: "rd", Scopes: policy.Roles["reader"]}
	ctx := context.Background()

	// 1. erlaubt: Handler läuft, Audit hat genau zwei Einträge: "started"
	// und den Abschluss "ok" mit Verweis auf den Start (A1 Variante A)
	if _, err := x.Execute(ctx, Request{Key: reader, Actor: audit.ActorAgent, Operation: "m.read"}); err != nil {
		t.Fatal(err)
	}
	if evs := au.Events(); called != 1 || len(evs) != 2 || evs[0].Result != audit.ResultStarted || evs[0].RefID != 0 ||
		evs[1].Result != audit.ResultOK || evs[1].RefID != 1 {
		t.Fatalf("allowed path: called=%d events=%+v", called, evs)
	}

	// 2. verweigert: Handler läuft nicht, Audit "denied"
	_, err := x.Execute(ctx, Request{Key: reader, Actor: audit.ActorAgent, Operation: "m.move"})
	if !errors.Is(err, ErrDenied) || called != 1 {
		t.Fatalf("deny path: err=%v called=%d", err, called)
	}
	if last := au.Events()[len(au.Events())-1]; last.Result != audit.ResultDenied || last.KeyID != "rd" {
		t.Fatalf("deny audit = %+v", last)
	}

	// 3. R3: Freigabe nötig, Handler läuft nicht
	_, err = x.Execute(ctx, Request{Key: operator, Actor: audit.ActorAgent, Operation: "m.trash"})
	if !errors.Is(err, ErrNeedsApproval) || called != 1 {
		t.Fatalf("approval path: err=%v called=%d", err, called)
	}

	// 4. Trockenlauf aus dem Schlüssel-Standard: Handler sieht DryRun=true
	dryKey := policy.Key{ID: "dk", Scopes: policy.Roles["operator"], DryRunDefault: true}
	res, err := x.Execute(ctx, Request{Key: dryKey, Operation: "m.move"})
	if err != nil || res.Output.(map[string]any)["dry"] != true {
		t.Fatalf("dry-run default: res=%+v err=%v", res, err)
	}
	f := false
	res, err = x.Execute(ctx, Request{Key: dryKey, Operation: "m.move", DryRun: &f})
	if err != nil || res.Output.(map[string]any)["dry"] != false {
		t.Fatalf("explicit dry_run=false: res=%+v err=%v", res, err)
	}

	// 5. Handler-Fehler wird durchgereicht und zusätzlich auditiert
	before := len(au.Events())
	if _, err = x.Execute(ctx, Request{Key: reader, Operation: "m.fail"}); err == nil || err.Error() != "boom" {
		t.Fatalf("handler error = %v", err)
	}
	if evs := au.Events(); len(evs) != before+2 || evs[len(evs)-1].Result != audit.ResultError {
		t.Fatalf("error audit missing: %+v", evs[before:])
	}

	// 6. unbekannte Operation
	if _, err = x.Execute(ctx, Request{Key: reader, Operation: "x.y"}); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unknown op err = %v", err)
	}
}

// A02, E2 (minimal in S2.0): Eine HumanOnly-Operation läuft nur für den
// Menschen ohne Schlüssel; mit Schlüssel (jede Rolle, auch admin, auch als
// Trockenlauf) ist sie verweigert, auditiert mit Grund human_only, und der
// Handler läuft nie.
func TestExecutorHumanOnly(t *testing.T) {
	called := 0
	reg := NewRegistry()
	op := newTestOp("m.set", policy.ScopeAdmin, policy.R1, func(context.Context, Call) (any, error) { called++; return map[string]any{}, nil })
	op.HumanOnly = true
	if err := reg.Register(op); err != nil {
		t.Fatal(err)
	}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au}
	yes := true
	for _, role := range []string{"reader", "assistant", "operator", "admin"} {
		for _, dry := range []*bool{nil, &yes} {
			k := policy.Key{ID: "key:" + role, Scopes: policy.Roles[role]}
			_, err := x.Execute(context.Background(), Request{Key: k, Actor: audit.ActorAgent, Operation: "m.set", DryRun: dry})
			var pe *PolicyError
			if !errors.Is(err, ErrDenied) || !errors.As(err, &pe) || pe.Decision.Reason != "human_only" {
				t.Fatalf("%s: err = %v", role, err)
			}
			if last := au.Events()[len(au.Events())-1]; last.Result != audit.ResultDenied || last.Reason != "human_only" || last.KeyID != k.ID {
				t.Fatalf("%s: audit = %+v", role, last)
			}
		}
	}
	if called != 0 {
		t.Fatalf("handler ran %d times for API keys", called)
	}
	if _, err := x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "m.set"}); err != nil || called != 1 {
		t.Fatalf("human: err=%v called=%d", err, called)
	}
}

type failingAudit struct{}

func (failingAudit) Append(context.Context, audit.Event) error { return errors.New("disk full") }

func TestExecutorFailsClosedWhenAuditFails(t *testing.T) {
	called := false
	reg := NewRegistry()
	_ = reg.Register(newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { called = true; return nil, nil }))
	x := &Executor{Registry: reg, Audit: failingAudit{}}
	_, err := x.Execute(context.Background(), Request{Key: policy.Key{Scopes: []policy.Scope{policy.ScopeMailRead}}, Operation: "m.read"})
	if err == nil || called {
		t.Fatalf("handler must not run without audit: err=%v called=%v", err, called)
	}
}

// --- Vertragstest (ADR-004) ----------------------------------------------------
//
// Jede Operation des Katalogs läuft mit jeder Rolle und jeder Trockenlauf-
// Variante durch den Executor. Geprüft wird die Kette Scope → Policy → Audit →
// Handler: Die Policy entscheidet (Erwartung unabhängig von policy.Check
// formuliert), genau ein Audit-Eintrag entsteht, der Handler läuft nur bei
// Erlaubnis, ein Trockenlauf (und jede R0-Operation) verändert nichts, und
// die Ausgabe trägt die Pflichtfelder ihres OutputSchema. Eine neue Operation
// im Katalog ohne Zeile in contractInputs lässt den Test scheitern.

// contractInputs: je Operation eine gültige Eingabe, mit der der Handler bei
// Erlaubnis bis zu seiner Wirkung kommt (Umgebung: newContractEnv).
var contractInputs = map[string]struct {
	input  string
	secret secrets.Secret
}{
	"accounts.list":      {input: `{}`},
	"accounts.add":       {input: `{"name":"neu","host":"127.0.0.1","port":11143,"tls":"none","username":"u@example.invalid","insecure_test_server":true}`, secret: canary},
	"accounts.remove":    {input: `{"name":"leer"}`},
	"accounts.set":       {input: `{"name":"demo","aliases":["Alias@Example.org","zweit@example.net"],"replace_password":true}`, secret: canary},
	"accounts.test":      {input: `{"name":"demo"}`},
	"sync.run":           {input: `{"account":"demo"}`},
	"import.run":         {input: `{"account":"demo","format":"mbox","path":"/tmp/x","mailbox":"Archiv","labels_from":["/tmp/l"],"rescan":true,"no_extract":true}`},
	"mailboxes.list":     {input: `{"account":"demo"}`},
	"mailboxes.overview": {input: `{"account":"demo","mailbox":1,"since":"3650d","limit":5}`},
	"messages.get":       {input: `{"message_id":"a@example.org","account":"demo","headers":true}`},
	"messages.search":    {input: `{"query":"Hallo -Werbung has:attachment","account":"demo","limit":5,"explain":true}`},
	"threads.get":        {input: `{"message":1,"text":true}`},
	"attachments.list":   {input: `{"message":1}`},
	"attachments.text":   {input: `{"id":2,"message":1,"account":"demo","max_chars":10}`},
	"keys.create":        {input: `{"name":"neu-key","role":"assistant","scopes":["mail:read","mail:search"],"expires_in_days":30}`},
	"keys.list":          {input: `{}`},
	"keys.revoke":        {input: `{"name":"alt"}`},
	"audit.list":         {input: `{"since":"7d","limit":5}`},
	"audit.export":       {input: `{}`},
	"audit.verify":       {input: `{}`},
	"policy.get":         {input: `{}`},
	// Entwürfe (S2.3): Umgebung mit Identitäten 1 (Standard) und 2, Entwurf
	// 1 (draft) und Entwurf 2 (pending_approval, Freigabe 1).
	"drafts.create": {input: `{"account":"demo","kind":"reply","in_reply_to":1,"identity":"demo@example.org","to":["Zweit <zweit@example.net>"],` +
		`"cc":["c@example.net"],"bcc":["b@example.net"],"subject":"Antwort","body_markdown":"**Hallo**","attachments":[{"attachment_id":1}],` +
		`"quote":true,"signature":true,"include_attachments":false}`},
	"drafts.update":          {input: `{"draft_id":1,"to":["neu@example.org"],"subject":"Neu","body_html":"<p>Neuer Text</p>","quote":false,"signature":false}`},
	"drafts.get":             {input: `{"draft_id":1}`},
	"drafts.list":            {input: `{"account":"demo","status":["draft","pending_approval"],"limit":5}`},
	"drafts.request_ready":   {input: `{"draft_id":1}`},
	"drafts.approve":         {input: `{"approval_id":1}`},
	"drafts.reject":          {input: `{"approval_id":1,"reason":"zu lang"}`},
	"drafts.abandon":         {input: `{"draft_id":1}`},
	"drafts.mark_sent":       {input: `{"draft_id":2}`},
	"approvals.get":          {input: `{"approval_id":1}`},
	"drafts.style":           {input: `{"identity":"demo@example.org","account":"demo","max_chars":100}`},
	"identities.add":         {input: `{"account":"demo","name":"Drei","email":"drei@example.org","reply_to":"antwort@example.org","signature_text":"Gruß","default":true}`},
	"identities.list":        {input: `{"account":"demo"}`},
	"identities.set_default": {input: `{"id":2}`},
	"identities.remove":      {input: `{"id":2}`},
}

// memBlobs ist ein Blob-Store im Speicher: die Blobs von testBlobs und
// alles, was Put ablegt (puts zählt die Schreibzugriffe).
type memBlobs struct {
	m    map[string]string
	puts int
	read int64 // gelesene Bytes (K2)
}

func newMemBlobs() *memBlobs {
	b := &memBlobs{m: map[string]string{}}
	for k, v := range testBlobs {
		// Unter dem Testnamen (wie in testBlobs) und unter dem echten
		// SHA-256 (so legt der Sync Anhänge ab; Entwürfe merken sich ihn).
		sum := sha256.Sum256([]byte(v))
		b.m[k], b.m[hex.EncodeToString(sum[:])] = v, v
	}
	return b
}

func (b *memBlobs) Get(sha string) (io.ReadCloser, error) {
	rc, err := mapBlobs(b.m).Get(sha)
	if err != nil {
		return nil, err
	}
	return countingReader{rc, &b.read}, nil
}

// countingReader zählt die gelesenen Bytes.
type countingReader struct {
	io.ReadCloser
	n *int64
}

func (c countingReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	*c.n += int64(n)
	return n, err
}

func (b *memBlobs) Put(r io.Reader) (blob.Info, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return blob.Info{}, err
	}
	sum := sha256.Sum256(data)
	h := hex.EncodeToString(sum[:])
	_, existed := b.m[h]
	b.m[h] = string(data)
	b.puts++
	return blob.Info{SHA256: h, Size: int64(len(data)), Existed: existed}, nil
}

// seedDrafts legt Identitäten und Entwürfe für die Entwurfsoperationen an
// (direkt über den Dienst, nicht über den Executor).
func seedDrafts(t *testing.T, st *store.SQLite, acct int64) {
	t.Helper()
	ctx := context.Background()
	if _, err := st.AddIdentity(ctx, store.Identity{AccountID: acct, Name: "Demo", Email: "demo@example.org",
		SignatureText: "Gruß"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddIdentity(ctx, store.Identity{AccountID: acct, Name: "Zweit", Email: "zweit@example.org"}); err != nil {
		t.Fatal(err)
	}
	svc := &drafts.Service{Store: st}
	for i := 0; i < 2; i++ {
		if _, err := svc.Create(ctx, drafts.NewDraft{AccountID: acct, Kind: drafts.KindNew, CreatedBy: "cli",
			Content: drafts.Content{To: []store.Addr{{Address: "a@example.org"}}, Subject: "Entwurf", BodyText: "Text"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RequestReady(ctx, 2, "cli", drafts.HookPass, drafts.Checked{}); err != nil {
		t.Fatal(err)
	}
}

// bindApproval trägt in Freigabe 1 den Hash der aus Entwurf 2 gebauten
// Nachricht ein, wie ihn drafts.request_ready speichert (W1), damit
// drafts.approve sie freigeben kann.
func bindApproval(t *testing.T, d Deps) {
	t.Helper()
	ctx := context.Background()
	o := newDraftOps(d)
	dr, err := o.svc.Store.Draft(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	acc, err := o.accountByID(ctx, dr.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := o.rebuild(ctx, acc, dr)
	if err != nil {
		t.Fatal(err)
	}
	if err := o.svc.Store.ExecForTest(`UPDATE approvals SET message_hash = ? WHERE id = 1`, compose.MessageHash(msg.Raw)); err != nil {
		t.Fatal(err)
	}
}

// contractImporter meldet Importe, die schreiben dürften (DryRun=false).
type contractImporter struct{ fakeImporter }

// contractRunner zählt Syncs, die schreiben dürften (DryRun=false).
type contractRunner struct{ fakeRunner }

func (c *contractRunner) Sync(ctx context.Context, a store.Account, pw secrets.Secret, o SyncOptions, p func(Progress)) (SyncOutput, error) {
	return c.fakeRunner.Sync(ctx, a, pw, o, p)
}

type contractEnv struct {
	*mirrorEnv
	runner   *contractRunner
	importer *contractImporter
	blobs    *memBlobs
	deps     Deps
}

// newContractEnv: Konto demo mit Passwort, einem Ordner und einer Nachricht;
// Konto leer ohne Nachrichten (für accounts.remove).
func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	m := newMirrorEnv(t)
	m.seed(t)
	in := demoInput()
	in["name"] = "leer"
	if _, err := m.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	m.prober.res = ProbeResult{Capabilities: []string{"IMAP4rev1"}, SyncStrategy: "uid-scan", CapabilitiesJSON: `{"imap4rev1":true}`}
	// Schlüssel alt (für keys.revoke) und ein Audit-Eintrag in der
	// Datenbank (für audit.export/verify).
	if _, err := m.st.CreateAPIKey(context.Background(), store.APIKey{Name: "alt", KeyHash: HashKey("x"), Role: "reader",
		Scopes: []string{"mail:read"}, DryRunDefault: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.st.AuditWriter().Append(context.Background(), audit.Event{TS: time.Now(), ActorType: audit.ActorHuman,
		Operation: "accounts.list", Result: audit.ResultOK}); err != nil {
		t.Fatal(err)
	}
	seedDrafts(t, m.st, m.acct)
	r := &contractRunner{}
	imp := &contractImporter{}
	blobs := newMemBlobs()
	e := &contractEnv{mirrorEnv: m, runner: r, importer: imp, blobs: blobs, deps: Deps{Accounts: m.st, Secrets: m.sec, Prober: m.prober,
		Mailboxes: m.st, Messages: m.st, Blobs: testBlobs, Runner: r, Importer: imp, Search: m.st, Extractor: extract.New(extract.Options{}),
		Keys: m.st, Audit: m.st, AuditExportDir: t.TempDir(), Drafts: &drafts.Service{Store: m.st}, DraftBlobs: blobs,
		Draft: DraftConfig{AttachDir: t.TempDir(), Operator: "Testperson"}}}
	bindApproval(t, e.deps)
	return e
}

// fingerprint fasst den lokalen Zustand zusammen: alle Tabellen der
// Datenbank, den Schlüsselbund und schreibende Sync-Aufrufe.
func (e *contractEnv) fingerprint(t *testing.T) string {
	t.Helper()
	db := e.st.DBForTest()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	_ = rows.Close()
	var b strings.Builder
	for _, tb := range tables {
		r, err := db.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			continue // virtuelle Hilfstabellen ohne SELECT *
		}
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = r.Scan(ptrs...)
			fmt.Fprintf(&b, "%s%v\n", tb, vals)
		}
		_ = r.Close()
	}
	e.sec.mu.Lock()
	keys := make([]string, 0, len(e.sec.m))
	for k, v := range e.sec.m {
		keys = append(keys, k+"="+string(v))
	}
	e.sec.mu.Unlock()
	sort.Strings(keys)
	fmt.Fprintf(&b, "secrets %v\n", keys)
	if e.runner.acct != "" && !e.runner.got.DryRun {
		b.WriteString("sync wrote\n")
	}
	if e.importer.acct != "" && !e.importer.got.DryRun {
		b.WriteString("import wrote\n")
	}
	fmt.Fprintf(&b, "blob puts %d\n", e.blobs.puts)
	return b.String()
}

// spyRegistry registriert den Katalog mit Handlern, die ihren Aufruf melden.
func spyRegistry(t *testing.T, d Deps, ran map[string]int) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, op := range Catalog(d) {
		h, name := op.Handler, op.Name
		op.Handler = func(ctx context.Context, c Call) (any, error) {
			ran[name]++
			return h(ctx, c)
		}
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// requiredFields liest die Pflichtfelder der obersten Ebene eines Schemas.
func requiredFields(t *testing.T, schema json.RawMessage) (required []string, props map[string]any) {
	t.Helper()
	var s struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatal(err)
	}
	return s.Required, s.Properties
}

func TestContractEveryOperationRoleAndDryRun(t *testing.T) {
	ops := Catalog(Deps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name] = true
		if _, ok := contractInputs[op.Name]; !ok {
			t.Errorf("operation %s has no row in contractInputs; add a valid sample input", op.Name)
		}
	}
	for n := range contractInputs {
		if !names[n] {
			t.Errorf("contractInputs has a row for %s, which is not in the catalog", n)
		}
	}
	if reg, err := NewDefaultRegistry(Deps{}); err != nil || len(reg.List()) != len(ops) {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}

	// Rollen als API-Schlüssel, dazu "human": der Mensch an der CLI ohne
	// Schlüssel (admin-Scopes, Human), der als Einziger HumanOnly-Operationen
	// ausführen darf (A02, E2).
	roles := []string{"none", "human"}
	for r := range policy.Roles {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	yes, no := true, false
	modes := []struct {
		name       string
		flag       *bool
		keyDefault bool
	}{
		{"default", nil, false},
		{"dry-run=false", &no, false},
		{"dry-run", &yes, false},
		{"key-default-dry", nil, true},
		{"key-default-dry+dry-run=false", &no, true},
	}
	if raceDetector {
		// Mit -race dauert die volle Matrix (jede Operation × jede Rolle ×
		// fünf Varianten, je mit frischer Datenbank) auf zwei Kernen über
		// 30 Minuten, zwei Varianten noch über 10 Minuten (gemessen
		// 22.10.2026, Schema 12). Der Race-Detector sucht Wettläufe, nicht
		// fehlende Kombinationen: Jede Operation läuft mit jeder Rolle einmal
		// wirklich (dry-run=false); die volle Matrix läuft ohne -race in
		// make check und im CI-Job test.
		modes = []struct {
			name       string
			flag       *bool
			keyDefault bool
		}{modes[1]}
	}

	for _, op := range ops {
		row := contractInputs[op.Name]
		for _, role := range roles {
			for _, mode := range modes {
				t.Run(op.Name+"/"+role+"/"+mode.name, func(t *testing.T) {
					e := newContractEnv(t)
					ran := map[string]int{}
					au := &audit.Memory{}
					x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: au}
					key := policy.Key{ID: "key-" + role, Scopes: policy.Roles[role], DryRunDefault: mode.keyDefault}
					human := role == "human"
					if human {
						key = policy.Key{ID: "cli", Scopes: policy.Roles["admin"], DryRunDefault: mode.keyDefault, Human: true}
					}

					// Erwartung, unabhängig von policy.Check formuliert
					// (docs/sicherheit.md, Abschnitt 2).
					dry := mode.keyDefault
					if mode.flag != nil {
						dry = *mode.flag
					}
					effDry := dry && op.Risk > policy.R0
					want, wantReason := policy.Allow, ""
					switch {
					case op.HumanOnly && !human:
						want, wantReason = policy.Deny, "human_only"
					case op.Scope != policy.ScopeAny && !slices.Contains(key.Scopes, op.Scope):
						want = policy.Deny
					case op.Risk == policy.R3 && !dry:
						want = policy.NeedsApproval
					}

					before := e.fingerprint(t)
					res, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent,
						Operation: op.Name, Input: json.RawMessage(row.input), DryRun: mode.flag, Secret: row.secret})

					// Policy
					switch want {
					case policy.Deny:
						if !errors.Is(err, ErrDenied) {
							t.Fatalf("want denied, got err=%v", err)
						}
					case policy.NeedsApproval:
						if !errors.Is(err, ErrNeedsApproval) {
							t.Fatalf("want needs_approval, got err=%v", err)
						}
					default:
						if err != nil {
							t.Fatalf("want allowed, got err=%v", err)
						}
						if res.Decision.DryRun != effDry {
							t.Fatalf("decision dry_run=%v, want %v", res.Decision.DryRun, effDry)
						}
					}

					// Handler nur bei Erlaubnis
					if got, wantRan := ran[op.Name], want == policy.Allow; (got == 1) != wantRan || got > 1 {
						t.Fatalf("handler ran %d times, want ran=%v", got, wantRan)
					}

					// Audit (A1 Variante A): genau ein Eintrag bei einer
					// Policy-Ablehnung, genau zwei bei Erlaubnis (started,
					// dann der Abschluss mit Verweis), ohne Geheimnis.
					evs := au.Events()
					wantEvents := 1
					if want == policy.Allow {
						wantEvents = 2
					}
					if len(evs) != wantEvents {
						t.Fatalf("audit events = %d, want %d: %+v", len(evs), wantEvents, evs)
					}
					ev := evs[0]
					wantResult := map[policy.Outcome]audit.Result{policy.Allow: audit.ResultStarted,
						policy.Deny: audit.ResultDenied, policy.NeedsApproval: audit.ResultNeedsApproval}[want]
					if ev.Operation != op.Name || ev.Scope != string(op.Scope) || ev.KeyID != key.ID ||
						ev.ActorType != audit.ActorAgent || ev.Result != wantResult || ev.RefID != 0 {
						t.Fatalf("audit event = %+v, want result %s", ev, wantResult)
					}
					if wantReason != "" && ev.Reason != wantReason {
						t.Fatalf("audit reason = %q, want %q", ev.Reason, wantReason)
					}
					if want == policy.Allow {
						end := evs[1]
						if end.Operation != op.Name || end.KeyID != key.ID || end.RefID != res.AuditID || end.RefID == 0 ||
							end.Result != audit.ResultOK || end.DryRun != effDry || end.TS.Before(ev.TS) {
							t.Fatalf("closing audit event = %+v (start id %d)", end, res.AuditID)
						}
						if ev.DryRun != effDry {
							t.Fatalf("audit dry_run=%v, want %v", ev.DryRun, effDry)
						}
					}
					// ParamsRedacted ([]byte) zusätzlich als Text prüfen:
					// json.Marshal schreibt es als Base64.
					for _, ev := range evs {
						if b, _ := json.Marshal(ev); strings.Contains(string(b), canary) || strings.Contains(string(ev.ParamsRedacted), canary) {
							t.Fatalf("secret in audit event: %s", b)
						}
					}

					// Wirkung: Trockenlauf, R0 und Ablehnung verändern nichts;
					// eine erlaubte schreibende Operation muss wirken (sonst
					// prüft die Beispiel-Eingabe den Trockenlauf nicht).
					after := e.fingerprint(t)
					writes := want == policy.Allow && !effDry && op.Risk > policy.R0
					if !writes && after != before {
						t.Fatalf("state changed although nothing may be written (dry=%v, risk %s)", effDry, op.Risk)
					}
					if writes && after == before {
						t.Fatalf("allowed %s operation without dry run changed nothing; fix the sample input", op.Risk)
					}

					// Ausgabe: Pflichtfelder des OutputSchema, dry_run wie entschieden
					if want != policy.Allow {
						return
					}
					b, err := json.Marshal(res.Output)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(b), canary) {
						t.Fatalf("secret in output: %s", b)
					}
					var out map[string]any
					if err := json.Unmarshal(b, &out); err != nil {
						t.Fatalf("output is not a JSON object: %s", b)
					}
					req, props := requiredFields(t, op.OutputSchema)
					for _, f := range req {
						if _, ok := out[f]; !ok {
							t.Fatalf("output lacks required field %q: %s", f, b)
						}
					}
					if _, ok := props["dry_run"]; ok && out["dry_run"] != effDry {
						t.Fatalf("output dry_run=%v, want %v", out["dry_run"], effDry)
					}
				})
			}
		}
	}
}

// Fail closed für jede Operation: Ohne Audit läuft kein Handler, nichts ändert sich.
func TestContractEveryOperationFailsClosed(t *testing.T) {
	for _, op := range Catalog(Deps{}) {
		t.Run(op.Name, func(t *testing.T) {
			e := newContractEnv(t)
			ran := map[string]int{}
			x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: failingAudit{}}
			row := contractInputs[op.Name]
			before := e.fingerprint(t)
			_, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "adm", Scopes: policy.Roles["admin"]},
				Operation: op.Name, Input: json.RawMessage(row.input), Secret: row.secret})
			if err == nil || ran[op.Name] != 0 || e.fingerprint(t) != before {
				t.Fatalf("err=%v handler ran=%d changed=%v", err, ran[op.Name], e.fingerprint(t) != before)
			}
		})
	}
}

// ctxAudit verhält sich wie der SQLite-Writer: Mit beendetem ctx schreibt er nicht.
type ctxAudit struct{ audit.Memory }

func (c *ctxAudit) Append(ctx context.Context, e audit.Event) error {
	_, err := c.AppendID(ctx, e)
	return err
}

func (c *ctxAudit) AppendID(ctx context.Context, e audit.Event) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Memory.AppendID(ctx, e)
}

// Nach Strg+C (ctx abgebrochen, Handler liefert context.Canceled) muss das
// Fehler-Ereignis trotzdem im Audit stehen.
func TestExecutorAuditsHandlerErrorAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reg := NewRegistry()
	_ = reg.Register(newTestOp("m.sync", policy.ScopeMailSync, policy.R1, func(ctx context.Context, _ Call) (any, error) {
		cancel() // Strg+C während des Laufs
		<-ctx.Done()
		return nil, ctx.Err()
	}))
	au := &ctxAudit{}
	x := &Executor{Registry: reg, Audit: au}
	_, err := x.Execute(ctx, Request{Key: policy.Key{ID: "k", Scopes: policy.Roles["assistant"]}, Operation: "m.sync"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	evs := au.Events()
	if len(evs) != 2 || evs[1].Result != audit.ResultError || evs[1].Reason != "handler_error" {
		t.Fatalf("error event missing after cancel: %+v", evs)
	}
}
