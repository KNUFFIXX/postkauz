package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Dritte Nachbesserung T3, Punkt 5: PrepareRestored setzt meta.restored_at,
// mit neuer Kennung und mit --keep-identity; eine nie wiederhergestellte
// Datenbank hat den Merker nicht.
func TestT3N3PrepareRestoredSetsMarker(t *testing.T) {
	for _, keep := range []bool{false, true} {
		ctx := context.Background()
		s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if r, err := s.Restored(ctx); err != nil || r {
			t.Fatalf("fresh database: Restored = %v, %v", r, err)
		}
		p := RestorePrep{Now: time.Now()}
		if !keep {
			p.DataDirID = strings.Repeat("cd", 16)
			p.CredentialRef = func(id, name string) string { return "account/" + id + "/" + name }
		}
		if _, err := s.PrepareRestored(ctx, p); err != nil {
			t.Fatal(err)
		}
		if r, err := s.Restored(ctx); err != nil || !r {
			t.Errorf("keep identity %v: Restored = %v, %v after PrepareRestored", keep, r, err)
		}
	}
}
