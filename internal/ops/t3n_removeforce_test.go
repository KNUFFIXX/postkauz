package ops

// Nachbesserung T3 (Wenn Zeit, Nachprüfung G2-6): accounts.remove mit
// force und Schlüssel ist eine Eingabegrenze nach Schlüssel (wie target_role_human_only bei move) und
// gehört in die Policy (A9, ADR-004 Nachtrag T1b): genau ein Audit-Eintrag
// denied, kein started, kein Handler-Aufruf.

import (
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

func TestT3NAccountsRemoveForceWithKeyDeniedByPolicy(t *testing.T) {
	e := newContractEnv(t)
	x, au := actionExec(t, e)
	adminKey := policy.Key{ID: "key:adm", Name: "adm", Scopes: policy.Roles["admin"]}
	_, err := execJSON(t, x, adminKey, "accounts.remove", `{"name":"leer","force":true}`, false)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("want denied, got %v", err)
	}
	var pe *PolicyError
	if !errors.As(err, &pe) {
		t.Errorf("denied in the handler (DeniedError), not by the policy: %v", err)
	}
	var evs []audit.Event
	for _, ev := range au.Events() {
		if ev.Operation == "accounts.remove" {
			evs = append(evs, ev)
		}
	}
	started := 0
	for _, ev := range evs {
		t.Logf("audit: result=%s reason=%s", ev.Result, ev.Reason)
		if ev.Result == audit.ResultStarted {
			started++
		}
	}
	if len(evs) != 1 || started != 0 {
		t.Errorf("want exactly one audit entry (denied) and no started; got %d entries, %d started", len(evs), started)
	}
}
