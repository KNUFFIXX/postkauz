package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/secretscan"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DoctorSecrets ist der Teil von `doctor --secrets` (S1.11): Dateirechte
// und die Suche nach Geheimnissen in Log, Audit, Exporten, Konfiguration
// und den Tabellen ohne Mailinhalt. Werte erscheinen nie, nur wessen
// Geheimnis wo steht.
type DoctorSecrets struct {
	Findings []secretscan.Finding `json:"findings"`
	// Problems zählt Prüfpunkte mit Status fail (Exit 1).
	Problems int `json:"problems"`
}

// keyPattern erkennt den Klartext eines API-Schlüssels; das Programm
// speichert ihn nie, jeder Fund ist ein Leck.
var keyPattern = regexp.MustCompile(regexp.QuoteMeta(ops.KeyPrefix) + `[A-Za-z0-9_-]{43}`)

// errSecretCheck: doctor --secrets hat ein Problem gefunden (Exit 1).
var errSecretCheck = errors.New("secret check failed")

func doctorSecrets(ctx context.Context, a *app, rep DoctorReport) *DoctorSecrets {
	out := &DoctorSecrets{Findings: []secretscan.Finding{}}
	add := func(f secretscan.Finding) {
		if f.Status == secretscan.Fail {
			out.Problems++
		}
		out.Findings = append(out.Findings, f)
	}
	if rep.ConfigError != "" {
		add(secretscan.Finding{Check: "configuration", Status: secretscan.Skipped, Detail: "invalid configuration"})
		return out
	}

	// 1. Dateirechte (Unix; unter Windows gilt der Profilordner).
	p := a.paths
	add(secretscan.Perm("permissions data dir", p.DataDir, true))
	for _, f := range []struct{ check, path string }{{"permissions database", p.Database}, {"permissions database WAL", p.Database + "-wal"},
		{"permissions database SHM", p.Database + "-shm"}, {"permissions log", p.Log}} {
		add(secretscan.Perm(f.check, f.path, true))
	}
	if entries, err := os.ReadDir(p.SecretsDir); err == nil {
		add(secretscan.Perm("permissions insecure-file store", p.SecretsDir, true))
		for _, e := range entries {
			add(secretscan.Perm("permissions insecure-file store", filepath.Join(p.SecretsDir, e.Name()), true))
		}
	}
	if p.ConfigExists {
		add(secretscan.Perm("permissions configuration", p.ConfigFile, false))
	}
	if kf := os.Getenv(EnvKeyFile); kf != "" {
		add(secretscan.Perm("permissions "+EnvKeyFile, kf, true))
	}

	// 2. Gesuchte Werte: Passwörter der Konten, ein Schlüssel in POSTKAUZ_KEY.
	var needles []secretscan.Needle
	addNeedle := func(name string, v []byte) {
		if len(v) < secretscan.MinNeedle {
			add(secretscan.Finding{Check: "search for " + name, Status: secretscan.Warn,
				Detail: fmt.Sprintf("shorter than %d characters, too short to search for", secretscan.MinNeedle)})
			return
		}
		needles = append(needles, secretscan.Needle{Name: name, Value: v})
	}
	var db *store.SQLite
	if rep.Database == "ok" {
		var err error
		if db, err = store.Open(p.Database); err != nil {
			add(secretscan.Finding{Check: "database", Status: secretscan.Warn, Detail: err.Error()})
			db = nil
		} else {
			defer db.Close()
		}
	}
	sec, secErr := openSecrets(a.cfg.Secrets.Backend, p.SecretsDir)
	if db != nil {
		accounts, err := db.ListAccounts(ctx)
		if err != nil {
			add(secretscan.Finding{Check: "accounts", Status: secretscan.Warn, Detail: err.Error()})
		}
		for _, acc := range accounts {
			if acc.AuthType == ops.AuthOAuth2 {
				// Refresh-Token (S3.7): Es darf so wenig wie ein Passwort in
				// Log, Konfiguration oder Datenbank stehen.
				name := "OAuth2 refresh token of account " + acc.Name
				if secErr != nil {
					add(secretscan.Finding{Check: "search for " + name, Status: secretscan.Skipped, Detail: "keyring unavailable"})
					continue
				}
				if saved, err := oauth.Load(sec, acc.CredentialRef); err == nil {
					addNeedle(name, []byte(saved.RefreshToken.Reveal()))
				}
				continue
			}
			name := "password of account " + acc.Name
			if v, ok := os.LookupEnv(secrets.EnvName(acc.CredentialRef)); ok && v != "" {
				addNeedle(name, []byte(v))
				continue
			}
			if secErr != nil {
				add(secretscan.Finding{Check: "search for " + name, Status: secretscan.Skipped, Detail: "keyring unavailable"})
				continue
			}
			pw, err := sec.Get(acc.CredentialRef)
			switch {
			case errors.Is(err, secrets.ErrNotFound):
				// kein Passwort hinterlegt: nichts zu suchen
			case err != nil:
				add(secretscan.Finding{Check: "search for " + name, Status: secretscan.Skipped, Detail: "keyring: " + err.Error()})
			default:
				addNeedle(name, []byte(pw.Reveal()))
			}
		}
	}
	if v := os.Getenv(EnvKey); v != "" {
		addNeedle("API key in "+EnvKey, []byte(v))
	}
	patterns := map[string]*regexp.Regexp{"an API key (" + ops.KeyPrefix + "…)": keyPattern}

	report := func(where string, hits []secretscan.Hit, err error) {
		check := "secrets in " + where
		switch {
		case err != nil:
			add(secretscan.Finding{Check: check, Status: secretscan.Warn, Detail: err.Error()})
		case len(hits) == 0:
			add(secretscan.Finding{Check: check, Status: secretscan.OK})
		default:
			var parts []string
			for _, h := range hits {
				parts = append(parts, fmt.Sprintf("%s found (line %d)", h.Name, h.Line))
			}
			add(secretscan.Finding{Check: check, Status: secretscan.Fail, Detail: strings.Join(parts, "; ")})
		}
	}

	// 3. Wo Geheimnisse nie stehen dürfen.
	hits, err := secretscan.ScanFile(p.Log, needles, patterns)
	report("log "+p.Log, hits, err)
	if p.ConfigExists {
		hits, err := secretscan.ScanFile(p.ConfigFile, needles, patterns)
		report("configuration "+p.ConfigFile, hits, err)
	}
	if db == nil {
		add(secretscan.Finding{Check: "secrets in database", Status: secretscan.Skipped, Detail: "database " + rep.Database})
		return out
	}
	for _, t := range store.ScanTables {
		b, err := db.TableText(ctx, t)
		var hits []secretscan.Hit
		if err == nil {
			hits, err = secretscan.Scan(strings.NewReader(string(b)), needles, patterns)
		}
		report("database table "+t, hits, err)
	}
	// Audit-Exporte am zweiten Ort (alle Orte, die der Nachweis kennt).
	exports, err := db.AuditExports(ctx)
	if err != nil {
		add(secretscan.Finding{Check: "secrets in audit exports", Status: secretscan.Warn, Detail: err.Error()})
	}
	locs := map[string]bool{}
	for _, e := range exports {
		locs[e.Location] = true
	}
	for loc := range locs {
		files, _ := filepath.Glob(filepath.Join(loc, "*-audit-*.jsonl"))
		files = append(files, filepath.Join(loc, "*-audit-manifest.json"))
		var all []secretscan.Hit
		var serr error
		for _, f := range files {
			matches, _ := filepath.Glob(f)
			for _, m := range matches {
				hits, err := secretscan.ScanFile(m, needles, patterns)
				if err != nil {
					serr = err
				}
				for _, h := range hits {
					h.Name += " in " + filepath.Base(m)
					all = append(all, h)
				}
			}
		}
		report("audit export "+loc, all, serr)
	}
	return out
}

func printDoctorSecrets(w io.Writer, s *DoctorSecrets) {
	fmt.Fprintln(w, "secrets:")
	for _, f := range s.Findings {
		line := fmt.Sprintf("  %-7s %s", f.Status, f.Check)
		if f.Detail != "" {
			line += ": " + f.Detail
		}
		fmt.Fprintln(w, termLine(line))
	}
	if s.Problems == 0 {
		fmt.Fprintln(w, "  result: ok (no secret found where it must not be, permissions fine)")
	} else {
		fmt.Fprintf(w, "  result: %d problems\n", s.Problems)
	}
}
