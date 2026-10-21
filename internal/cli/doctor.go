package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DoctorReport ist die Ausgabe von `doctor`. Sie enthält Pfade, Zustände und
// Verweise, nie Geheimnisse.
type DoctorReport struct {
	Version        string `json:"version"`
	ConfigPath     string `json:"config_path"`
	ConfigExists   bool   `json:"config_exists"`
	ConfigError    string `json:"config_error,omitempty"`
	DataDir        string `json:"data_dir"`
	DatabasePath   string `json:"database_path"`
	LogPath        string `json:"log_path"`
	Database       string `json:"database"` // ok | missing | needs_migration | too_new | error
	SchemaVersion  int    `json:"schema_version"`
	SchemaKnown    int    `json:"schema_known"`
	SecretsBackend string `json:"secrets_backend"` // Konfiguration: auto | keyring | insecure-file
	Keyring        string `json:"keyring"`         // available | unavailable | insecure
	KeyringDetail  string `json:"keyring_detail,omitempty"`
	// PDFTool ist der Pfad zu pdftotext ("" = nicht gefunden; PDF-Anhänge
	// bleiben dann ohne Text). Optional, siehe README.
	PDFTool string `json:"pdftotext"`
	// Suchindex (S1.7): wartende Nachrichten und Stand von Normalisierer
	// und Stemmer (meta.fts_version).
	IndexPending int `json:"index_pending"`
	FTSVersion   int `json:"fts_version"`
	// ExtractPending: Anhänge, deren Text der nächste Sync noch extrahiert.
	ExtractPending int `json:"extract_pending"`
	// Audit-Export (S1.11): Ziel aus der Konfiguration, letzter Export,
	// Tage mit Einträgen ohne Export. Der tägliche Export läuft als
	// Windows-Aufgabe bzw. systemd-Timer (README, "Daily audit export").
	AuditExportDir       string          `json:"audit_export_dir"`
	AuditLastExport      string          `json:"audit_last_export,omitempty"`
	AuditExportLocation  string          `json:"audit_export_location,omitempty"`
	AuditDaysNotExported int             `json:"audit_days_not_exported"`
	Accounts             []DoctorAccount `json:"accounts"`
	// Secrets nur mit --secrets.
	Secrets  *DoctorSecrets `json:"secrets,omitempty"`
	Warnings []string       `json:"warnings"`
}

// DoctorAccount meldet je Konto, woher das Passwort käme.
type DoctorAccount struct {
	Name     string `json:"name"`
	Password string `json:"password"` // stored | environment | missing | unknown
	EnvVar   string `json:"env_var"`
	Status   string `json:"status"`
}

func doctorCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the installation: paths, keyring, database, schema version, audit export",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rep := doctor(cmd.Context(), a)
			if withSecrets, _ := cmd.Flags().GetBool("secrets"); withSecrets {
				rep.Secrets = doctorSecrets(cmd.Context(), a, rep)
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if err := json.NewEncoder(a.stdout).Encode(rep); err != nil {
					return err
				}
			} else {
				printDoctor(a.stdout, rep)
				if rep.Secrets != nil {
					printDoctorSecrets(a.stdout, rep.Secrets)
				}
			}
			if rep.ConfigError != "" {
				return errors.New("configuration is invalid: " + rep.ConfigError)
			}
			if rep.Secrets != nil && rep.Secrets.Problems > 0 {
				return fmt.Errorf("%w: %d problems (see the lines marked fail)", errSecretCheck, rep.Secrets.Problems)
			}
			return nil
		},
	}
	cmd.Flags().Bool("secrets", false, "also check file permissions and search log, audit, audit exports and configuration for stored passwords and API keys")
	return cmd
}

// doctor legt nichts an: Fehlt die Datenbank, meldet er das nur.
func doctor(ctx context.Context, a *app) DoctorReport {
	rep := DoctorReport{Version: a.version, ConfigPath: a.paths.ConfigFile, ConfigExists: a.paths.ConfigExists,
		DataDir: a.paths.DataDir, DatabasePath: a.paths.Database, LogPath: a.paths.Log,
		SchemaKnown: store.KnownSchemaVersion(), SecretsBackend: a.cfg.Secrets.Backend, AuditExportDir: a.cfg.Audit.ExportDir,
		Accounts: []DoctorAccount{}, Warnings: []string{}}
	if a.cfgErr != nil {
		rep.ConfigError = a.cfgErr.Error()
		if rep.ConfigPath == "" {
			rep.ConfigPath, _ = config.ConfigPath()
		}
		rep.Database, rep.Keyring = "not_checked", "not_checked"
		return rep
	}
	rep.PDFTool = extract.New(extract.Options{}).PDFTool()
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
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("database schema %d is older than this program (%d); the next command that opens it (e.g. accounts list) migrates it, with a backup copy next to the database",
			v, rep.SchemaKnown))
	default:
		rep.Database = "ok"
	}
	rep.IndexPending, _ = db.IndexPending(ctx, 0)
	rep.FTSVersion, _ = db.FTSVersion(ctx)
	rep.ExtractPending, _ = db.ExtractPending(ctx, 0)
	doctorAudit(ctx, db, &rep, time.Now())
	// Nur Spalten, die jedes Schema hat: doctor migriert nicht und muss die
	// Konten auch bei needs_migration lesen können.
	list, err := db.AccountRefs(ctx)
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
	doctorCredentialRefs(ctx, db, sec, list, &rep)
	return rep
}

