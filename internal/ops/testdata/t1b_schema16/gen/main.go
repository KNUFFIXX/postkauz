// Erzeugt die Testdaten für den zweiten Pflichttest A1
// (TestMigration16AuditVerify, Nachbesserung T1b, A-F5) mit dem Stand rc8
// (99a7cc0, Schema 16, Dauerbetrieb des Betreibers): 30 Audit-Einträge über
// drei Tage, ein Export, dann ein verspäteter Eintrag mit dem Zeitstempel
// eines exportierten Tages, den der zweite Export von rc8 in diesen alten
// Tag neu schreibt, eine Prüfung und zuletzt ein Tag, der nicht exportiert
// ist (offener Tag).
//
// Neu erzeugen (nur falls nötig; die Daten sollen den alten Stand belegen):
//
//	git worktree add /tmp/wt16 99a7cc0
//	mkdir /tmp/wt16/cmd/genfixture && cp gen/main.go /tmp/wt16/cmd/genfixture/
//	cd /tmp/wt16 && go run ./cmd/genfixture /tmp/pkfixture16
//
// danach postkauz.db mit gzip -9 nach postkauz.db.gz und export/ hierher.
// Das Verzeichnis testdata übersetzt go build/vet/test nicht.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

func main() {
	out := os.Args[1]
	ctx := context.Background()
	st, err := store.Open(filepath.Join(out, "postkauz.db"))
	must(err)
	must(st.Migrate(ctx))
	w := st.AuditWriter()
	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	results := []audit.Result{audit.ResultOK, audit.ResultDenied, audit.ResultNeedsApproval, audit.ResultError}
	for i := 0; i < 30; i++ {
		ev := audit.Event{TS: base.Add(time.Duration(i) * 2 * time.Hour), ActorType: audit.ActorAgent, KeyID: "key:assistant-1",
			Scope: "mail:read", Operation: "messages.search", ParamsRedacted: []byte(fmt.Sprintf(`{"query_chars":%d}`, i)),
			Targets: []string{fmt.Sprintf("message:%d", i)}, Result: results[i%4], Reason: "ok", DryRun: i%5 == 0,
			Duration: time.Duration(i) * time.Millisecond, PolicyVersion: int64(i % 3)}
		if i%3 == 0 {
			ev.ActorType, ev.KeyID, ev.Scope, ev.Operation, ev.Targets = audit.ActorSystem, "", "", "drafts.push", nil
		}
		must(w.Append(ctx, ev))
	}
	reg := ops.NewRegistry()
	must(reg.Register(ops.AuditExport(st, "")))
	must(reg.Register(ops.AuditVerify(st)))
	clock := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	x := &ops.Executor{Registry: reg, Audit: w, Now: func() time.Time { return clock }}
	human := policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}
	in, _ := json.Marshal(map[string]string{"to": filepath.Join(out, "export")})
	exec := func(op string, input json.RawMessage) {
		res, err := x.Execute(ctx, ops.Request{Key: human, Actor: audit.ActorHuman, Operation: op, Input: input})
		must(err)
		fmt.Printf("%s: %+v\n", op, res.Output)
	}
	exec("audit.export", in)
	// Verspäteter Eintrag (Abschluss eines Laufs über Mitternacht) mit dem
	// Zeitstempel des 2. Oktober; rc8 schreibt den Tag beim nächsten Export neu.
	must(w.Append(ctx, audit.Event{TS: time.Date(2026, 10, 2, 23, 59, 0, 0, time.UTC), ActorType: audit.ActorHuman, KeyID: "cli",
		Scope: "mail:sync", Operation: "sync.run", ParamsRedacted: []byte(`{"watch":true}`), Targets: []string{"account:demo"},
		Result: audit.ResultOK, Duration: 3 * time.Hour}))
	clock = clock.Add(time.Hour)
	exec("audit.export", in)
	clock = clock.Add(time.Hour)
	exec("audit.verify", json.RawMessage(`{}`))
	// Offener Tag: nach dem letzten Export, nie exportiert.
	for i := 0; i < 3; i++ {
		must(w.Append(ctx, audit.Event{TS: time.Date(2026, 10, 5, 8+i, 0, 0, 0, time.UTC), ActorType: audit.ActorAgent,
			KeyID: "key:assistant-1", Scope: "mail:read", Operation: "messages.get", ParamsRedacted: []byte(`{}`),
			Targets: []string{fmt.Sprintf("message:%d", i)}, Result: audit.ResultOK}))
	}
	must(st.Close())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
