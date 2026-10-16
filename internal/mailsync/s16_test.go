package mailsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/extract"
)

// Akzeptanz S1.6 über den Sync: Der Korpus wird gegen den Testserver im
// Prozess gespiegelt; Mails mit verstecktem Text tragen hidden_text,
// Links und Zitate wie im Manifest, Anhänge mit erkanntem Typ und
// Widerspruch, Text der Anhänge per Extraktion (PDF nur mit pdftotext).
// Danach wird der Stand „vor S1.6" simuliert und der nächste Lauf baut
// alles aus den Rohmails wieder auf.
func TestCorpusCleanupAttachmentsAndExtraction(t *testing.T) {
	c := standardCorpus(t)
	addr, _ := memServer(t, true)
	e := newEnv(t, addr)
	eng := e.engine(t, connect(t, addr, 0))
	ex := extract.New(extract.Options{Timeout: 20 * time.Second})
	eng.Extractor, eng.ExtractBudget = ex, 5*time.Minute
	rep, err := eng.SyncAll(context.Background(), Options{})
	if err != nil || rep.Mirrored != memCount {
		t.Fatalf("sync = %+v %v", rep, err)
	}
	if rep.Extracted == 0 {
		t.Fatal("no attachment extracted")
	}
	pdfTool := ex.PDFTool() != ""
	check := func(round string) {
		t.Helper()
		db := e.st.DBForTest()
		if n := e.count(t, `SELECT count(*) FROM bodies WHERE hidden_text = 1`); n != 3 {
			t.Errorf("%s: messages with hidden text = %d, want 3", round, n)
		}
		if n := e.count(t, `SELECT count(*) FROM attachments WHERE type_mismatch = 1`); n != 2 {
			t.Errorf("%s: type mismatches = %d, want 2", round, n)
		}
		if n := e.count(t, `SELECT count(*) FROM attachments WHERE extract_status = 'pending'`); n != 0 {
			t.Errorf("%s: %d attachments still pending", round, n)
		}
		if n := e.count(t, `SELECT count(*) FROM bodies WHERE clean_version != ?`, CleanVersion); n != 0 {
			t.Errorf("%s: %d bodies with an old cleanup version", round, n)
		}
		for _, m := range c.Manifest.Messages {
			if m.Kind == "single" || m.Kind == "thread" || m.Kind == "newsletter" || m.Kind == "broken" || m.Kind == "zipbomb" {
				continue
			}
			var id int64
			var html, unquoted, links string
			var hidden int
			err := db.QueryRow(`SELECT m.id, b.text_from_html, b.text_unquoted, b.links, b.hidden_text FROM messages m
				JOIN bodies b ON b.message_id = m.id WHERE m.message_id_header = ?`, m.MessageID).Scan(&id, &html, &unquoted, &links, &hidden)
			if err != nil {
				t.Fatalf("%s: %s: %v", round, m.File, err)
			}
			if (hidden == 1) != m.HiddenText || strings.Contains(html, "Ignoriere") || strings.Contains(unquoted, "Ignoriere") {
				t.Errorf("%s: %s: hidden=%d, html %q", round, m.File, hidden, html)
			}
			if m.HTMLContains != "" && !strings.Contains(html, m.HTMLContains) {
				t.Errorf("%s: %s: html text lacks %q", round, m.File, m.HTMLContains)
			}
			if m.UnquotedContains != "" && (!strings.Contains(unquoted, m.UnquotedContains) || strings.Contains(unquoted, m.QuotedContains)) {
				t.Errorf("%s: %s: unquoted = %q", round, m.File, unquoted)
			}
			if len(m.Links) > 0 {
				var got []struct {
					Host     string   `json:"host"`
					Warnings []string `json:"warnings"`
				}
				_ = json.Unmarshal([]byte(links), &got)
				if len(got) != len(m.Links) {
					t.Fatalf("%s: links %s", round, links)
				}
				for k, want := range m.Links {
					w, ww := slices.Clone(got[k].Warnings), slices.Clone(want.Warnings)
					slices.Sort(w)
					slices.Sort(ww)
					if got[k].Host != want.Host || !slices.Equal(w, ww) {
						t.Errorf("%s: link %d = %+v, want %+v", round, k, got[k], want)
					}
				}
			}
			for _, x := range m.AttachmentTypes {
				var ct, status string
				var text, extErr sql.NullString
				var mism, exe, ahidden int
				err := db.QueryRow(`SELECT content_type, type_mismatch, executable, extract_status, extracted_text, extract_error, hidden_text
					FROM attachments WHERE message_id = ? AND filename = ?`, id, x.Name).Scan(&ct, &mism, &exe, &status, &text, &extErr, &ahidden)
				if err != nil {
					t.Fatalf("%s: %s %s: %v", round, m.File, x.Name, err)
				}
				if ct != x.Detected || (mism == 1) != x.Mismatch || (exe == 1) != x.Executable {
					t.Errorf("%s: %s: type %s mismatch %d exe %d, want %+v", round, x.Name, ct, mism, exe, x)
				}
				switch {
				case x.TextContains == "":
					if status == extract.StatusOK {
						t.Errorf("%s: %s: unexpected extraction", round, x.Name)
					}
				case x.TextNeedsTool == "pdftotext" && !pdfTool:
					if status != extract.StatusSkipped {
						t.Errorf("%s: %s: status %s without pdftotext", round, x.Name, status)
					}
				default:
					if status != extract.StatusOK || !strings.Contains(text.String, x.TextContains) {
						t.Errorf("%s: %s: status %s (%s), text %q", round, x.Name, status, extErr.String, text.String)
					}
				}
				if x.Name == "Aushang.html" && (ahidden != 1 || strings.Contains(text.String, "Ignoriere")) {
					t.Errorf("%s: html attachment hidden=%d text %q", round, ahidden, text.String)
				}
			}
		}
	}
	check("first sync")
	attachments := e.count(t, `SELECT count(*) FROM attachments`)

	// Bestand „vor S1.6": Texte ohne Aufbereitung, keine Anhänge.
	for _, q := range []string{`UPDATE bodies SET clean_version = 0, text_from_html = '', text_unquoted = '', links = '[]', hidden_text = 0`,
		`DELETE FROM attachments`} {
		if _, err := e.st.DBForTest().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := eng.SyncAll(context.Background(), Options{}); err != nil {
		t.Fatal(err)
	}
	check("after cleanup of older messages")
	if n := e.count(t, `SELECT count(*) FROM attachments`); n != attachments {
		t.Errorf("attachments after reprocessing = %d, want %d", n, attachments)
	}
	_ = corpus.Version
}
