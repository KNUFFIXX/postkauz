package mailsync

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"hash"
	"slices"
)

// ContentHasher berechnet content_hash (SHA-256 über die Rohmail ohne
// CR-Bytes) und dazu Varianten am Zeilenende: eines weniger und eines mehr
// als die Nachricht selbst hat, dazu 0, 1 und 2 Zeilenenden am Schluss.
// Der mbox-Import speichert seit Entscheid C2 (25.10.2026) byte-genau und
// entfernt nur die Leerzeile des Rahmens; ob die letzte Leerzeile vor der
// Trennzeile Rahmen oder Inhalt ist, ist aber nicht eindeutig (mboxrd-Quellen
// und die letzte Nachricht einer Datei haben sie nicht immer). Deshalb
// weicht eine Kopie um höchstens ein Zeilenende ab, gleich wie viele
// Leerzeilen die Nachricht am Ende hat (±1). Bis 0.2.0-rc2 speicherte der
// Import immer genau ein Zeilenende am Schluss; diese Bestände findet die
// feste Variante 1 (0 und 2 bleiben aus S1.10). Sync und Import suchen mit
// allen Varianten (Rückfall, wenn Message-ID und Größe nicht passen);
// gespeichert wird immer der exakte Wert.
type ContentHasher struct {
	h       hash.Hash
	pending int // zurückgehaltene "\n" am Ende
	buf     []byte
}

// NewContentHasher legt einen leeren Hasher an.
func NewContentHasher() *ContentHasher { return &ContentHasher{h: sha256.New()} }

var newlines = []byte("\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n")

func (v *ContentHasher) writeNL(n int) {
	for n > 0 {
		k := min(n, len(newlines))
		v.h.Write(newlines[:k])
		n -= k
	}
}

func (v *ContentHasher) Write(p []byte) (int, error) {
	q := p
	if slices.Contains(p, '\r') {
		v.buf = v.buf[:0]
		for _, b := range p {
			if b != '\r' {
				v.buf = append(v.buf, b)
			}
		}
		q = v.buf
	}
	if len(q) == 0 {
		return len(p), nil
	}
	t := 0
	for t < len(q) && q[len(q)-1-t] == '\n' {
		t++
	}
	if t == len(q) {
		v.pending += t
		return len(p), nil
	}
	v.writeNL(v.pending)
	v.h.Write(q[:len(q)-t])
	v.pending = t
	return len(p), nil
}

// Sums liefert den Hash des Geschriebenen (exact) und die Varianten mit
// einem Zeilenende weniger und einem mehr am Schluss sowie mit 0, 1 und 2
// (ohne Dubletten, exact zuerst; höchstens sechs Werte).
func (v *ContentHasher) Sums() (exact string, variants []string) {
	state, err := v.h.(encoding.BinaryMarshaler).MarshalBinary()
	if err != nil {
		panic(err) // sha256 kann das immer
	}
	sum := func(n int) string {
		h := sha256.New()
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
			panic(err)
		}
		for n > 0 {
			k := min(n, len(newlines))
			h.Write(newlines[:k])
			n -= k
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	exact = sum(v.pending)
	variants = []string{exact}
	seen := []int{v.pending}
	for _, n := range []int{v.pending - 1, v.pending + 1, 1, 0, 2} {
		if n >= 0 && !slices.Contains(seen, n) {
			seen = append(seen, n)
			variants = append(variants, sum(n))
		}
	}
	return exact, variants
}
