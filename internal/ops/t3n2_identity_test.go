package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Zweite Nachbesserung T3, Punkt 9 (B3 der Nachprüfung): Punkt 15 der
// ersten Nachbesserung gilt auch für Identitäten. identities.add erkennt
// eine andere Gmail-Schreibweise derselben Adresse als Dublette; die Wahl
// der Identität per Adresse (drafts.create, drafts.style) findet sie über
// mailaddr.Key, wenn die exakte Schreibweise fehlt. Erfundene Adressen; die
// Gmail-Domains stehen getrennt (PII-Gate).
func TestT3N2IdentityGmailSpellings(t *testing.T) {
	const gm, ggm = "@gmail.com", "@googlemail.com"
	e := newDraftEnv(t, false)
	added := e.must(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "pktestowner" + gm}).(IdentityResultOutput)
	for _, spelling := range []string{"PK.Test.Owner" + gm, "pktestowner+news" + ggm, " PKTESTOWNER" + gm} {
		_, err := e.do(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": spelling}, false)
		var ie *InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), "already has identity") {
			t.Errorf("identities.add %q next to pktestowner%s: %v", spelling, gm, err)
		}
	}
	// Andere Anbieter: Punkte und „+“ können verschiedene Postfächer sein.
	e.must(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "pk.test@example.net"})
	e.must(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "pktest@example.net"})

	out, err := e.do(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"},
		"identity": "pk.testowner" + ggm}, false)
	if err != nil {
		t.Fatalf("drafts.create with another spelling of an own identity: %v", err)
	}
	if from := out.(DraftsWriteOutput).Preview.From; from != "pktestowner"+gm {
		t.Errorf("from = %q, want the spelling of the identity", from)
	}
	for _, in := range []map[string]any{{"account": "demo", "identity": "PK.Test.Owner+x" + gm}, {"identity": "pk.test.owner" + ggm}} {
		st, err := e.do(t, assistant, "drafts.style", in, false)
		if err != nil {
			t.Fatalf("drafts.style %v: %v", in, err)
		}
		if got := st.(DraftsStyleOutput); got.IdentityID != added.Identity.ID {
			t.Errorf("drafts.style %v chose identity %d, want %d", in, got.IdentityID, added.Identity.ID)
		}
	}
}

// Erst exakt, dann nach Key: Hat ein Konto aus der Zeit vor der Dubletten-
// prüfung zwei Schreibweisen derselben Gmail-Adresse als Identitäten,
// gewinnt die exakte; eine dritte Schreibweise ist mehrdeutig und verlangt
// die ID. Klein geschrieben wird nur ASCII (Kelvin-Zeichen U+212A ist kein
// „k“).
func TestT3N2IdentityExactFirstThenKey(t *testing.T) {
	const gm = "@gmail.com"
	ctx := context.Background()
	e := newDraftEnv(t, false)
	acct, err := e.st.QueryIntForTest(`SELECT id FROM accounts WHERE name = 'demo'`)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, addr := range []string{"zweit.konto" + gm, "zweitkonto" + gm, "kunde@example.org"} {
		id, err := e.st.AddIdentity(ctx, store.Identity{AccountID: int64(acct), Email: addr, ReplyPrefix: "Re: ", ForwardPrefix: "Fwd: "})
		if err != nil {
			t.Fatal(err)
		}
		ids[addr] = id
	}
	for _, addr := range []string{"zweit.konto" + gm, "ZweitKonto" + gm} {
		out, err := e.do(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "identity": addr}, false)
		if err != nil {
			t.Fatalf("drafts.create identity %q: %v", addr, err)
		}
		if from, want := out.(DraftsWriteOutput).Preview.From, strings.ToLower(addr); from != want {
			t.Errorf("identity %q: from = %q, want the exact spelling %q", addr, from, want)
		}
		st, err := e.do(t, assistant, "drafts.style", map[string]any{"identity": addr}, false)
		if err != nil {
			t.Fatalf("drafts.style %q: %v", addr, err)
		}
		if got := st.(DraftsStyleOutput).IdentityID; got != ids[strings.ToLower(addr)] {
			t.Errorf("drafts.style %q chose identity %d, want %d", addr, got, ids[strings.ToLower(addr)])
		}
	}
	for _, op := range []string{"drafts.create", "drafts.style"} {
		in := map[string]any{"account": "demo", "identity": "zweit.kon.to+x" + gm}
		if op == "drafts.create" {
			in["kind"] = "new"
		}
		_, err := e.do(t, assistant, op, in, false)
		var ie *InputError
		if !errors.As(err, &ie) || !strings.Contains(err.Error(), "choose one by ID") {
			t.Errorf("%s with an ambiguous spelling: %v", op, err)
		}
	}
	// Kelvin-Zeichen: eine andere Adresse, keine Schreibweise von kunde@.
	_, err = e.do(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "identity": "Kunde@example.org"}, false)
	var ie *InputError
	if !errors.As(err, &ie) || !strings.Contains(err.Error(), "unknown identity") {
		t.Errorf("identity with KELVIN SIGN: %v (want unknown identity)", err)
	}
	if _, err := e.do(t, assistant, "drafts.style", map[string]any{"identity": "Kunde@example.org"}, false); err == nil {
		t.Errorf("drafts.style found an identity for an address with KELVIN SIGN")
	}
}
