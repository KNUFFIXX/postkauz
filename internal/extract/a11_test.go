package extract

import "testing"

// A11 Teil 3 (b): Muster vorübergehender Gründe.
func TestA11IsTransientReason(t *testing.T) {
	for s, want := range map[string]bool{
		"extractor failed: exit status 0xc0000142":                          true,
		"extractor failed: exit status 0xC0000005":                          true,
		"extractor failed: fork/exec /usr/bin/pdftotext: permission denied": true,
		`extractor failed: exec: "pdftotext": executable file not found`:    true,
		"extractor failed: signal: killed":                                  true,
		"extractor failed: exit status 1":                                   false,
		"extractor failed: Syntax Error: Couldn't find trailer dictionary":  false,
		"extract: timed out after 30s (process killed)":                     false,
		"internal error in extractor":                                       false,
	} {
		if got := IsTransientReason(s); got != want {
			t.Errorf("%q: %v, want %v", s, got, want)
		}
	}
}
