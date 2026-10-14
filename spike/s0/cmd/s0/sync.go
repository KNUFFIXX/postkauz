package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// flagChange beschreibt eine erkannte Flag-Aenderung.
type flagChange struct {
	UID uint32 `json:"uid"`
	Old string `json:"old"`
	New string `json:"new"`
}

// mailboxSync ist das Ergebnis je Ordner.
type mailboxSync struct {
	Name               string       `json:"name"`
	Mode               string       `json:"mode"`
	UIDValidity        uint32       `json:"uidvalidity"`
	UIDNext            uint32       `json:"uidnext"`
	HighestModSeq      uint64       `json:"highestmodseq"`
	Exists             uint32       `json:"exists"`
	PrevUIDNext        uint32       `json:"prev_uidnext,omitempty"`
	PrevHighestModSeq  uint64       `json:"prev_highestmodseq,omitempty"`
	UIDValidityChanged bool         `json:"uidvalidity_changed,omitempty"`
	Fetched            int          `json:"fetched"`
	NewUIDs            []uint32     `json:"new_uids,omitempty"`
	FlagMethod         string       `json:"flag_method,omitempty"`
	FlagChanges        []flagChange `json:"flag_changes,omitempty"`
	DeleteMethod       string       `json:"delete_method,omitempty"`
	Vanished           []uint32     `json:"vanished,omitempty"`
	MetaSeconds        float64      `json:"meta_s"`
	BodySeconds        float64      `json:"body_s"`
	StoreSeconds       float64      `json:"store_s"`
	Bytes              int64        `json:"bytes"`
}

// syncResult ist der Gesamtreport eines Synclaufs.
type syncResult struct {
	Mode                 string         `json:"mode"`
	Mailboxes            []mailboxSync  `json:"mailboxes"`
	TotalFetched         int            `json:"total_fetched"`
	TotalBytes           int64          `json:"total_bytes"`
	Seconds              float64        `json:"seconds"`
	MailsPerSecond       float64        `json:"mails_per_second"`
	PeakHeapMB           float64        `json:"peak_heap_mb"`
	SysMB                float64        `json:"sys_mb"`
	EnvelopeChecked      int            `json:"envelope_subjects_checked"`
	EnvelopeMismatch     int            `json:"envelope_subject_mismatches"`
	EnvelopeMismatchEx   []string       `json:"envelope_mismatch_examples,omitempty"`
	BodystructureMissing int            `json:"bodystructure_missing"`
	DBMessages           int            `json:"db_messages"`
	DBMailboxRows        int            `json:"db_mailbox_rows"`
	Verification         map[string]any `json:"verification,omitempty"`
}

type syncer struct {
	cfg       *config
	c         *imapclient.Client
	st        *store
	condstore bool
	manifest  map[string]manifestEntry
	res       *syncResult
	peakHeap  uint64
}

func runSync(cfg *config) (*syncResult, error) {
	if cfg.Fresh {
		os.RemoveAll(cfg.DataDir)
	}
	st, err := openStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	c, err := dial(cfg, nil)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	res, err := syncAll(cfg, c, st)
	if err != nil {
		return res, err
	}
	// Erwartete Aenderungen aus "mutate" pruefen, falls vorhanden.
	mp := filepath.Join(cfg.DataDir, "mutation.json")
	if b, err := os.ReadFile(mp); err == nil && res.Mode == "incremental" {
		var m mutation
		if json.Unmarshal(b, &m) == nil {
			res.Verification = verifyMutation(&m, res)
			_ = os.Rename(mp, mp+".verified")
		}
	}
	return res, nil
}

