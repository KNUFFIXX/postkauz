package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// --- backup create (S3.4, E7) ------------------------------------------------

func backupCreateFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("to", "", "backup folder: new, empty or holding an earlier backup (then only missing blobs are copied); never inside the data directory")
	f.String("tar", "", `"-": write a tar stream to standard output instead (pipe it to age or restic)`)
}

func backupCreateInput(cmd *cobra.Command, _ []string, a *app) (json.RawMessage, secrets.Secret, error) {
	to, _ := cmd.Flags().GetString("to")
	tar, _ := cmd.Flags().GetString("tar")
	switch {
	case cmd.Flags().Changed("tar") && tar != "-":
		return nil, "", usageError{errors.New(`--tar takes only "-" (standard output); for a folder use --to <folder>`)}
	case (to == "") == (tar == ""):
		return nil, "", usageError{errors.New("give either --to <folder> or --tar - (tar stream on standard output)")}
	case tar != "":
		if f, ok := a.stdout.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			return nil, "", usageError{errors.New("refusing to write a tar stream to the terminal; redirect it, e.g. backup create --tar - | age -r <recipient> > backup.tar.age")}
		}
	}
	in := map[string]string{"to": to}
	if tar != "" {
		in = map[string]string{"tar": tar}
	}
	b, err := json.Marshal(in)
	return b, "", err
}

// backupToStdout: Mit --tar - gehört stdout dem Tar-Strom.
func backupToStdout(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetString("tar")
	return v == "-"
}

func backupWarnings(out any) []string {
	o, ok := out.(backup.Result)
	if !ok {
		return nil
	}
	var w []string
	for _, s := range o.AuditArchivesSkipped {
		w = append(w, "audit archive "+s+" is not in this backup: it is missing or no longer matches its recorded checksum; see audit verify")
	}
	if !o.DryRun {
		w = append(w, o.Warning)
	}
	return w
}

func renderBackupCreate(w io.Writer, out any) error {
	o, ok := out.(backup.Result)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		where := termLine(o.Target)
		if o.Mode == "tar" {
			where = "a tar stream on standard output"
		}
		_, err := fmt.Fprintf(w, "dry run: would write %s (%s) to %s: %d of %s to copy (%s), %d already there; nothing was written\n",
			plural(o.Files, "file", "files"), humanBytes(o.Bytes), where, o.BlobsCopied, plural(o.Blobs, "blob", "blobs"),
			humanBytes(o.BytesCopied), o.BlobsExisting)
		return err
	}
	if o.Mode == "tar" {
		fmt.Fprintln(w, "backup written as a tar stream to standard output")
	} else {
		fmt.Fprintf(w, "backup written to %s\n", termLine(o.Target))
	}
	fmt.Fprintf(w, "  database:  %s (schema %d)\n", o.Database, o.SchemaVersion)
	fmt.Fprintf(w, "  blobs:     %d (copied %d, already there %d; %s copied)\n", o.Blobs, o.BlobsCopied, o.BlobsExisting, humanBytes(o.BytesCopied))
	if o.AuditArchives > 0 {
		fmt.Fprintf(w, "  archives:  %s\n", plural(o.AuditArchives, "audit archive", "audit archives"))
	}
	fmt.Fprintf(w, "  manifest:  %s, %s (%s), sha256 %s\n", termLine(o.Manifest), plural(o.Files, "file", "files"), humanBytes(o.Bytes), o.ManifestSHA256)
	if o.SnapshotsRemoved > 0 {
		fmt.Fprintf(w, "  replaced:  %s of the earlier backup\n", plural(o.SnapshotsRemoved, "database snapshot", "database snapshots"))
	}
	_, err := fmt.Fprintf(w, "  restore:   restore --from <backup folder> --data-dir <new folder>\n")
	return err
}

// --- restore (S3.4, E7, A10) ---------------------------------------------------

// RestoreReport ist die Ausgabe von `restore`: das Ergebnis der
// Wiederherstellung und danach doctor mit --blobs=all für das neue
// Datenverzeichnis (fehlende Zugangsdaten stehen dort als Warnung).
type RestoreReport struct {
	Restore backup.RestoreResult `json:"restore"`
	// Doctor fehlt beim Trockenlauf (es gibt kein Datenverzeichnis).
	Doctor *DoctorReport `json:"doctor,omitempty"`
}

