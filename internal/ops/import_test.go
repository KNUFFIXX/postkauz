package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

type fakeImporter struct {
	got   ImportOptions
	acct  string
	calls int
	err   error
}

func (f *fakeImporter) Import(_ context.Context, a store.Account, o ImportOptions, progress func(Progress)) (ImportOutput, error) {
	f.got, f.acct = o, a.Name
	f.calls++
	if progress != nil {
		progress(Progress{Event: "import_progress", Source: "INBOX", Unit: "bytes", Done: 10, Total: 20})
	}
	if f.err != nil {
		return ImportOutput{}, f.err
	}
	return ImportOutput{New: 2, Existing: 1, Skipped: 1, Sources: []ImportSource{{Path: "INBOX", Folder: "INBOX", Mode: "content",
		New: 2, Existing: 1, Skipped: 1}}}, nil
}

func TestImportRun(t *testing.T) {
	e := newMirrorEnv(t)
	imp := &fakeImporter{}
	if err := e.x.Registry.Register(ImportRun(e.st, imp)); err != nil {
		t.Fatal(err)
	}
	const secretPath = "/home/Erika-Musterfrau/Thunderbird/Profile/ImapMail/imap.example.org"
	var events []Progress
	b, _ := json.Marshal(ImportRunInput{Format: "mbox", Path: secretPath, Mailbox: "Archiv", LabelsFrom: []string{"/x"}, Rescan: true})
	res, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "import.run", Input: b,
		Progress: func(p Progress) { events = append(events, p) }})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(ImportOutput)
	if out.Account != "demo" || out.Format != "mbox" || out.New != 2 || out.DryRun || imp.acct != "demo" ||
		imp.got.Path != secretPath || imp.got.Mailbox != "Archiv" || !imp.got.Rescan || len(imp.got.LabelsFrom) != 1 {
		t.Fatalf("out = %+v, importer = %+v", out, imp.got)
	}
	if len(events) != 1 || events[0].Source != "INBOX" {
		t.Fatalf("progress = %+v", events)
	}
	// Audit: Konto und Feldnamen, nie der Pfad (er kann Namen enthalten).
	ev := e.au.Events()
	last := ev[len(ev)-1]
	if last.Operation != "import.run" || last.Scope != string(policy.ScopeAdmin) || strings.Contains(string(last.ParamsRedacted), "Musterfrau") ||
		!strings.Contains(string(last.ParamsRedacted), "path") {
		t.Fatalf("audit = %+v %s", last, last.ParamsRedacted)
	}
	// Trockenlauf kommt beim Runner an.
	dry := true
	if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "import.run", Input: b, DryRun: &dry}); err != nil || !imp.got.DryRun {
		t.Fatalf("dry run: %v %+v", err, imp.got)
	}
	// Eingabefehler, ohne den Runner zu rufen.
	calls := imp.calls
	for _, in := range []string{`{"format":"pst","path":"/x"}`, `{"format":"mbox"}`, `{"format":"mbox","path":""}`,
		`{"format":"mbox","path":"/x","mailbox":"/"}`, `{"format":"mbox","path":"/x","account":"Nope_1"}`, `{"format":"mbox","path":"/x","bogus":1}`} {
		_, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "import.run", Input: json.RawMessage(in)})
		var ie *InputError
		if !errors.As(err, &ie) {
			t.Errorf("%s: want input error, got %v", in, err)
		}
	}
	if imp.calls != calls {
		t.Fatal("runner called for invalid input")
	}
	// Agenten-Rollen haben admin nicht: verweigert.
	for _, role := range []string{"reader", "assistant"} {
		_, err := e.x.Execute(context.Background(), Request{Key: policy.Key{Scopes: policy.Roles[role]}, Operation: "import.run", Input: b})
		if !errors.Is(err, ErrDenied) {
			t.Fatalf("%s: %v", role, err)
		}
	}
	// Fehler des Runners kommen durch.
	imp.err = &InputError{Msg: "no mbox files found"}
	if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "import.run", Input: b}); err == nil {
		t.Fatal("runner error lost")
	}
}
