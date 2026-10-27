package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DoctorReport ist die Ausgabe von `doctor`. Sie enthält Pfade, Zustände und
// Verweise, nie Geheimnisse.
type DoctorReport struct {
	Version      string `json:"version"`
	ConfigPath   string `json:"config_path"`
	ConfigExists bool   `json:"config_exists"`
	ConfigError  string `json:"config_error,omitempty"`
	DataDir      string `json:"data_dir"`
	// Windows (A7): echter Pfad, wenn Windows Datenverzeichnis bzw.
	// Konfigurationsdatei in den Ordner eines App-Pakets umleitet, und der
	// Name des Pakets, in dem doctor läuft. Leer = nicht umgeleitet.
	DataDirReal    string `json:"data_dir_real,omitempty"`
	ConfigPathReal string `json:"config_path_real,omitempty"`
	AppPackage     string `json:"app_package,omitempty"`
	// DataDirShadow: Datenverzeichnis gleichen Namens mit Datenbank im
	// Paketordner, das doctor nicht sieht (A7, Gegenprüfung).
	DataDirShadow  string `json:"data_dir_shadow,omitempty"`
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
	// Policy-Datei (S2.1, A02 E8): Pfad, Zustand (ok | default = keine
	// Datei, eingebaute Standards | invalid), SHA-256, Version in der
	// Tabelle policies (0 = noch nicht eingetragen; das nächste Kommando
	// trägt sie ein) und der Fehler einer ungültigen Datei.
	PolicyPath    string `json:"policy_path"`
	Policy        string `json:"policy"`
	PolicySHA256  string `json:"policy_sha256,omitempty"`
	PolicyVersion int64  `json:"policy_version"`
	PolicyError   string `json:"policy_error,omitempty"`
	// AttachDir: Ordner für Dateianhänge von Entwürfen ([paths] attach_dir).
	AttachDir string `json:"attach_dir"`
	// Secrets nur mit --secrets, Blobs nur mit --blobs.
	Secrets  *DoctorSecrets `json:"secrets,omitempty"`
	Blobs    *DoctorBlobs   `json:"blobs,omitempty"`
	Warnings []string       `json:"warnings"`
}

// DoctorAccount meldet je Konto, woher das Passwort käme.
type DoctorAccount struct {
	Name     string `json:"name"`
	Password string `json:"password"` // stored | environment | missing | unknown | oauth2 (kein Passwort)
	EnvVar   string `json:"env_var"`
	Status   string `json:"status"`
	// OAuth2-Konten (S3.7): Token stored | missing | unreadable | unknown;
	// Ablauf des zuletzt ausgegebenen Access-Tokens (es bleibt im Speicher
	// und wird erneuert) und Zeitpunkt der letzten Anmeldung bzw. des
	// letzten Refresh (RFC 3339, UTC). Nie ein Token.
	Auth           string `json:"auth,omitempty"`
	Token          string `json:"token,omitempty"`
	TokenExpiry    string `json:"token_expiry,omitempty"`
	TokenUpdatedAt string `json:"token_updated_at,omitempty"`
	// IncompleteAnswers: leere oder unvollständige Serverantworten bisher
	// (LIST ohne INBOX, UID-Liste passt nicht zu EXISTS; K2), Last und
	// LastAt die des letzten Laufs mit solchen Antworten.
	IncompleteAnswers     int        `json:"incomplete_answers"`
	IncompleteAnswersLast int        `json:"incomplete_answers_last,omitempty"`
	IncompleteAnswersAt   *time.Time `json:"incomplete_answers_at,omitempty"`
}

func doctorCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the installation: paths, keyring, database, schema version, audit export",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			flag, _ := cmd.Flags().GetString("blobs")
			blobMode, err := parseBlobsFlag(flag)
			if err != nil {
				return err
			}
			rep := doctor(cmd.Context(), a)
			if withSecrets, _ := cmd.Flags().GetBool("secrets"); withSecrets {
				rep.Secrets = doctorSecrets(cmd.Context(), a, rep)
			}
			if blobMode != "" {
				rep.Blobs = doctorBlobs(cmd.Context(), a, rep, blobMode)
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
				if rep.Blobs != nil {
					printDoctorBlobs(a.stdout, rep.Blobs)
				}
			}
			if rep.ConfigError != "" {
				return errors.New("configuration is invalid: " + rep.ConfigError)
			}
			if rep.PolicyError != "" {
				return errors.New(rep.PolicyError)
			}
			if rep.Secrets != nil && rep.Secrets.Problems > 0 {
				return fmt.Errorf("%w: %d problems (see the lines marked fail)", errSecretCheck, rep.Secrets.Problems)
			}
			if rep.Blobs != nil && rep.Blobs.Problems > 0 {
				return fmt.Errorf("%w: %d problems (see the section blobs)", errBlobCheck, rep.Blobs.Problems)
			}
			return nil
		},
	}
	cmd.Flags().Bool("secrets", false, "also check file permissions and search log, audit, audit exports and configuration for stored passwords and API keys")
	cmd.Flags().String("blobs", "", "also verify the blob store against the database: a 1% sample of the files (hash and size), or every file with --blobs=all; counts rows without file and files without row, never shows content")
	cmd.Flags().Lookup("blobs").NoOptDefVal = "sample"
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
	rep.AttachDir = a.paths.AttachDir
	pf := doctorPolicy(&rep, a.paths.ConfigFile)
	if w := config.CloudSyncWarning(a.paths.DataDir); w != "" {
		rep.Warnings = append(rep.Warnings, w)
	}
	redir := config.CheckRedirects(a.paths)
	rep.DataDirReal, rep.ConfigPathReal, rep.AppPackage = redir.DataDirReal, redir.ConfigReal, redir.Package
	rep.DataDirShadow = redir.DataDirShadow
	rep.Warnings = append(rep.Warnings, redir.Warnings...)

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
	if v >= 10 && rep.Policy != "invalid" {
		if last, ok, err := db.LatestPolicy(ctx); err != nil {
			rep.Warnings = append(rep.Warnings, "policy: "+err.Error())
		} else if ok && last.SHA256 == pf.SHA256 && last.Document == pf.Document() {
			rep.PolicyVersion = last.Version
		}
	}
	// Nur Spalten, die jedes Schema hat: doctor migriert nicht und muss die
	// Konten auch bei needs_migration lesen können.
	list, err := db.AccountRefs(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "accounts: "+err.Error())
		return rep
	}
	incomplete, err := db.IncompleteAnswersByAccount(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "incomplete server answers: "+err.Error())
	}
	for _, acc := range list {
		da := DoctorAccount{Name: acc.Name, EnvVar: secrets.EnvName(acc.CredentialRef), Status: acc.Status, Password: "unknown"}
		if ia, ok := incomplete[acc.Name]; ok {
			at := ia.LastAt
			da.IncompleteAnswers, da.IncompleteAnswersLast, da.IncompleteAnswersAt = ia.Total, ia.Last, &at
			if time.Since(at) < 7*24*time.Hour {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s: %d incomplete server answers in the sync of %s (%d in total); nothing was removed, the next sync retries; if this repeats every day, the server answers inconsistently (see the log)",
					acc.Name, ia.Last, at.Format("2006-01-02 15:04 UTC"), ia.Total))
			}
		}
		if acc.AuthType == ops.AuthOAuth2 {
			doctorOAuthAccount(sec, acc, &da, &rep)
			rep.Accounts = append(rep.Accounts, da)
			continue
		}
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

