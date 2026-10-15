package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// NewRoot baut den Kommandobaum. Die Kette Register → Executor wird hier
// verdrahtet; in Phase 0 mit leerer Kontenquelle und Nop-Audit, ab Schnitt 1
// mit Store und SQLite-Audit.
func NewRoot(version string, stdout, stderr io.Writer) (*cobra.Command, error) {
	reg := ops.NewRegistry()
	if err := reg.Register(ops.AccountsList(ops.EmptyAccounts{})); err != nil {
		return nil, err
	}
	deps := Deps{
		Exec: &ops.Executor{Registry: reg, Audit: audit.Nop{}},
		Key:  policy.Key{ID: "cli", Name: "cli (human)", Scopes: policy.Roles["admin"]},
	}

	root := &cobra.Command{
		Use:           brand.Name,
		Short:         "Agent-first e-mail client (working title; pre-alpha)",
		SilenceUsage:  true,
		SilenceErrors: true, // Fehler gibt Run aus, einheitlich und maschinenlesbar
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.PersistentFlags().Bool("json", false, "machine-readable output (JSON); errors as JSON on stderr")
	root.PersistentFlags().Bool("dry-run", false, "describe what would happen without changing anything")

	root.AddCommand(versionCmd(version, stdout), doctorCmd(version, stdout))
	if err := bindOperations(root, deps, stdout); err != nil {
		return nil, err
	}
	return root, nil
}

// Run führt die CLI aus und liefert den Exit-Code.
func Run(args []string, stdout, stderr io.Writer, version string) int {
	root, err := NewRoot(version, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return ExitError
	}
	root.SetArgs(args)
	err = root.Execute()
	if err == nil {
		return ExitOK
	}
	code, info := classify(err)
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
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand flag", "accepts ", "requires "} {
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

// ConfigPath liefert den Pfad der Konfigurationsdatei: POSTKAUZ_CONFIG, sonst
// <Konfigurationsordner des Betriebssystems>/postkauz/config.toml.
func ConfigPath() (string, error) {
	if p := os.Getenv(brand.EnvPrefix + "_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, brand.ConfigDir, "config.toml"), nil
}

// DoctorReport ist die Ausgabe von `doctor` (Stub, Phase 0).
type DoctorReport struct {
	Version      string `json:"version"`
	ConfigPath   string `json:"config_path"`
	ConfigExists bool   `json:"config_exists"`
	Keyring      string `json:"keyring"`
	Database     string `json:"database"`
	Note         string `json:"note"`
}

func doctorCmd(version string, stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the installation (stub: reports paths only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := ConfigPath()
			if err != nil {
				return err
			}
			_, statErr := os.Stat(path)
			rep := DoctorReport{
				Version:      version,
				ConfigPath:   path,
				ConfigExists: statErr == nil,
				Keyring:      "not_checked", // Platzhalter: go-keyring wird erst mit Schnitt 1 angebunden
				Database:     "not_checked",
				Note:         "doctor is a stub in phase 0; keyring, database, capabilities and extractor checks follow in slice 1",
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return json.NewEncoder(stdout).Encode(rep)
			}
			fmt.Fprintf(stdout, "version:  %s\nconfig:   %s (exists: %v)\nkeyring:  %s\ndatabase: %s\nnote:     %s\n",
				rep.Version, rep.ConfigPath, rep.ConfigExists, rep.Keyring, rep.Database, rep.Note)
			return nil
		},
	}
}
