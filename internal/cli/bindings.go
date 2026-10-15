package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// binding verbindet genau eine Operation mit genau einem Kommando.
// Bewusst eine handgeschriebene Tabelle (ADR-004): Namen, Hilfetexte und
// Ausgabeformate sind Entscheidungen, keine Ableitungen.
type binding struct {
	op    string                                  // Name im Register
	path  []string                                // Kommandopfad, z. B. {"accounts","list"}
	human func(w io.Writer, out ops.Result) error // Ausgabe ohne --json
}

var bindings = []binding{
	{op: "accounts.list", path: []string{"accounts", "list"}, human: renderAccounts},
}

// Deps sind die Abhängigkeiten der Adapter.
type Deps struct {
	Exec *ops.Executor
	// Key ist der Schlüssel, unter dem die CLI handelt. Die CLI ist die
	// menschliche Oberfläche; Agenten-Schlüssel (--key) kommen mit Schnitt 1.
	Key policy.Key
}

// bindOperations hängt alle Bindungen unter root.
func bindOperations(root *cobra.Command, d Deps, stdout io.Writer) error {
	groups := map[string]*cobra.Command{}
	for _, b := range bindings {
		op, err := d.Exec.Registry.Get(b.op)
		if err != nil {
			return fmt.Errorf("cli: binding for unregistered operation: %w", err)
		}
		if len(b.path) != 2 {
			return fmt.Errorf("cli: binding %s: path must be <object> <verb>", b.op)
		}
		group, ok := groups[b.path[0]]
		if !ok {
			group = &cobra.Command{Use: b.path[0], Short: "Commands for " + b.path[0]}
			groups[b.path[0]] = group
			root.AddCommand(group)
		}
		b := b // Schleifenvariable pro Kommando
		group.AddCommand(&cobra.Command{
			Use:   b.path[1],
			Short: op.Summary,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				req := ops.Request{
					Key:       d.Key,
					Actor:     audit.ActorHuman,
					Operation: b.op,
					Input:     json.RawMessage(`{}`),
				}
				if cmd.Flags().Changed("dry-run") {
					v, _ := cmd.Flags().GetBool("dry-run")
					req.DryRun = &v
				}
				res, err := d.Exec.Execute(cmd.Context(), req)
				if err != nil {
					return err
				}
				if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
					return json.NewEncoder(stdout).Encode(res.Output)
				}
				return b.human(stdout, res)
			},
		})
	}
	return nil
}

func renderAccounts(w io.Writer, res ops.Result) error {
	out, ok := res.Output.(struct {
		Accounts []ops.AccountInfo `json:"accounts"`
	})
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", res.Output)
	}
	if len(out.Accounts) == 0 {
		_, err := fmt.Fprintln(w, "No accounts configured yet.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tEMAIL\tPROVIDER\tUNREAD\tLAST SYNC")
	for _, a := range out.Accounts {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%s\n", a.ID, a.Name, a.Email, a.Provider, a.UnreadTotal, a.LastSyncAt)
	}
	return tw.Flush()
}