// doctorOAuthAccount meldet den Token-Stand eines OAuth2-Kontos (S3.7):
// gespeichert oder nicht, Ablauf des letzten Access-Tokens, letzte
// Erneuerung. Liest nur; nie ein Token in der Ausgabe.
func doctorOAuthAccount(sec secrets.Store, acc store.AccountRef, da *DoctorAccount, rep *DoctorReport) {
	da.Auth, da.Password, da.EnvVar, da.Token = ops.AuthOAuth2, ops.AuthOAuth2, "", "unknown"
	if sec == nil {
		return
	}
	saved, err := oauth.Load(sec, acc.CredentialRef)
	switch {
	case err == nil:
		da.Token = "stored"
		if !saved.Expiry.IsZero() {
			da.TokenExpiry = saved.Expiry.UTC().Format(time.RFC3339)
		}
		if !saved.UpdatedAt.IsZero() {
			da.TokenUpdatedAt = saved.UpdatedAt.UTC().Format(time.RFC3339)
		}
	case errors.Is(err, secrets.ErrNotFound):
		da.Token = "missing"
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s has no OAuth2 token (keyring entry %s); sign in with: account login %s", acc.Name, acc.CredentialRef, acc.Name))
	case errors.Is(err, oauth.ErrTokenCorrupt):
		da.Token = "unreadable"
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s: the OAuth2 token in the keyring (%s) is unreadable; sign in again with: account login %s", acc.Name, acc.CredentialRef, acc.Name))
	}
}

// doctorPolicy lädt die Policy-Datei nur lesend (doctor trägt nichts ein).
func doctorPolicy(rep *DoctorReport, configFile string) policy.File {
	path, explicit := policy.FilePath(configFile)
	rep.PolicyPath = path
	f, err := policy.LoadFile(path, explicit)
	switch {
	case err != nil:
		rep.Policy, rep.PolicyError = "invalid", err.Error()
	case f.Exists:
		rep.Policy, rep.PolicySHA256 = "ok", f.SHA256
	default:
		rep.Policy = "default"
	}
	return f
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
	pw := map[string]string{}
	for _, da := range rep.Accounts {
		pw[da.Name] = da.Password
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
		case pw[acc.Name] == "missing":
			// Der Umzug lässt den Verweis stehen, solange unter dem alten
			// Namen nichts lesbar und keine Umgebungsvariable gesetzt ist.
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("account %s still uses the old keyring entry %s, which holds no readable password; it moves to %s once the entry can be read (e.g. keyring unlocked) or %s is set",
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
	}
	base := secrets.Stored(sec)
	reported := map[string]bool{}
	for _, ref := range left {
		if _, err := base.Get(ref); err == nil {
			reported[ref] = true
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("old keyring entry %s is still there after the move and could not be deleted; delete it in the OS keyring (service %s, entry %s)",
				ref, brand.KeyringService, ref))
		}
	}
	// Zusätzlich je umgezogenem Konto nur lesend: Besteht der alte Eintrag
	// noch, ohne dass er in meta vermerkt ist (z. B. Abbruch zwischen dem
	// Umstellen des Verweises und dem Löschen), warnt doctor ebenfalls.
	// Vorsicht beim Löschen: Ein anderes, noch nicht umgezogenes
	// Datenverzeichnis mit gleichem Kontonamen (Stand S1) liest denselben
	// Eintrag.
	if rep.Keyring == "unavailable" {
		return
	}
	for _, acc := range list {
		old := secrets.LegacyAccountRef(acc.Name)
		if acc.CredentialRef != secrets.AccountRef(dirID, acc.Name) || reported[old] {
			continue
		}
		if _, err := base.Get(old); err == nil {
			rep.Warnings = append(rep.Warnings, fmt.Sprintf("old keyring entry %s is still there, but account %s uses %s; unless another data directory of an older version still uses it, delete it in the OS keyring (service %s, entry %s)",
				old, acc.Name, acc.CredentialRef, brand.KeyringService, old))
		}
	}
}

// auditExportStale: Ein Tag mit Einträgen, der so lange zurückliegt und noch
// nicht exportiert ist, ergibt eine Warnung (der tägliche Export fehlt).
const auditExportStale = 48 * time.Hour