// syncAll synchronisiert INBOX + Archiv-Ordner.
func syncAll(cfg *config, c *imapclient.Client, st *store) (*syncResult, error) {
	t0 := time.Now()
	boxes, err := listMailboxes(c)
	if err != nil {
		return nil, err
	}
	s := &syncer{cfg: cfg, c: c, st: st, condstore: c.Caps().Has(imap.CapCondStore), res: &syncResult{}}
	s.manifest, _ = loadManifest(cfg.Corpus)
	modes := map[string]bool{}
	for _, b := range syncTargets(boxes) {
		ms, err := s.syncMailbox(b)
		if err != nil {
			return s.res, fmt.Errorf("%s: %w", b.Name, err)
		}
		modes[ms.Mode] = true
		s.res.Mailboxes = append(s.res.Mailboxes, *ms)
		s.res.TotalFetched += ms.Fetched
		s.res.TotalBytes += ms.Bytes
	}
	if len(modes) == 1 {
		for k := range modes {
			s.res.Mode = k
		}
	} else {
		s.res.Mode = "mixed"
	}
	s.res.Seconds = time.Since(t0).Seconds()
	if s.res.Seconds > 0 {
		s.res.MailsPerSecond = float64(s.res.TotalFetched) / s.res.Seconds
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s.sampleHeap()
	s.res.PeakHeapMB = float64(s.peakHeap) / 1e6
	s.res.SysMB = float64(m.Sys) / 1e6
	_ = st.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&s.res.DBMessages)
	_ = st.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&s.res.DBMailboxRows)
	return s.res, nil
}

func (s *syncer) sampleHeap() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.HeapAlloc > s.peakHeap {
		s.peakHeap = m.HeapAlloc
	}
}

