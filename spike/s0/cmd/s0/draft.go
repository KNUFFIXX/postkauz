package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message/mail"
)

// draftResult beschreibt den Draft-APPEND-Test.
type draftResult struct {
	DraftsMailbox   string          `json:"drafts_mailbox"`
	ReplyTo         string          `json:"reply_to_message_id"`
	NewMessageID    string          `json:"new_message_id"`
	Subject         string          `json:"subject"`
	RawSubjectLine  string          `json:"raw_subject_header"`
	AppendUID       uint32          `json:"append_uid"`
	AppendMs        float64         `json:"append_ms"`
	ReadbackFlags   []string        `json:"readback_flags"`
	Checks          map[string]bool `json:"checks"`
	OK              bool            `json:"ok"`
	ReadbackHeaders string          `json:"readback_headers"`
}

var replyPrefix = regexp.MustCompile(`^(?i)((re|aw|wg|fwd?)\s*:\s*)+`)

// buildReply erzeugt eine Antwort (Draft) auf eine Rohmail.
func buildReply(orig []byte) ([]byte, string, string, []string, error) {
	pm := parseGoMessage(orig)
	if pm.MessageID == "" {
		return nil, "", "", nil, fmt.Errorf("Originalmail ohne Message-ID")
	}
	mr, err := mail.CreateReader(bytes.NewReader(orig))
	if err != nil {
		return nil, "", "", nil, err
	}
	from, _ := mr.Header.AddressList("From")
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{{Name: "Spike Postfach", Address: "spike@mw.example.test"}})
	h.SetAddressList("To", from)
	subject := "AW: " + replyPrefix.ReplaceAllString(pm.Subject, "")
	h.SetSubject(subject)
	newID := fmt.Sprintf("mw.draft.%d@mw.example.test", time.Now().UnixNano())
	h.SetMessageID(newID)
	refs := append(append([]string{}, pm.References...), pm.MessageID)
	h.SetMsgIDList("In-Reply-To", []string{pm.MessageID})
	h.SetMsgIDList("References", refs)
	h.Set("X-Mailwerk-Draft", "agent-proposal")
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8", "format": "flowed"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	var buf bytes.Buffer
	w, err := mail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return nil, "", "", nil, err
	}
	fmt.Fprintf(w, "Guten Tag,\r\n\r\nvielen Dank für Ihre Nachricht. Wir kümmern uns – Grüße aus Neuß.\r\n\r\n")
	fmt.Fprintf(w, "Am %s schrieb %s:\r\n", pm.Date, pm.From)
	for _, l := range strings.Split(strings.ReplaceAll(pm.Text, "\r\n", "\n"), "\n") {
		fmt.Fprintf(w, "> %s\r\n", l)
	}
	if err := w.Close(); err != nil {
		return nil, "", "", nil, err
	}
	return buf.Bytes(), newID, subject, refs, nil
}

func runDraft(cfg *config) (*draftResult, error) {
	st, err := openStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	// Originalmail: INBOX-Mail mit der laengsten References-Kette
	var blob, origID string
	err = st.db.QueryRow(`SELECT m.blob_sha256, m.message_id FROM messages m JOIN message_mailboxes mm ON mm.message_rowid=m.id
		WHERE mm.mailbox='INBOX' AND m.message_id<>'' ORDER BY length(m.refs) DESC LIMIT 1`).Scan(&blob, &origID)
	if err != nil {
		return nil, fmt.Errorf("keine Originalmail im Spiegel (erst sync laufen lassen): %w", err)
	}
	orig, err := st.readBlob(blob)
	if err != nil {
		return nil, err
	}
	raw, newID, subject, refs, err := buildReply(orig)
	if err != nil {
		return nil, err
	}
	c, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	boxes, err := listMailboxes(c)
	if err != nil {
		return nil, err
	}
	res := &draftResult{DraftsMailbox: findDrafts(boxes), ReplyTo: origID, NewMessageID: newID, Subject: subject, Checks: map[string]bool{}}
	for _, l := range strings.Split(string(raw), "\r\n") {
		if strings.HasPrefix(l, "Subject:") {
			res.RawSubjectLine = l
		}
	}
	t0 := time.Now()
	res.AppendUID, err = appendRaw(c, res.DraftsMailbox, raw, []imap.Flag{imap.FlagDraft, imap.FlagSeen})
	res.AppendMs = ms(time.Since(t0))
	if err != nil {
		return res, fmt.Errorf("APPEND Drafts: %w", err)
	}
	// Zuruecklesen
	if _, err := c.Select(res.DraftsMailbox, nil).Wait(); err != nil {
		return res, err
	}
	var set imap.UIDSet
	if res.AppendUID > 0 {
		set = imap.UIDSetNum(imap.UID(res.AppendUID))
	} else {
		uids, err := uidsWith(c, &imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: newID}}})
		if err != nil || len(uids) == 0 {
			return res, fmt.Errorf("Draft nicht wiedergefunden: %v", err)
		}
		set = imap.UIDSetNum(imap.UID(uids[0]))
	}
	msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true, Envelope: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}}).Collect()
	if err != nil || len(msgs) != 1 {
		return res, fmt.Errorf("FETCH Draft: %v (%d)", err, len(msgs))
	}
	back := msgs[0].BodySection[0].Bytes
	for _, f := range msgs[0].Flags {
		res.ReadbackFlags = append(res.ReadbackFlags, string(f))
	}
	pm := parseGoMessage(back)
	en := parseEnmime(back)
	hdrEnd := bytes.Index(back, []byte("\r\n\r\n"))
	if hdrEnd > 0 {
		res.ReadbackHeaders = string(back[:hdrEnd])
	}
	res.Checks["bytes_identical"] = bytes.Equal(back, raw)
	res.Checks["message_id"] = pm.MessageID == newID
	res.Checks["in_reply_to"] = pm.InReplyTo == origID
	res.Checks["references_chain"] = strings.Join(pm.References, " ") == strings.Join(refs, " ")
	res.Checks["subject_go_message"] = pm.Subject == subject
	res.Checks["subject_enmime"] = en.Subject == subject
	res.Checks["subject_header_ascii_encoded"] = strings.Contains(res.RawSubjectLine, "=?utf-8?") || !strings.ContainsFunc(subject, func(r rune) bool { return r > 127 })
	res.Checks["envelope_subject"] = msgs[0].Envelope != nil && msgs[0].Envelope.Subject == subject
	res.Checks["flag_draft"] = strings.Contains(strings.Join(res.ReadbackFlags, " "), "\\Draft")
	res.Checks["quoted_text"] = strings.Contains(pm.Text, "\n> ")
	res.Checks["umlaut_body"] = strings.Contains(pm.Text, "kümmern")
	res.OK = true
	for k, v := range res.Checks {
		if !v && k != "bytes_identical" {
			res.OK = false
		}
	}
	return res, nil
}
