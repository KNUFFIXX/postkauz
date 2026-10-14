package main

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
)

// wantedCaps ist die Faehigkeitsmatrix, die der Spike abfragt.
var wantedCaps = []imap.Cap{
	imap.CapCondStore, imap.CapQResync, imap.CapIdle, imap.CapMove, imap.CapUIDPlus,
	imap.CapSpecialUse, imap.CapListStatus, imap.CapEnable, imap.CapLiteralPlus,
	imap.CapNamespace, imap.CapIMAP4rev2, imap.CapESearch, imap.CapBinary,
}

// wordDecoder dekodiert RFC-2047-Woerter auch in iso-8859-1/windows-1252.
// Ohne CharsetReader liefert go-imap bei Nicht-UTF-8 still den Rohtext zurueck.
var wordDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

// dial baut eine Verbindung auf und meldet sich an.
func dial(cfg *config, h *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	opts := &imapclient.Options{
		UnilateralDataHandler: h,
		WordDecoder:           wordDecoder,
		Dialer:                &net.Dialer{Timeout: 10 * time.Second},
	}
	if cfg.PlainWD {
		opts.WordDecoder = nil
	}
	if cfg.Insecure {
		// Nur fuer lokale Docker-Testserver mit selbstsigniertem Zertifikat.
		opts.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	var c *imapclient.Client
	var err error
	if cfg.TLS {
		c, err = imapclient.DialTLS(cfg.Addr, opts)
	} else {
		c, err = imapclient.DialInsecure(cfg.Addr, opts)
	}
	if err != nil {
		return nil, fmt.Errorf("verbinden: %w", err)
	}
	if err := c.Login(cfg.User, cfg.Pass).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("login: %w", err)
	}
	return c, nil
}

// mailboxInfo beschreibt einen Ordner aus LIST + STATUS.
type mailboxInfo struct {
	Name          string   `json:"name"`
	Delim         string   `json:"delim"`
	Attrs         []string `json:"attrs"`
	Role          string   `json:"role,omitempty"`
	Messages      *uint32  `json:"messages,omitempty"`
	Unseen        *uint32  `json:"unseen,omitempty"`
	UIDValidity   uint32   `json:"uidvalidity"`
	UIDNext       uint32   `json:"uidnext"`
	HighestModSeq uint64   `json:"highestmodseq,omitempty"`
	StatusVia     string   `json:"status_via"`
}

// capsResult ist der Report von caps.
type capsResult struct {
	Capabilities []string        `json:"capabilities"`
	Matrix       map[string]bool `json:"matrix"`
	Mailboxes    []mailboxInfo   `json:"mailboxes"`
	ConnectMs    float64         `json:"connect_login_ms"`
	ListMs       float64         `json:"list_status_ms"`
}

func capList(c *imapclient.Client) []string {
	var out []string
	for k := range c.Caps() {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}

func capMatrix(c *imapclient.Client) map[string]bool {
	caps := c.Caps()
	m := map[string]bool{}
	for _, k := range wantedCaps {
		m[string(k)] = caps.Has(k)
	}
	return m
}

func roleOf(attrs []imap.MailboxAttr) string {
	for _, a := range attrs {
		switch a {
		case imap.MailboxAttrDrafts, imap.MailboxAttrSent, imap.MailboxAttrTrash,
			imap.MailboxAttrJunk, imap.MailboxAttrArchive, imap.MailboxAttrAll, imap.MailboxAttrFlagged:
			return strings.TrimPrefix(string(a), "\\")
		}
	}
	return ""
}

// listMailboxes holt alle Ordner mit Rollen und STATUS. Mit LIST-STATUS in
// einem Kommando, sonst STATUS je Ordner.
func listMailboxes(c *imapclient.Client) ([]mailboxInfo, error) {
	caps := c.Caps()
	condstore := caps.Has(imap.CapCondStore)
	so := &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDValidity: true, UIDNext: true, HighestModSeq: condstore}
	lo := &imap.ListOptions{}
	if caps.Has(imap.CapSpecialUse) {
		lo.ReturnSpecialUse = true
	}
	listStatus := caps.Has(imap.CapListStatus) || caps.Has(imap.CapIMAP4rev2)
	if listStatus {
		lo.ReturnStatus = so
	}
	list, err := c.List("", "*", lo).Collect()
	if err != nil {
		return nil, fmt.Errorf("LIST: %w", err)
	}
	var out []mailboxInfo
	for _, l := range list {
		mi := mailboxInfo{Name: l.Mailbox, Delim: string(l.Delim), Role: roleOf(l.Attrs)}
		for _, a := range l.Attrs {
			mi.Attrs = append(mi.Attrs, string(a))
		}
		noSelect := false
		for _, a := range l.Attrs {
			if a == imap.MailboxAttrNoSelect || a == imap.MailboxAttrNonExistent {
				noSelect = true
			}
		}
		st := l.Status
		mi.StatusVia = "LIST-STATUS"
		if st == nil && !noSelect {
			st, err = c.Status(l.Mailbox, so).Wait()
			if err != nil {
				return nil, fmt.Errorf("STATUS %s: %w", l.Mailbox, err)
			}
			mi.StatusVia = "STATUS"
		}
		if st != nil {
			mi.Messages, mi.Unseen = st.NumMessages, st.NumUnseen
			mi.UIDValidity, mi.UIDNext, mi.HighestModSeq = st.UIDValidity, uint32(st.UIDNext), st.HighestModSeq
		}
		out = append(out, mi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func runCaps(cfg *config) (*capsResult, error) {
	t0 := time.Now()
	c, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	res := &capsResult{ConnectMs: ms(time.Since(t0))}
	res.Capabilities = capList(c)
	res.Matrix = capMatrix(c)
	t1 := time.Now()
	res.Mailboxes, err = listMailboxes(c)
	res.ListMs = ms(time.Since(t1))
	return res, err
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// findDrafts sucht den Entwuerfe-Ordner (SPECIAL-USE \Drafts, sonst "Drafts").
func findDrafts(boxes []mailboxInfo) string {
	for _, b := range boxes {
		if b.Role == "Drafts" {
			return b.Name
		}
	}
	for _, b := range boxes {
		if strings.EqualFold(b.Name, "Drafts") || strings.EqualFold(b.Name, "INBOX.Drafts") {
			return b.Name
		}
	}
	return "Drafts"
}

// syncTargets liefert INBOX + Archiv-Ordner (alles mit "Archiv" im Namen).
func syncTargets(boxes []mailboxInfo) []mailboxInfo {
	var out []mailboxInfo
	for _, b := range boxes {
		if b.Name == "INBOX" || (strings.HasPrefix(b.Name, "Archiv") && b.Messages != nil && *b.Messages > 0) {
			out = append(out, b)
		}
	}
	return out
}