func normFlags(fl []imap.Flag) string {
	var out []string
	for _, f := range fl {
		if f == "\\Recent" {
			continue
		}
		out = append(out, string(f))
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func (s *syncer) syncMailbox(b mailboxInfo) (*mailboxSync, error) {
	sel, err := s.c.Select(b.Name, &imap.SelectOptions{CondStore: s.condstore}).Wait()
	if err != nil {
		return nil, fmt.Errorf("SELECT: %w", err)
	}
	ms := &mailboxSync{Name: b.Name, UIDValidity: sel.UIDValidity, UIDNext: uint32(sel.UIDNext),
		HighestModSeq: sel.HighestModSeq, Exists: sel.NumMessages}
	prev, err := s.st.getMailbox(b.Name)
	if err != nil {
		return nil, err
	}
	last := uint32(sel.UIDNext) - 1
	if prev == nil || prev.UIDValidity != sel.UIDValidity {
		ms.Mode = "initial"
		if prev != nil {
			// UIDVALIDITY gewechselt: lokale Zuordnung verwerfen, Blobs bleiben.
			ms.UIDValidityChanged = true
			if _, err := s.st.db.Exec(`DELETE FROM message_mailboxes WHERE mailbox=?`, b.Name); err != nil {
				return nil, err
			}
		}
		if last >= 1 {
			if err := s.fetchRange(ms, 1, last); err != nil {
				return nil, err
			}
		}
	} else {
		ms.Mode = "incremental"
		ms.PrevUIDNext, ms.PrevHighestModSeq = prev.UIDNext, prev.HighestModSeq
		// 1) neue UIDs ab gespeichertem UIDNEXT
		if uint32(sel.UIDNext) > prev.UIDNext {
			if err := s.fetchRange(ms, prev.UIDNext, last); err != nil {
				return nil, err
			}
		}
		if prev.UIDNext > 1 {
			// 2) Flag-Aenderungen der bekannten UIDs
			if err := s.syncFlags(ms, prev); err != nil {
				return nil, err
			}
			// 3) Loeschungen
			if err := s.syncDeletes(ms, prev); err != nil {
				return nil, err
			}
		}
	}
	err = s.st.putMailbox(b.Name, b.Role, mailboxState{UIDValidity: sel.UIDValidity, UIDNext: uint32(sel.UIDNext), HighestModSeq: sel.HighestModSeq})
	return ms, err
}

// fetchRange holt UIDs [from, to] in Fenstern: erst Metadaten, dann BODY.PEEK[].
func (s *syncer) fetchRange(ms *mailboxSync, from, to uint32) error {
	w := uint32(s.cfg.Window)
	for lo := from; lo <= to; lo += w {
		hi := min(lo+w-1, to)
		var set imap.UIDSet
		set.AddRange(imap.UID(lo), imap.UID(hi))

		t0 := time.Now()
		metas, err := s.c.Fetch(set, &imap.FetchOptions{
			UID: true, Flags: true, InternalDate: true, RFC822Size: true, Envelope: true,
			BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, ModSeq: s.condstore,
		}).Collect()
		if err != nil {
			return fmt.Errorf("FETCH meta %d:%d: %w", lo, hi, err)
		}
		ms.MetaSeconds += time.Since(t0).Seconds()
		byUID := map[imap.UID]*imapclient.FetchMessageBuffer{}
		for _, m := range metas {
			byUID[m.UID] = m
			if m.BodyStructure == nil {
				s.res.BodystructureMissing++
			}
			s.checkEnvelope(m)
		}

		t1 := time.Now()
		cmd := s.c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{{Peek: true}}})
		type rawMsg struct {
			uid imap.UID
			raw []byte
		}
		var raws []rawMsg
		for msg := cmd.Next(); msg != nil; msg = cmd.Next() {
			var r rawMsg
			for item := msg.Next(); item != nil; item = msg.Next() {
				switch it := item.(type) {
				case imapclient.FetchItemDataUID:
					r.uid = it.UID
				case imapclient.FetchItemDataBodySection:
					r.raw, err = io.ReadAll(it.Literal)
					if err != nil {
						return err
					}
				}
			}
			raws = append(raws, r)
		}
		if err := cmd.Close(); err != nil {
			return fmt.Errorf("FETCH body %d:%d: %w", lo, hi, err)
		}
		ms.BodySeconds += time.Since(t1).Seconds()
		s.sampleHeap()

		t2 := time.Now()
		tx, err := s.st.db.Begin()
		if err != nil {
			return err
		}
		for _, r := range raws {
			meta := byUID[r.uid]
			if meta == nil {
				tx.Rollback()
				return fmt.Errorf("UID %d ohne Metadaten", r.uid)
			}
			if err := s.storeMessage(tx, ms.Name, meta, r.raw); err != nil {
				tx.Rollback()
				return err
			}
			ms.Fetched++
			ms.Bytes += int64(len(r.raw))
			if ms.Mode == "incremental" {
				ms.NewUIDs = append(ms.NewUIDs, uint32(r.uid))
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		ms.StoreSeconds += time.Since(t2).Seconds()
		logf(s.cfg, "%s: UIDs %d-%d: %d Mails", ms.Name, lo, hi, len(raws))
	}
	return nil
}

func (s *syncer) checkEnvelope(m *imapclient.FetchMessageBuffer) {
	if s.manifest == nil || m.Envelope == nil {
		return
	}
	e, ok := s.manifest[m.Envelope.MessageID]
	if !ok || e.Broken != "" {
		return
	}
	s.res.EnvelopeChecked++
	if m.Envelope.Subject != e.Subject {
		s.res.EnvelopeMismatch++
		if len(s.res.EnvelopeMismatchEx) < 5 {
			s.res.EnvelopeMismatchEx = append(s.res.EnvelopeMismatchEx, fmt.Sprintf("%q != %q", m.Envelope.Subject, e.Subject))
		}
	}
}

func (s *syncer) storeMessage(tx *sql.Tx, mailbox string, meta *imapclient.FetchMessageBuffer, raw []byte) error {
	h, err := s.st.putBlob(raw)
	if err != nil {
		return err
	}
	pm := parseForIndex(raw)
	var rowid int64
	err = tx.QueryRow(`SELECT id FROM messages WHERE blob_sha256=?`, h).Scan(&rowid)
	if err == sql.ErrNoRows {
		r, err := tx.Exec(`INSERT INTO messages(blob_sha256, message_id, in_reply_to, refs, subject, from_addr, to_addr, date, size)
			VALUES(?,?,?,?,?,?,?,?,?)`, h, pm.MessageID, pm.InReplyTo, strings.Join(pm.References, " "),
			pm.Subject, pm.From, pm.To, pm.Date, len(raw))
		if err != nil {
			return err
		}
		rowid, _ = r.LastInsertId()
		if _, err := tx.Exec(`INSERT INTO messages_fts(rowid, subject, from_addr, to_addr, body) VALUES(?,?,?,?,?)`,
			rowid, pm.Subject, pm.From, pm.To, pm.Text); err != nil {
			return fmt.Errorf("FTS: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO messages_fts_tri(rowid, subject, body) VALUES(?,?,?)`,
			rowid, pm.Subject, pm.Text); err != nil {
			return fmt.Errorf("FTS trigram: %w", err)
		}
	} else if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT OR REPLACE INTO message_mailboxes(mailbox, uid, message_rowid, flags, modseq, internaldate)
		VALUES(?,?,?,?,?,?)`, mailbox, uint32(meta.UID), rowid, normFlags(meta.Flags), meta.ModSeq,
		meta.InternalDate.Format(time.RFC3339))
	return err
}

// syncFlags erkennt Flag-Aenderungen: CHANGEDSINCE bei CONDSTORE, sonst Vollvergleich.
func (s *syncer) syncFlags(ms *mailboxSync, prev *mailboxState) error {
	local, err := s.st.localFlags(ms.Name)
	if err != nil {
		return err
	}
	var set imap.UIDSet
	set.AddRange(1, imap.UID(prev.UIDNext-1))
	opts := &imap.FetchOptions{UID: true, Flags: true}
	if s.condstore && prev.HighestModSeq > 0 {
		if ms.HighestModSeq == prev.HighestModSeq {
			ms.FlagMethod = "HIGHESTMODSEQ unveraendert (kein FETCH)"
			return nil
		}
		ms.FlagMethod = fmt.Sprintf("FETCH (FLAGS) (CHANGEDSINCE %d)", prev.HighestModSeq)
		opts.ModSeq = true
		opts.ChangedSince = prev.HighestModSeq
	} else {
		ms.FlagMethod = "FETCH 1:* (FLAGS) Vollvergleich"
	}
	msgs, err := s.c.Fetch(set, opts).Collect()
	if err != nil {
		return fmt.Errorf("FETCH flags: %w", err)
	}
	for _, m := range msgs {
		old, ok := local[uint32(m.UID)]
		if !ok {
			continue // neu in diesem Lauf oder unbekannt
		}
		nf := normFlags(m.Flags)
		if nf != old {
			ms.FlagChanges = append(ms.FlagChanges, flagChange{UID: uint32(m.UID), Old: old, New: nf})
			if _, err := s.st.db.Exec(`UPDATE message_mailboxes SET flags=?, modseq=? WHERE mailbox=? AND uid=?`,
				nf, m.ModSeq, ms.Name, uint32(m.UID)); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncDeletes vergleicht die bekannten UIDs mit UID SEARCH (QRESYNC/VANISHED
// kann go-imap v2 beta.8 nicht).
func (s *syncer) syncDeletes(ms *mailboxSync, prev *mailboxState) error {
	var set imap.UIDSet
	set.AddRange(1, imap.UID(prev.UIDNext-1))
	ms.DeleteMethod = "UID SEARCH UID 1:<prev_uidnext-1> Abgleich"
	data, err := s.c.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{set}}, nil).Wait()
	if err != nil {
		return fmt.Errorf("UID SEARCH: %w", err)
	}
	server := map[uint32]bool{}
	for _, u := range data.AllUIDs() {
		server[uint32(u)] = true
	}
	local, err := s.st.localFlags(ms.Name)
	if err != nil {
		return err
	}
	for uid := range local {
		if uid < prev.UIDNext && !server[uid] {
			ms.Vanished = append(ms.Vanished, uid)
		}
	}
	sort.Slice(ms.Vanished, func(i, j int) bool { return ms.Vanished[i] < ms.Vanished[j] })
	for _, uid := range ms.Vanished {
		if _, err := s.st.db.Exec(`DELETE FROM message_mailboxes WHERE mailbox=? AND uid=?`, ms.Name, uid); err != nil {
			return err
		}
	}
	return nil
}
