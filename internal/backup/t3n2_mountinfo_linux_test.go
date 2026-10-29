package backup

import "testing"

// unescapeMount löst die oktalen Masken aus /proc/self/mountinfo auf.
func TestT3N2UnescapeMount(t *testing.T) {
	for in, want := range map[string]string{
		`/plain`:                `/plain`,
		`/mit\040Leerzeichen`:   `/mit Leerzeichen`,
		`/a\011b\012c\134d`:     "/a\tb\nc\\d",
		`/kaputt\04`:            `/kaputt\04`,
		`/ende\`:                `/ende\`,
		`/kein\999oktal`:        `/kein\999oktal`,
		`/zwei\040\040Zeichen`:  `/zwei  Zeichen`,
		`\040am\040Anfang\040x`: ` am Anfang x`,
	} {
		if got := unescapeMount(in); got != want {
			t.Errorf("unescapeMount(%q) = %q, want %q", in, got, want)
		}
	}
}
