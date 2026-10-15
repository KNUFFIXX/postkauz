package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DoctorReport ist die Ausgabe von `doctor`. Sie enthält Pfade, Zustände und
// Verweise, nie Geheimnisse.
type DoctorReport struct {
	Version        string          `json:"version"`
	ConfigPath     string          `json:"config_path"`
	ConfigExists   bool            `json:"config_exists"`
	ConfigError    string          `json:"config_error,omitempty"`
	DataDir        string          `json:"data_dir"`
	DatabasePath   string          `json:"database_path"`
	LogPath        string          `json:"log_path"`
	Database       string          `json:"database"` // ok | missing | needs_migration | too_new | error
	SchemaVersion  int             `json:"schema_version"`
	SchemaKnown    int             `json:"schema_known"`
	SecretsBackend string          `json:"secrets_backend"` // Konfiguration: auto | keyring | insecure-file
	Keyring        string          `json:"keyring"`         // available | unavailable | insecure
	KeyringDetail  string          `json:"keyring_detail,omitempty"`
	Accounts       []DoctorAccount `json:"accounts"`
	Warnings       []string        `json:"warnings"`
}

// DoctorAccount meldet je Konto, woher das Passwort käme.
type DoctorAccount struct {
	Name     string `json:"name"`
	Password string `json:"password"` // stored | environment | missing | unknown
	EnvVar   string `json:"env_var"`
	Status   string `json:"status"`
}

func doctorCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the installation: paths, keyring, database, schema version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := doctor(cmd.Context(), a)
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if err := json.NewEncoder(a.stdout).Encode(rep); err != nil {
					return err
				}
			} else {
				printDoctor(a.stdout, rep)
			}
			if rep.ConfigError != "" {
				return errors.New("configuration is invalid: " + rep.ConfigError)
			}
			return nil
		},
	}
}

// doctor legt nichts an: Fehlt die Datenbank, meldet er das nur.
func doctor(ctx context.Context, a *app) DoctorReport {
	rep := DoctorReport{Version: a.version, ConfigPath: a.paths.ConfigFile, ConfigExists: a.paths.ConfigExists,
		DataDir: a.paths.DataDir, DatabasePath: a.paths.Database, LogPath: a.paths.Log,
		SchemaKnown: store.KnownSchemaVersion(), SecretsBackend: a.cfg.Secrets.Backend,
		Accounts: []DoctorAccount{}, Warnings: []string{}}
	if a.cfgErr != nil {
		rep.ConfigError = a.cfgErr.Error()
		if rep.ConfigPath == "" {
			rep.ConfigPath, _ = config.ConfigPath()
		}
		rep.Database, rep.Keyring = "not_checked", "not_checked"
		return rep
	}
	if w := config.CloudSyncWarning(a.paths.DataDir); w != "" {
		rep.Warnings = append(rep.Warnings, w)
	}

	sec, err := openSecrets(a.cfg.Secrets.Backend, a.paths.SecretsDir)
	switch {
	case err != nil:
		rep.Keyring, rep.KeyringDetail = "unavailable", err.Error()
	default:
		st := sec.Status()
		rep.KeyringDetail = st.Detail
		switch {
		case st.Insecure:
			rep.Keyring = "insecure"
			rep.Warnings = append(rep.Warnings, "secrets.backend = insecure-file: passwords are only obfuscated; use it for test servers only")
		case st.Available:
			rep.Keyring = "available"
		default:
			rep.Keyring = "unavailable"
			rep.Warnings = append(rep.Warnings, "no OS keyring: `account add` cannot store passwords; set POSTKAUZ_PASSWORD_<NAME> and use --password-from-env, or secrets.backend = \"insecure-file\" for test servers")
		}
	}

	if _, err := os.Stat(a.paths.Database); errors.Is(err, os.ErrNotExist) {
		rep.Database = "missing"
		return rep
	}
	db, err := store.Open(a.paths.Database)
	if err != nil {
		rep.Database = "error"
		rep.Warnings = append(rep.Warnings, "database: "+err.Error())
		return rep
	}
	defer db.Close()
	v, err := db.SchemaVersion(ctx)
	rep.SchemaVersion = v
	switch {
	case err != nil:
		rep.Database = "error"
		rep.Warnings = append(rep.Warnings, "database: "+err.Error())
		return rep
	case v > rep.SchemaKnown:
		rep.Database = "too_new"
		rep.Warnings = append(rep.Warnings, "database schema is newer than this program; use a newer version")
		return rep
	case v < rep.SchemaKnown:
		rep.Database = "needs_migration" // wird beim nächsten Kommando angewendet
		if v == 0 {
			return rep
		}
	default:
		rep.Database = "ok"
	}
	list, err := db.ListAccounts(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "accounts: "+err.Error())
		return rep
	}
	for _, acc := range list {
		da := DoctorAccount{Name: acc.Name, EnvVar: secrets.EnvName(acc.CredentialRef), Status: acc.Status, Password: "unknown"}
		if v, ok := os.LookupEnv(da.EnvVar); ok && v != "" {
			da.Password = "environment"
		} else if sec != nil {
			switch _, err := sec.Get(acc.CredentialRef); {
			case err == nil:
				da.Password = "stored"
			case errors.Is(err, secrets.ErrNotFound):
				da.Password = "missing"
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s has no password (keyring entry %s or %s)", acc.Name, acc.CredentialRef, da.EnvVar))
			}
		}
		rep.Accounts = append(rep.Accounts, da)
	}
	return rep
}

func printDoctor(w io.Writer, r DoctorReport) {
	fmt.Fprintf(w, "version:   %s\n", r.Version)
	fmt.Fprintf(w, "config:    %s (exists: %v)\n", r.ConfigPath, r.ConfigExists)
	if r.ConfigError != "" {
		fmt.Fprintf(w, "           error: %s\n", r.ConfigError)
	}
	fmt.Fprintf(w, "data dir:  %s\n", r.DataDir)
	fmt.Fprintf(w, "log:       %s\n", r.LogPath)
	fmt.Fprintf(w, "database:  %s (%s)\n", r.Database, r.DatabasePath)
	fmt.Fprintf(w, "schema:    %d (program knows %d)\n", r.SchemaVersion, r.SchemaKnown)
	kr := r.Keyring
	if r.KeyringDetail != "" {
		kr += " (" + r.KeyringDetail + ")"
	}
	fmt.Fprintf(w, "keyring:   %s [secrets.backend = %s]\n", kr, r.SecretsBackend)
	if len(r.Accounts) > 0 {
		fmt.Fprintln(w, "accounts:")
		for _, a := range r.Accounts {
			fmt.Fprintf(w, "  %s: password %s, status %s (env override: %s)\n", a.Name, a.Password, a.Status, a.EnvVar)
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "warnings:")
		for _, x := range r.Warnings {
			fmt.Fprintln(w, "  - "+strings.TrimSpace(x))
		}
	}
}