// restoreCmd ist wie doctor ein Kommando vor dem Öffnen einer Datenbank:
// Es legt das neue Datenverzeichnis selbst an (backup.Restore) und
// migriert nur dessen Datenbank.
func restoreCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore --from <backup folder> --data-dir <new folder> [--dry-run]",
		Short: "Restore a backup into a new, empty data directory: checks every file against the manifest first, gives the copy a new identity, then verifies all blobs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			from, _ := cmd.Flags().GetString("from")
			if strings.TrimSpace(from) == "" {
				return usageError{errors.New("restore needs --from <backup folder> (a tar stream: unpack it into a folder first)")}
			}
			if !cmd.Flags().Changed("data-dir") {
				return usageError{errors.New("restore needs --data-dir <new folder>; it never writes into the data directory from the configuration or the environment")}
			}
			if a.cfgErr != nil {
				return a.cfgErr
			}
			keep, _ := cmd.Flags().GetBool("keep-identity")
			dry, _ := cmd.Flags().GetBool("dry-run")
			res, err := backup.Restore(cmd.Context(), backup.RestoreOptions{From: from, DataDir: a.paths.DataDir,
				Refuse: protectedDataDirs(cmd), KeepIdentity: keep, DryRun: dry, DBName: filepath.Base(a.paths.Database)})
			if err != nil {
				return err
			}
			if dry {
				// Nur geprüft: kein Datenverzeichnis, also kein doctor.
				if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
					return json.NewEncoder(a.stdout).Encode(RestoreReport{Restore: res})
				}
				_, err := fmt.Fprintf(a.stdout, "dry run: the backup of %s is complete and unchanged (%s, %s, manifest sha256 %s); %s would be created; nothing was written\n",
					res.BackupCreatedAt, plural(res.Files, "file", "files"), humanBytes(res.Bytes), res.ManifestSHA256, termLine(res.DataDir))
				printLeftovers(a.stdout, res.Leftovers)
				return err
			}
			d := doctor(cmd.Context(), a)
			d.Blobs = doctorBlobs(cmd.Context(), a, d, "all")
			rep := RestoreReport{Restore: res, Doctor: &d}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if err := json.NewEncoder(a.stdout).Encode(rep); err != nil {
					return err
				}
			} else {
				printRestore(a.stdout, res)
				fmt.Fprintln(a.stdout)
				printDoctor(a.stdout, d)
				printDoctorBlobs(a.stdout, d.Blobs)
			}
			if d.Blobs.Problems > 0 {
				return fmt.Errorf("%w: %d problems in the restored data directory (see the section blobs)", errBlobCheck, d.Blobs.Problems)
			}
			return nil
		},
	}
	cmd.Flags().String("from", "", "backup folder with manifest.json (made by backup create --to, or a tar stream unpacked into a folder)")
	cmd.Flags().Bool("keep-identity", false, "keep the data directory id of the original, so this copy uses the same keyring entries "+
		"(only to replace a lost data directory; default: a new id)")
	return cmd
}

// protectedDataDirs sind die Datenverzeichnisse, in die restore nie
// schreibt: das Standard-Datenverzeichnis und das aus Konfiguration und
// Umgebung (ohne --data-dir), jeweils mit --profile.
func protectedDataDirs(cmd *cobra.Command) []string {
	var out []string
	if d, err := config.DefaultDataDir(); err == nil {
		out = append(out, d)
	}
	var opts config.LoadOptions
	opts.Profile, _ = cmd.Flags().GetString("profile")
	opts.ConfigFile, _ = cmd.Flags().GetString("config")
	if _, p, err := config.LoadWith(opts); err == nil {
		out = append(out, p.DataDir)
	}
	return out
}

func printRestore(w io.Writer, r backup.RestoreResult) {
	fmt.Fprintf(w, "restored:  backup of %s (program %s, schema %d) from %s\n", r.BackupCreatedAt, termLine(r.BackupProgramVersion),
		r.BackupSchemaVersion, termLine(r.Source))
	fmt.Fprintf(w, "into:      %s (schema %d)\n", termLine(r.DataDir), r.SchemaVersion)
	fmt.Fprintf(w, "manifest:  sha256 %s; %s (%s) checked: database, %s, %s\n", r.ManifestSHA256, plural(r.Files, "file", "files"),
		humanBytes(r.Bytes), plural(r.Blobs, "blob", "blobs"), plural(r.AuditArchives, "audit archive", "audit archives"))
	if r.MigrationSnapshot != "" {
		fmt.Fprintf(w, "migrated:  copy before the migration: %s\n", termLine(r.MigrationSnapshot))
	}
	fx := r.Fixes
	if fx.NewIdentity {
		fmt.Fprintln(w, "identity:  new data directory id; the keyring entries of the original are not used here")
		fmt.Fprintln(w, "           store the passwords again: account set <name> --password-stdin (OAuth2: account login <name>)")
	} else {
		fmt.Fprintln(w, "identity:  kept (--keep-identity): this copy uses the keyring entries of the original")
	}
	// Sichtprüfung (T3-Nachbesserung): wohin die Konten sich verbinden und
	// welche Dateien als Stildatei gelesen werden; Werte aus der Sicherung.
	if len(r.Accounts) > 0 {
		fmt.Fprintln(w, "accounts:  check each server and user before you store a password (only restore your own backups):")
		for _, a := range r.Accounts {
			fmt.Fprintf(w, "           %s: %s:%d (%s), user %s, %s\n", termLine(a.Name), termLine(a.Host), a.Port, termLine(a.TLS),
				termLine(a.Username), termLine(a.AuthType))
		}
	}
	if len(r.StyleFiles) > 0 {
		fmt.Fprintln(w, "style:     files this copy reads as style guide (mail://style); check that they are yours:")
		for _, s := range r.StyleFiles {
			fmt.Fprintf(w, "           %s (%s): %s\n", termLine(s.Identity), termLine(s.Account), termLine(s.Path))
		}
	}
	fmt.Fprintf(w, "stopped:   %s, %s, %s, %s held back (draft show <id>), %s cleared\n",
		plural(fx.Jobs, "open job", "open jobs"), plural(fx.Actions, "mailbox action", "mailbox actions"),
		plural(fx.Requests, "request rejected", "requests rejected"), plural(fx.DraftsHeld, "draft", "drafts"),
		plural(fx.PushAttempts, "push attempt", "push attempts"))
	fmt.Fprintf(w, "audit:     %s closed, %s cleared (they belong to the original), event %s written\n",
		plural(fx.AuditStarts, "unfinished call", "unfinished calls"), plural(fx.AuditExports, "export location", "export locations"), backup.OpRestore)
	fmt.Fprintln(w, "           nothing in this copy writes to a mail server on its own")
}

// printLeftovers nennt Arbeitsordner abgebrochener Wiederherstellungen
// (T3-Nachbesserung 4) beim Trockenlauf; nach einer Wiederherstellung
// nennt sie der Bericht von doctor als Warnung.
func printLeftovers(w io.Writer, left []string) {
	for _, l := range left {
		fmt.Fprintf(w, "leftover:  %s is left from an interrupted restore; it is not a usable data directory, delete it\n", termLine(l))
	}
}
