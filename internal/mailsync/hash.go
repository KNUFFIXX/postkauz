package mailsync

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"hash"
	"slices"
)

// ContentHasher berechnet content_hash (SHA-256 über die Rohmail ohne
// CR-Bytes) und dazu Varianten mit 0, 1 und 2 Zeilenenden am Schluss. Der
// mbox-Import speichert seit Entscheid C2 (25.10.2026) byte-genau (nur die
// Leerzeile des Rahmens fällt weg), bis 0.2.0-rc2 aber mit genau einem
// Zeilenende am Schluss; und ob die letzte Leerzeile vor der Trennzeile
// Rahmen oder Inhalt ist, ist nicht eindeutig. Die Kopie des Servers kann am
// Ende also anders aussehen. Sync und Import suchen deshalb mit allen
// Varianten (S1.10; Rückfall, wenn Message-ID und Größe nicht passen);
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
// 0, 1 und 2 Zeilenenden am Schluss (ohne Dubletten, exact zuerst).
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
		for range n {
			h.Write(newlines[:1])
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	exact = sum(v.pending)
	variants = []string{exact}
	for _, n := range []int{1, 0, 2} {
		if n != v.pending {
			variants = append(variants, sum(n))
		}
	}
	return exact, variants
}
