//go:build windows

package export

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Nachbesserung T3, Punkt 11c (nur Windows): Jede abgelegte Datei
// (Anhang, .eml, index.csv, mbox) trägt den Datenstrom Zone.Identifier mit
// Zone 3 („aus dem Internet“). Das Temp-Verzeichnis liegt auf NTFS; ein
// Dateisystem ohne Datenströme ließe den Test scheitern (dann nur Warnung
// im Programm).
func TestT3NZoneIdentifierOnWindows(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "export")
	res := save(t, dir, SaveRequest{Filename: "Rechnung.pdf"})
	if res.Unmarked {
		t.Fatalf("attachment not marked")
	}
	a := newArchive()
	a.add("INBOX", 1, []byte("Subject: eins\r\n\r\nx\r\n"), Item{From: "a@example.org", Received: when})
	for _, format := range []string{FormatMbox, FormatEML} {
		rep, err := Run(context.Background(), a.options(format, filepath.Join(dir, format)))
		if err != nil || rep.Unmarked != 0 {
			t.Fatalf("%s: unmarked %d, %v", format, rep.Unmarked, err)
		}
	}
	for _, f := range files(t, dir) {
		got, err := os.ReadFile(f + ":Zone.Identifier")
		if err != nil || string(got) != "[ZoneTransfer]\r\nZoneId=3\r\n" {
			t.Errorf("%s: Zone.Identifier = %q, %v", f, got, err)
		}
	}
}
