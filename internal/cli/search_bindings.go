package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// --- search (S1.7) --------------------------------------------------------------

func searchFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.Int("limit", ops.DefaultSearchLimit, fmt.Sprintf("hits per page (1-%d)", ops.MaxSearchLimit))
	f.String("cursor", "", "continue after this position (next_cursor or a hit's cursor of the same query)")
	f.String("mailbox", "", "only this mailbox: path (with subfolders) or role (same as in:)")
	f.String("fields", "", "with --json: only these fields per hit, e.g. id,subject,date")
}

// searchInput: Mehrere Argumente bilden zusammen die Anfrage
// (`search from:anna Rechnung` wie `search "from:anna Rechnung"`).
func searchInput(cmd *cobra.Command, args []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	in := map[string]any{"query": strings.Join(args, " ")}
	if acct := accountFlag(cmd); acct != "" {
		in["account"] = acct
	}
	if f.Changed("limit") {
		n, _ := f.GetInt("limit")
		in["limit"] = n
	}
	if c, _ := f.GetString("cursor"); c != "" {
		in["cursor"] = c
	}
	if m, _ := f.GetString("mailbox"); m != "" {
		in["mailbox"] = m
	}
	if fl, _ := f.GetString("fields"); fl != "" {
		if _, err := hitFields(fl); err != nil {
			return nil, "", err
		}
	}
	if x, _ := f.GetBool("explain"); x {
		in["explain"] = true
	}
	b, err := json.Marshal(in)
	return b, "", err
}

// hitFieldNames sind die Felder eines Treffers (SearchHitOutput).
var hitFieldNames = []string{"id", "account", "thread_id", "date", "from", "subject", "snippet", "flags", "has_attachments",
	"deleted_remote", "score", "cursor"}

// hitFields liest --fields (kommagetrennt) und prüft die Namen.
func hitFields(s string) ([]string, error) {
	var out []string
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !slices.Contains(hitFieldNames, f) {
			return nil, usageError{fmt.Errorf("unknown field %q for --fields (known: %s)", f, strings.Join(hitFieldNames, ","))}
		}
		out = append(out, f)
	}
	return out, nil
}

// untrustedHit ist eine NDJSON-Zeile von search --json.
// Die Felder des Treffers stehen vorn (die Zeile beginnt weiter mit "id").
type untrustedHit struct {
	ops.SearchHitOutput
	Untrusted bool   `json:"untrusted"`
	Source    string `json:"source"`
}

// searchJSON: NDJSON, eine Zeile je Treffer (jeder mit seinem Cursor; mit
// --fields nur die genannten Felder). Mit --explain steht davor eine Zeile
// {"explain":…, "total":…, …}.
func searchJSON(cmd *cobra.Command, w io.Writer, out any) error {
	o, ok := out.(ops.SearchOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	fl, _ := cmd.Flags().GetString("fields")
	fields, err := hitFields(fl)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	if o.Explain != nil {
		head := struct {
			Explain      *ops.SearchExplain `json:"explain"`
			Total        int                `json:"total"`
			Order        string             `json:"order"`
			IndexPending int                `json:"index_pending"`
			NextCursor   string             `json:"next_cursor,omitempty"`
		}{o.Explain, o.Total, o.Order, o.IndexPending, o.NextCursor}
		if err := enc.Encode(head); err != nil {
			return err
		}
	}
	for _, h := range o.Hits {
		// Jede Zeile trägt die Kennzeichnung der Untrusted-Hülle (Betreff,
		// Absender und Snippet stammen aus Mails); den Hinweis notice gibt es
		// nur in der Ausgabe ohne --json bzw. über MCP. Kein Kopf als eigene
		// Zeile: `| head -n 3` liefert weiter drei Treffer.
		var v any = untrustedHit{SearchHitOutput: h, Untrusted: o.Untrusted, Source: o.Source}
		if len(fields) > 0 {
			b, err := json.Marshal(v)
			if err != nil {
				return err
			}
			var all map[string]json.RawMessage
			if err := json.Unmarshal(b, &all); err != nil {
				return err
			}
			sel := map[string]json.RawMessage{"untrusted": all["untrusted"], "source": all["source"]}
			for _, f := range fields {
				if raw, ok := all[f]; ok {
					sel[f] = raw
				}
			}
			v = sel
		}
		if err := enc.Encode(v); err != nil {
			return err
		}
	}
	return nil
}

// cut kürzt eine Zeile für die Tabelle.
func cut(s string, n int) string {
	s = termLine(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func renderSearch(w io.Writer, out any) error {
	o, ok := out.(ops.SearchOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if x := o.Explain; x != nil {
		fmt.Fprintf(w, "query:   %s\nscope:   %s\norder:   %s\n", termLine(x.Query), x.Scope,
			map[string]string{"relevance": "relevance (BM25 and date)", "date": "date (newest first)"}[x.Order])
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		if len(x.Terms) > 0 {
			fmt.Fprintln(tw, "terms:\tTERM\tFIELD\tMODE\tWORD INDEX\tSUBSTRING (TRIGRAM)")
		}
		for _, t := range x.Terms {
			text := t.Text
			if t.Phrase {
				text = `"` + text + `"`
			}
			if t.Negated {
				text = "-" + text
			}
			field := t.Field
			if field == "" {
				field = "all"
			}
			fmt.Fprintf(tw, "\t%s\t%s\t%s\t%s\t%s\n", termLine(text), field, t.Mode, orDash(termLine(t.Word)), orDash(termLine(t.Tri)))
		}
		_ = tw.Flush()
		for _, f := range x.Filters {
			not := ""
			if f.Negated {
				not = "not "
			}
			fmt.Fprintf(w, "filter:  %s%s %s\n", not, f.Kind, termLine(f.Value))
		}
		if len(x.Ignored) > 0 {
			fmt.Fprintf(w, "ignored: %s\n", strings.Join(termLines(x.Ignored), " "))
		}
		fmt.Fprintf(w, "sql:     %s\n\n", strings.Join(strings.Fields(x.SQL), " "))
	}
	if len(o.Hits) == 0 {
		fmt.Fprintln(w, "No messages found.")
	} else {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tDATE\tFROM\tSUBJECT")
		for _, h := range o.Hits {
			date := ""
			if t, err := time.Parse(time.RFC3339, h.Date); err == nil {
				date = t.Local().Format("2006-01-02 15:04")
			}
			subject := cut(h.Subject, 80)
			if h.HasAttachments {
				subject += " [attachment]"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", h.ID, date, cut(h.From, 40), subject)
		}
		_ = tw.Flush()
		order := map[string]string{"relevance": "by relevance", "date": "newest first"}[o.Order]
		fmt.Fprintf(w, "%d of %d results (%s)\n", len(o.Hits), o.Total, order)
	}
	if o.NextCursor != "" {
		fmt.Fprintf(w, "more: --cursor %s\n", o.NextCursor)
	}
	if o.IndexPending > 0 {
		fmt.Fprintf(w, "note: %d messages are not in the search index yet; run sync to finish it\n", o.IndexPending)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
