package compose

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// S2.3: Beim Ändern eines gespeicherten Entwurfs sind To/Cc/Bcc die
// vollständigen Listen; nichts wird aus der Ausgangsnachricht nachgeleitet
// (sonst käme ein entfernter Empfänger bei jeder Änderung zurück). Zitat,
// In-Reply-To und References kommen weiter aus der Ausgangsnachricht.
func TestExplicitRecipients(t *testing.T) {
	in := baseInput(KindReplyAll)
	in.ExplicitRecipients = true
	in.To = []string{"erika@example.org"}
	m := mustBuild(t, in)
	if got := addrs(m.To); !slices.Equal(got, []string{"erika@example.org"}) || len(m.Cc) != 0 {
		t.Fatalf("to %v cc %v", got, addrs(m.Cc))
	}
	if m.InReplyTo != "orig.42@example.org" || len(m.References) != 3 {
		t.Fatalf("in-reply-to %q references %v", m.InReplyTo, m.References)
	}
	// Ohne den Schalter wie bisher: abgeleitet plus genannt.
	in.ExplicitRecipients = false
	if m := mustBuild(t, in); len(m.Cc) == 0 {
		t.Fatal("derived cc missing without ExplicitRecipients")
	}
}

// LoadFile folgt denselben Regeln wie ein Dateianhang in Build.
func TestLoadFile(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "attach")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("Inhalt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "geheim.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, name, err := LoadFile("attachments[0]", dir, "ok.txt", 0)
	if err != nil || string(data) != "Inhalt\n" || name != "ok.txt" {
		t.Fatalf("LoadFile = %q %q %v", data, name, err)
	}
	_, _, err = LoadFile("attachments[0]", dir, "../geheim.txt", 0)
	wantReason(t, err, ReasonAttachOutsideDir)
	_, _, err = LoadFile("attachments[0]", dir, "fehlt.txt", 0)
	wantReason(t, err, ReasonAttachNotFound)
	_, _, err = LoadFile("attachments[0]", dir, "ok.txt", 3)
	wantReason(t, err, ReasonTooLarge)
}
