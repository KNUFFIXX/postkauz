package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

// mutation sind die von aussen vorgenommenen Aenderungen (Soll fuer Lauf 2).
type mutation struct {
	Mailbox           string   `json:"mailbox"`
	FlaggedUIDs       []uint32 `json:"flagged_uids"`
	UnseenUIDs        []uint32 `json:"unseen_uids"`
	ExpungedUID       uint32   `json:"expunged_uid"`
	AppendedUID       uint32   `json:"appended_uid"`
	AppendedMessageID string   `json:"appended_message_id"`
	ExpungeMethod     string   `json:"expunge_method"`
}

// testMail baut eine kleine UTF-8-Mail fuer APPEND-Tests.
func testMail(subject, tag string) ([]byte, string, error) {
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{{Name: "Grete Größe", Address: "grete@groesse.example.test"}})
	h.SetAddressList("To", []*mail.Address{{Name: "Spike Postfach", Address: "spike@mw.example.test"}})
	h.SetSubject(subject)
	id := fmt.Sprintf("s0.%s.%d@mw.example.test", tag, time.Now().UnixNano())
	h.SetMessageID(id)
	h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	h.Set("Content-Transfer-Encoding", "quoted-printable")
	var buf bytes.Buffer
	w, err := mail.CreateSingleInlineWriter(&buf, h)
	if err != nil {
		return nil, "", err
	}
	fmt.Fprintf(w, "Testnachricht %s – Grüße aus der Spülküche.\r\n", tag)
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), id, nil
}

// appendRaw haengt eine Rohmail an und liefert die UID (UIDPLUS) zurueck.
func appendRaw(c *imapclient.Client, mailbox string, raw []byte, flags []imap.Flag) (uint32, error) {
	cmd := c.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: flags, Time: time.Now()})
	if _, err := cmd.Write(raw); err != nil {
		return 0, err
	}
	if err := cmd.Close(); err != nil {
		return 0, err
	}
	data, err := cmd.Wait()
	if err != nil {
		return 0, err
	}
	return uint32(data.UID), nil
}

func uidsWith(c *imapclient.Client, crit *imap.SearchCriteria) ([]uint32, error) {
	d, err := c.UIDSearch(crit, nil).Wait()
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, u := range d.AllUIDs() {
		out = append(out, uint32(u))
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func runMutate(cfg *config) (*mutation, error) {
	c, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	m := &mutation{Mailbox: "INBOX"}
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		return nil, err
	}
	// \Flagged auf 3 bisher nicht markierte Mails setzen
	notFlagged, err := uidsWith(c, &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagFlagged, imap.FlagSeen}})
	if err != nil {
		return nil, err
	}
	seen, err := uidsWith(c, &imap.SearchCriteria{Flag: []imap.Flag{imap.FlagSeen}, NotFlag: []imap.Flag{imap.FlagFlagged}})
	if err != nil {
		return nil, err
	}
	if len(notFlagged) < 4 || len(seen) < 2 {
		return nil, fmt.Errorf("zu wenige Mails fuer Mutationstest (%d/%d)", len(notFlagged), len(seen))
	}
	m.FlaggedUIDs = notFlagged[:3]
	m.UnseenUIDs = seen[:2]
	m.ExpungedUID = notFlagged[len(notFlagged)-1]
	if err := c.Store(imap.UIDSetNum(toUIDs(m.FlaggedUIDs)...), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagFlagged}}, nil).Close(); err != nil {
		return nil, fmt.Errorf("STORE +Flagged: %w", err)
	}
	if err := c.Store(imap.UIDSetNum(toUIDs(m.UnseenUIDs)...), &imap.StoreFlags{Op: imap.StoreFlagsDel, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		return nil, fmt.Errorf("STORE -Seen: %w", err)
	}
	// Eine Mail loeschen
	del := imap.UIDSetNum(imap.UID(m.ExpungedUID))
	if err := c.Store(del, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		return nil, err
	}
	if c.Caps().Has(imap.CapUIDPlus) {
		m.ExpungeMethod = "UID EXPUNGE"
		err = c.UIDExpunge(del).Close()
	} else {
		m.ExpungeMethod = "EXPUNGE"
		err = c.Expunge().Close()
	}
	if err != nil {
		return nil, fmt.Errorf("EXPUNGE: %w", err)
	}
	// Eine neue Mail anhaengen
	raw, id, err := testMail("Neue Nachricht von außen: Mängelanzeige", "mutate")
	if err != nil {
		return nil, err
	}
	m.AppendedMessageID = id
	m.AppendedUID, err = appendRaw(c, "INBOX", raw, nil)
	if err != nil {
		return nil, fmt.Errorf("APPEND: %w", err)
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	_ = os.MkdirAll(cfg.DataDir, 0o755)
	return m, os.WriteFile(filepath.Join(cfg.DataDir, "mutation.json"), b, 0o644)
}

func toUIDs(l []uint32) []imap.UID {
	out := make([]imap.UID, len(l))
	for i, u := range l {
		out[i] = imap.UID(u)
	}
	return out
}

// verifyMutation vergleicht die Soll-Aenderungen mit dem Inkrementallauf.
func verifyMutation(m *mutation, res *syncResult) map[string]any {
	var mb *mailboxSync
	for i := range res.Mailboxes {
		if res.Mailboxes[i].Name == m.Mailbox {
			mb = &res.Mailboxes[i]
		}
	}
	if mb == nil {
		return map[string]any{"ok": false, "reason": "INBOX fehlt im Lauf"}
	}
	want := map[uint32]bool{}
	for _, u := range append(append([]uint32{}, m.FlaggedUIDs...), m.UnseenUIDs...) {
		want[u] = true
	}
	got := map[uint32]bool{}
	for _, fc := range mb.FlagChanges {
		got[fc.UID] = true
	}
	flagsOK := len(want) == len(got)
	for u := range want {
		flagsOK = flagsOK && got[u]
	}
	delOK := len(mb.Vanished) == 1 && mb.Vanished[0] == m.ExpungedUID
	newOK := len(mb.NewUIDs) == 1 && mb.NewUIDs[0] == m.AppendedUID
	return map[string]any{
		"flags_ok": flagsOK, "expunge_ok": delOK, "new_ok": newOK,
		"ok":             flagsOK && delOK && newOK,
		"expected_flags": len(want), "seen_flag_changes": len(got),
		"other_mailboxes_quiet": otherQuiet(res, m.Mailbox),
	}
}

func otherQuiet(res *syncResult, except string) bool {
	for _, mb := range res.Mailboxes {
		if mb.Name != except && (len(mb.NewUIDs)+len(mb.FlagChanges)+len(mb.Vanished)) > 0 {
			return false
		}
	}
	return true
}
