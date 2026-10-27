// Erzeugt die Testdaten für den Pflichttest A1 (TestMigration0018AuditVerify,
// Teil T1b) mit dem Stand T1a (main f536810, Schema 17): 30 Audit-Einträge
// über drei Tage, dazu ein Export und eine Prüfung mit diesem Stand.
//
// Neu erzeugen (nur falls nötig; die Daten sollen den alten Stand belegen):
//
//	git worktree add /tmp/wt17 f536810
//	mkdir /tmp/wt17/cmd/genfixture && cp gen/main.go /tmp/wt17/cmd/genfixture/
//	cd /tmp/wt17 && go run ./cmd/genfixture /tmp/pkfixture
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
	x := &ops.Executor{Registry: reg, Audit: w}
	human := policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}
	in, _ := json.Marshal(map[string]string{"to": filepath.Join(out, "export")})
	_, err = x.Execute(ctx, ops.Request{Key: human, Actor: audit.ActorHuman, Operation: "audit.export", Input: in})
	must(err)
	res, err := x.Execute(ctx, ops.Request{Key: human, Actor: audit.ActorHuman, Operation: "audit.verify", Input: json.RawMessage(`{}`)})
	must(err)
	fmt.Printf("%+v\n", res.Output)
	must(st.Close())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
