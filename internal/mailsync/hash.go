package mailsync

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"hash"
	"slices"
)

// ContentHasher berechnet content_hash (SHA-256 über die Rohmail ohne
// CR-Bytes) und dazu Varianten mit 0, 1 und 2 Zeilenenden am Schluss. Eine
// Nachricht aus einem mbox-Import hat am Ende genau ein Zeilenende (die
// Leerzeile vor der nächsten Trennzeile gehört zum Rahmen und ist nicht
// eindeutig); die Kopie des Servers kann dort anders aussehen. Sync und
// Import suchen deshalb mit allen Varianten (S1.10); gespeichert wird immer
// der exakte Wert.
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
