package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// runScenario fuehrt den kompletten Online-Ablauf gegen einen Server aus.
func runScenario(cfg *config) (map[string]any, error) {
	out := map[string]any{}
	step := func(name string, fn func() (any, error)) error {
		logf(cfg, "Schritt %s", name)
		t0 := time.Now()
		r, err := fn()
		entry := map[string]any{"result": r, "seconds": time.Since(t0).Seconds()}
		if err != nil {
			entry["error"] = err.Error()
		}
		out[name] = entry
		return err
	}
	os.RemoveAll(cfg.DataDir)
	if err := step("1_caps", func() (any, error) { return runCaps(cfg) }); err != nil {
		return out, err
	}
	if err := step("2_sync_initial", func() (any, error) { return runSync(cfg) }); err != nil {
		return out, err
	}
	if err := step("3_mutate", func() (any, error) { return runMutate(cfg) }); err != nil {
		return out, err
	}
	if err := step("4_sync_incremental", func() (any, error) { return runSync(cfg) }); err != nil {
		return out, err
	}
	_ = step("5_sync_noop", func() (any, error) { return runSync(cfg) })
	_ = step("6_draft", func() (any, error) { return runDraft(cfg) })
	_ = step("7_idle_reconnect", func() (any, error) { return runIdle(cfg) })
	_ = step("8_qresync_probe", func() (any, error) { return runQResync(cfg) })
	return out, nil
}

// ftsQuery ist eine Testabfrage mit Erwartung aus einem Klartext-Scan.
type ftsQuery struct {
	Table    string `json:"table"`
	Match    string `json:"match"`
	Hits     int    `json:"hits"`
	Expected int    `json:"expected_substring_hits"`
	Note     string `json:"note,omitempty"`
	Error    string `json:"error,omitempty"`
}

func runFTS(cfg *config) (map[string]any, error) {
	out := map[string]any{}
	dir, err := os.MkdirTemp("", "mw-fts-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "fts.db"))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var ver string
	_ = db.QueryRow(`SELECT sqlite_version()`).Scan(&ver)
	out["sqlite_version"] = ver
	var opts []string
	rows, err := db.Query(`PRAGMA compile_options`)
	if err == nil {
		for rows.Next() {
			var o string
			_ = rows.Scan(&o)
			if strings.Contains(o, "FTS") || strings.Contains(o, "ICU") || strings.Contains(o, "JSON") {
				opts = append(opts, o)
			}
		}
		rows.Close()
	}
	out["compile_options_fts"] = opts
	tok := map[string]string{}
	for _, t := range []string{"unicode61", "unicode61 remove_diacritics 2", "ascii", "porter unicode61",
		"trigram", "trigram case_sensitive 0 remove_diacritics 1", "icu"} {
		_, err := db.Exec(fmt.Sprintf(`CREATE VIRTUAL TABLE t_probe USING fts5(x, tokenize=%q)`, t))
		if err != nil {
			tok[t] = "nein: " + err.Error()
		} else {
			tok[t] = "ja"
			_, _ = db.Exec(`DROP TABLE t_probe`)
		}
	}
	out["tokenizers"] = tok

	// Index aus dem Korpus aufbauen (Text via go-message)
	list, err := loadManifestList(cfg.Corpus)
	if err != nil {
		return out, err
	}
	for _, ddl := range []string{
		`CREATE VIRTUAL TABLE f_uni USING fts5(subject, body, tokenize='unicode61 remove_diacritics 2')`,
		`CREATE VIRTUAL TABLE f_tri USING fts5(subject, body, tokenize='trigram remove_diacritics 1')`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			return out, err
		}
	}
	texts := make([]string, 0, len(list))
	t0 := time.Now()
	tx, _ := db.Begin()
	for i, e := range list {
		raw, err := os.ReadFile(filepath.Join(cfg.Corpus, e.File))
		if err != nil {
			return out, err
		}
		pm := safeParse(parseGoMessage, raw)
		texts = append(texts, strings.ToLower(pm.Subject+"\n"+pm.Text))
		for _, tbl := range []string{"f_uni", "f_tri"} {
			if _, err := tx.Exec(`INSERT INTO `+tbl+`(rowid, subject, body) VALUES(?,?,?)`, i+1, pm.Subject, pm.Text); err != nil {
				tx.Rollback()
				return out, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return out, err
	}
	out["index_build_s_both_tables"] = time.Since(t0).Seconds()
	count := func(sub string) int {
		n := 0
		for _, t := range texts {
			if strings.Contains(t, strings.ToLower(sub)) {
				n++
			}
		}
		return n
	}
	var qs []ftsQuery
	add := func(tbl, match, substr, note string) {
		q := ftsQuery{Table: tbl, Match: match, Note: note}
		if substr != "" {
			q.Expected = count(substr)
		}
		err := db.QueryRow(`SELECT count(*) FROM `+tbl+` WHERE `+tbl+` MATCH ?`, match).Scan(&q.Hits)
		if err != nil {
			q.Error = err.Error()
		}
		qs = append(qs, q)
	}
	add("f_uni", `"Zählerstände"`, "zählerstände", "ganzes Wort mit Umlaut")
	add("f_uni", `zahlerstande`, "zählerstände", "ohne Umlaute (remove_diacritics 2)")
	add("f_uni", `Grundsteuer`, "grundsteuer", "")
	add("f_uni", `steuer`, "steuer", "Teilwort -> unicode61 findet nur ganze Token")
	add("f_uni", `Grund*`, "grund", "Praefixsuche")
	add("f_uni", `Erdgeschoß`, "erdgeschoß", "ß")
	add("f_uni", `Erdgeschoss`, "erdgeschoss", "ß vs ss: keine Faltung")
	add("f_tri", `"steuer"`, "steuer", "Teilwort per Trigram")
	add("f_tri", `"ählerst"`, "ählerst", "Teilwort mit Umlaut")
	add("f_tri", `"ahlerst"`, "ählerst", "Teilwort ohne Umlaut (remove_diacritics 1)")
	add("f_tri", `"gärtner"`, "gärtner", "")
	add("f_tri", `"üb"`, "", "2 Zeichen: Trigram braucht >= 3")
	out["queries"] = qs
	return out, nil
}
