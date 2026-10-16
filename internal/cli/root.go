package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// NewRoot baut den Kommandobaum (stdin = os.Stdin).
func NewRoot(version string, stdout, stderr io.Writer) (*cobra.Command, error) {
	root, _, err := newRoot(version, os.Stdin, stdout, stderr)
	return root, err
}

func newRoot(version string, stdin io.Reader, stdout, stderr io.Writer) (*cobra.Command, *app, error) {
	a := newApp(version, stdin, stdout, stderr)
	// Register ohne Abhängigkeiten: für Hilfe und Bindungsprüfung. Der
	// Executor mit Store, Schlüsselbund und SQLite-Audit entsteht erst, wenn
	// ein Kommando läuft (app.open).
	reg, err := ops.NewDefaultRegistry(ops.Deps{})
	if err != nil {
		return nil, nil, err
	}
	root := &cobra.Command{
		Use:           brand.Name,
		Short:         "Agent-first e-mail client (working title; pre-alpha)",
		SilenceUsage:  true,
		SilenceErrors: true, // Fehler gibt Run aus, einheitlich und maschinenlesbar
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetIn(stdin)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	pf := root.PersistentFlags()
	pf.Bool("json", false, "machine-readable output (JSON; lists as NDJSON); errors as JSON on stderr")
	pf.Bool("dry-run", false, "describe what would happen without changing anything")
	pf.StringP("account", "a", "", "account name (default: the only configured account)")
	pf.String("key", "", "act with the rights of this API key (its name, never the key itself) to test the policy")
	pf.BoolP("quiet", "q", false, "no progress output")
	pf.String("profile", "", "separate instance: own configuration and data in profiles/<name>")
	pf.Bool("explain", false, "show the policy decision and the audit event id on stderr (search: also the query translation)")
	// --profile wirkt vor jedem Kommando: Konfiguration und Pfade neu laden
	// (newApp hat sie ohne Profil geladen, bevor die Flags gelesen waren).
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if p, _ := cmd.Flags().GetString("profile"); p != "" {
			a.cfg, a.paths, a.cfgErr = config.LoadProfile(p)
			if a.cfgErr != nil && strings.Contains(a.cfgErr.Error(), "invalid profile name") {
				return usageError{a.cfgErr}
			}
		}
		return nil
	}

	root.AddCommand(versionCmd(version, stdout), doctorCmd(a))
	if err := bindOperations(root, reg, a); err != nil {
		return nil, nil, err
	}
	return root, a, nil
}

// Run führt die CLI aus und liefert den Exit-Code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	return RunWithInput(args, os.Stdin, stdout, stderr, version)
}

// RunWithInput ist Run mit eigener Standardeingabe (Tests, --password-stdin).
func RunWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer, version string) int {
	root, a, err := newRoot(version, stdin, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return ExitError
	}
	defer a.close()
	root.SetArgs(args)
	// Strg+C beendet laufende Operationen geordnet (Sync: das laufende
	// Fenster wird verworfen, der gespeicherte Stand bleibt).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	code, info := classify(err)
	if ctx.Err() != nil {
		code, info = ExitError, ErrorInfo{Code: "interrupted", Message: "interrupted",
			Hint: "progress up to the last completed window is saved; run the same command again to resume"}
	}
	// Unbekannte Kommandos meldet cobra als einfachen Fehler: als Eingabefehler behandeln.
	if code == ExitError && info.Code == "error" && isCobraUsage(err) {
		code, info = ExitUsage, ErrorInfo{Code: "usage", Message: err.Error(), Hint: "see --help"}
	}
	if slices.Contains(args, "--json") {
		_ = json.NewEncoder(stderr).Encode(map[string]ErrorInfo{"error": info})
	} else {
		fmt.Fprintln(stderr, "error:", info.Message)
		if info.Hint != "" {
			fmt.Fprintln(stderr, "hint:", info.Hint)
		}
	}
	return code
}

func isCobraUsage(err error) bool {
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "accepts ", "requires ", "required flag"} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

func versionCmd(version string, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := map[string]any{
				"name":           brand.Name,
				"version":        version,
				"go":             runtime.Version(),
				"os":             runtime.GOOS,
				"arch":           runtime.GOARCH,
				"schema_version": ops.SchemaVersion,
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return json.NewEncoder(stdout).Encode(info)
			}
			_, err := fmt.Fprintf(stdout, "%s %s (%s %s/%s)\n", brand.Name, version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}