// doctorCredentialRefs meldet den Stand des Umzugs der Schlüsselbund-Einträge
// (S2.0/A2): Konten mit altem Verweis und alte Einträge, die nach dem Umzug
// nicht gelöscht werden konnten und noch bestehen.
func doctorCredentialRefs(ctx context.Context, db *store.SQLite, sec secrets.Store, list []store.AccountRef, rep *DoctorReport) {
	dirID, err := db.DataDirID(ctx)
	if errors.Is(err, store.ErrNoDataDirID) {
		// Schema vor 8 (needs_migration): Jedes Konto hat noch den alten
		// Verweis; das nächste Kommando migriert und zieht dann um. Die
		// Kennung entsteht erst mit der Migration.
		for _, acc := range list {
			if acc.CredentialRef != secrets.LegacyAccountRef(acc.Name) {
				continue
			}
			if rep.Keyring == "unavailable" {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s still uses the old keyring entry %s; it moves to a name with the data directory id once the database is migrated and the OS keyring is reachable (%s is not affected)",
					acc.Name, acc.CredentialRef, secrets.EnvName(acc.CredentialRef)))
				continue
			}
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s still uses the old keyring entry %s; the next command (e.g. accounts list) migrates the database and moves it to a name with the data directory id",
				acc.Name, acc.CredentialRef))
		}
		return
	}
	if err != nil {
		rep.Warnings = append(rep.Warnings, "keyring: "+err.Error())
		return
	}
	for _, acc := range list {
		want := secrets.AccountRef(dirID, acc.Name)
		switch {
		case acc.CredentialRef == want:
		case rep.Keyring == "unavailable":
			// Ohne Schlüsselbund wird nichts umgezogen (ein gesperrter
			// Schlüsselbund sähe sonst leer aus); die Umgebungsvariable gilt.
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s still uses the old keyring entry %s; it moves to %s once the OS keyring is reachable (%s is not affected)",
				acc.Name, acc.CredentialRef, want, secrets.EnvName(want)))
		default:
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s still uses the old keyring entry %s; the next command (e.g. accounts list) moves it to %s",
				acc.Name, acc.CredentialRef, want))
		}
	}
	if sec == nil {
		return
	}
	left, err := db.CredentialRefsLeft(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "keyring: "+err.Error())
		return
	}
	base := secrets.Stored(sec)
	for _, ref := range left {
		if _, err := base.Get(ref); err == nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("old keyring entry %s is still there after the move and could not be deleted; delete it in the OS keyring (service %s, entry %s)",
				ref, brand.KeyringService, ref))
		}
	}
}

// auditExportStale: Ein Tag mit Einträgen, der so lange zurückliegt und noch
// nicht exportiert ist, ergibt eine Warnung (der tägliche Export fehlt).
const auditExportStale = 48 * time.Hour

// doctorAudit meldet den Stand des Audit-Exports.
func doctorAudit(ctx context.Context, db *store.SQLite, rep *DoctorReport, now time.Time) {
	days, err := db.AuditDays(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "audit: "+err.Error())
		return
	}
	done, err := db.AuditExports(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "audit: "+err.Error())
		return
	}
	exported := map[string]bool{}
	var last store.AuditExport
	for _, e := range done {
		exported[e.Day] = true
		if e.ExportedAt.After(last.ExportedAt) {
			last = e
		}
	}
	if !last.ExportedAt.IsZero() {
		rep.AuditLastExport = last.ExportedAt.UTC().Format(time.RFC3339)
		rep.AuditExportLocation = last.Location
	}
	stale := false
	for _, d := range days {
		if exported[d] {
			continue
		}
		rep.AuditDaysNotExported++
		if t, err := time.Parse("2006-01-02", d); err == nil && now.Sub(t.Add(24*time.Hour)) > auditExportStale {
			stale = true
		}
	}
	if stale {
		rep.Warnings = append(rep.Warnings, "audit: days older than two days are not exported; set up the daily audit export (README, \"Daily audit export\")")
	}
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
	if r.PDFTool != "" {
		fmt.Fprintf(w, "pdftotext: %s\n", r.PDFTool)
	} else {
		fmt.Fprintln(w, "pdftotext: not found (optional; without it PDF attachments get no text, see README)")
	}
	if r.Database == "ok" {
		fmt.Fprintf(w, "search:    %d messages waiting for the index (index version %d)\n", r.IndexPending, r.FTSVersion)
		fmt.Fprintf(w, "extract:   %d attachments waiting for text extraction\n", r.ExtractPending)
		dir := r.AuditExportDir
		if dir == "" {
			dir = "not set"
		}
		if r.AuditLastExport != "" {
			fmt.Fprintf(w, "audit:     last export %s to %s; %s not exported yet [audit.export_dir = %s]\n", r.AuditLastExport, r.AuditExportLocation, plural(r.AuditDaysNotExported, "day", "days"), dir)
		} else {
			fmt.Fprintf(w, "audit:     never exported; %s not exported yet [audit.export_dir = %s]\n", plural(r.AuditDaysNotExported, "day", "days"), dir)
		}
	}
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
