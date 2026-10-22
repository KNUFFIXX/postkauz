package drafts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// HashInput ist alles, was eine Freigabe bindet (A02 E7): Identität
// (ID und Absender), To, Cc, Bcc, Betreff, body_text, body_html, die
// Hashes der Anhänge, In-Reply-To und die Art. Markdown fehlt bewusst: Es
// wirkt nur über das daraus erzeugte HTML und den Text.
type HashInput struct {
	IdentityID       int64
	FromName         string
	FromEmail        string
	To, Cc, Bcc      []store.Addr
	Subject          string
	BodyText         string
	BodyHTML         string
	AttachmentHashes []string
	InReplyTo        string
	Kind             Kind
}

// canonical ist die feste Form für den Hash. Die Feldreihenfolge steht
// fest (encoding/json folgt der Struktur), Listen sind sortiert.
type canonical struct {
	V           int          `json:"v"`
	Identity    int64        `json:"identity"`
	FromName    string       `json:"from_name"`
	FromEmail   string       `json:"from_email"`
	To          []store.Addr `json:"to"`
	Cc          []store.Addr `json:"cc"`
	Bcc         []store.Addr `json:"bcc"`
	Subject     string       `json:"subject"`
	BodyText    string       `json:"body_text"`
	BodyHTML    string       `json:"body_html"`
	Attachments []string     `json:"attachments"`
	InReplyTo   string       `json:"in_reply_to"`
	Kind        string       `json:"kind"`
}

// hashDomain trennt diesen Hash von allen anderen SHA-256-Werten des
// Programms (eine Kanonik-Änderung bekommt eine neue Version).
const hashDomain = "postkauz-preview-v1\n"

// NormalizeAddr bringt eine Adresse in die kanonische Form: Adresse klein
// und ohne Leerraum am Rand, Name mit einfachem Leerraum.
func NormalizeAddr(a store.Addr) store.Addr {
	return store.Addr{Name: strings.Join(strings.Fields(a.Name), " "), Address: strings.ToLower(strings.TrimSpace(a.Address))}
}

// normalizeList normalisiert und sortiert (nach Adresse, dann Name); die
// Reihenfolge der Eingabe ändert den Hash nicht, jede andere Änderung schon.
func normalizeList(in []store.Addr) []store.Addr {
	out := make([]store.Addr, 0, len(in))
	for _, a := range in {
		out = append(out, NormalizeAddr(a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Address != out[j].Address {
			return out[i].Address < out[j].Address
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// PreviewHash ist SHA-256 (hex) über die kanonische Form (E7).
func PreviewHash(h HashInput) string {
	atts := append([]string{}, h.AttachmentHashes...)
	for i := range atts {
		atts[i] = strings.ToLower(strings.TrimSpace(atts[i]))
	}
	sort.Strings(atts)
	c := canonical{V: 1, Identity: h.IdentityID, FromName: strings.Join(strings.Fields(h.FromName), " "),
		FromEmail: strings.ToLower(strings.TrimSpace(h.FromEmail)), To: normalizeList(h.To), Cc: normalizeList(h.Cc),
		Bcc: normalizeList(h.Bcc), Subject: h.Subject, BodyText: h.BodyText, BodyHTML: h.BodyHTML, Attachments: atts,
		InReplyTo: strings.TrimSpace(strings.Trim(strings.TrimSpace(h.InReplyTo), "<>")), Kind: string(h.Kind)}
	b, err := json.Marshal(c)
	if err != nil {
		// Strings und Zahlen lassen sich immer serialisieren.
		panic("drafts: canonical form: " + err.Error())
	}
	sum := sha256.Sum256(append([]byte(hashDomain), b...))
	return hex.EncodeToString(sum[:])
}

// HeaderInReplyTo ist der Schlüssel in headers_json, aus dem der Hash
// In-Reply-To liest (Namen der Kopfzeilen wie in der Nachricht; die Werte
// setzt der Nachrichtenbau, S2.2).
const HeaderInReplyTo = "In-Reply-To"

// HashOf liefert die Eingabe des Hashes für einen gespeicherten Entwurf.
func HashOf(d store.Draft) HashInput {
	h := HashInput{IdentityID: d.IdentityID, FromName: d.FromName, FromEmail: d.FromEmail, To: d.To, Cc: d.Cc, Bcc: d.Bcc,
		Subject: d.Subject, BodyText: d.BodyText, BodyHTML: d.BodyHTML, Kind: Kind(d.Kind)}
	for _, a := range d.Attachments {
		h.AttachmentHashes = append(h.AttachmentHashes, a.SHA256)
	}
	var headers map[string]any
	if json.Unmarshal([]byte(d.HeadersJSON), &headers) == nil {
		if v, ok := headers[HeaderInReplyTo].(string); ok {
			h.InReplyTo = v
		}
	}
	return h
}

// Fingerprint kennzeichnet den gespeicherten Inhalt eines Entwurfs: alles,
// woraus die Nachricht gebaut wird (preview_hash, Markdown, Kopfzeilen,
// Name und Typ der Anhänge, Schalter quote/signature). Ein Aufruf, der den
// Entwurf außerhalb der Schreibtransaktion liest, prüft (Hook) oder
// zusammenführt (Änderung einzelner Felder), gibt ihn mit; weicht er in der
// Transaktion ab, hat ein anderer Schreiber dazwischen geändert
// (draft_changed; Gegenprüfung S2.3, W2/W4). Zustand, Hook-Ergebnis und
// Zeitstempel gehören nicht dazu.
func Fingerprint(d store.Draft) string {
	b, _ := json.Marshal([]string{PreviewHash(HashOf(d)), extrasKey(d), d.ComposeOptions})
	sum := sha256.Sum256(append([]byte("postkauz-draft-fingerprint-v1\n"), b...))
	return hex.EncodeToString(sum[:])
}