// doctorAudit meldet den Stand des Audit-Exports.
func doctorAudit(ctx context.Context, db *store.SQLite, rep *DoctorReport, now time.Time) {
	done, err := db.AuditExports(ctx)
	if err != nil {
		rep.Warnings = append(rep.Warnings, "audit: "+err.Error())
		return
	}
	// Dieselbe Zuordnung der Tage wie audit.export (verspätete Einträge,
	// T1a Punkt 4). Eine noch nicht migrierte Datenbank kennt nicht alle
	// Spalten: dann wie bisher nach Tagen ohne Nachweis.
	days, err := ops.AuditUnexportedDays(ctx, db)
	if err != nil {
		all, derr := db.AuditDays(ctx)
		if derr != nil {
			rep.Warnings = append(rep.Warnings, "audit: "+derr.Error())
			return
		}
		exported := map[string]bool{}
		for _, e := range done {
			exported[e.Day] = true
		}
		days = slices.DeleteFunc(all, func(d string) bool { return exported[d] })
	}
	var last store.AuditExport
	for _, e := range done {
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
	if r.ConfigPathReal != "" {
		fmt.Fprintf(w, "           really: %s (redirected by Windows)\n", r.ConfigPathReal)
	}
	if r.ConfigError != "" {
		fmt.Fprintf(w, "           error: %s\n", r.ConfigError)
	}
	fmt.Fprintf(w, "data dir:  %s\n", r.DataDir)
	if r.DataDirReal != "" {
		fmt.Fprintf(w, "           really: %s (redirected by Windows)\n", r.DataDirReal)
	}
	if r.AppPackage != "" {
		fmt.Fprintf(w, "package:   %s (runs inside a Windows app package)\n", r.AppPackage)
	}
	fmt.Fprintf(w, "log:       %s\n", r.LogPath)
	fmt.Fprintf(w, "database:  %s (%s)\n", r.Database, r.DatabasePath)
	fmt.Fprintf(w, "schema:    %d (program knows %d)\n", r.SchemaVersion, r.SchemaKnown)
	kr := r.Keyring
	if r.KeyringDetail != "" {
		kr += " (" + r.KeyringDetail + ")"
	}
	fmt.Fprintf(w, "keyring:   %s [secrets.backend = %s]\n", kr, r.SecretsBackend)
	switch r.Policy {
	case "ok", "default":
		state := "found"
		if r.Policy == "default" {
			state = "not found, built-in defaults"
		}
		version := "not recorded yet (the next command records it)"
		if r.PolicyVersion > 0 {
			version = fmt.Sprintf("version %d", r.PolicyVersion)
		}
		fmt.Fprintf(w, "policy:    %s (%s; %s)\n", r.PolicyPath, state, version)
	case "invalid":
		fmt.Fprintf(w, "policy:    %s (INVALID: %s)\n", r.PolicyPath, r.PolicyError)
	}
	if r.AttachDir != "" {
		fmt.Fprintf(w, "attach:    %s [paths.attach_dir]\n", r.AttachDir)
	}
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
			if a.Auth == ops.AuthOAuth2 {
				exp := ""
				if a.TokenExpiry != "" {
					exp = ", last access token valid until " + a.TokenExpiry + " (renewed automatically)"
				}
				if a.TokenUpdatedAt != "" {
					exp += ", last renewed " + a.TokenUpdatedAt
				}
				fmt.Fprintf(w, "  %s: OAuth2 token %s%s, status %s\n", a.Name, a.Token, exp, a.Status)
			} else {
				fmt.Fprintf(w, "  %s: password %s, status %s (env override: %s)\n", a.Name, a.Password, a.Status, a.EnvVar)
			}
			if a.IncompleteAnswersAt != nil {
				fmt.Fprintf(w, "    incomplete server answers: %d (last: %d on %s)\n", a.IncompleteAnswers, a.IncompleteAnswersLast,
					a.IncompleteAnswersAt.Format("2006-01-02 15:04 UTC"))
			}
		}
	}
	if len(r.Warnings) > 0 {
		fmt.Fprintln(w, "warnings:")
		for _, x := range r.Warnings {
			fmt.Fprintln(w, "  - "+strings.TrimSpace(x))
		}
	}
}
