package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// --- key create / list / revoke (S1.8) -----------------------------------------

func keyCreateFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("name", "", "name of the key (lowercase letters, digits, '-'); appears in the audit log")
	f.String("role", "assistant", "reader | assistant | triage | operator | admin")
	f.StringSlice("scopes", nil, "narrow the role to these scopes, e.g. mail:search (never widens)")
	f.String("expires", "90d", "lifetime in days, e.g. 30d; 'never' for no expiry")
	f.Bool("dry-run-default", true, "writing calls of this key run as dry run unless the agent sets dry_run=false")
	_ = cmd.MarkFlagRequired("name")
}

// parseExpiryDays liest --expires: "90d", "90" oder "never".
func parseExpiryDays(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "never" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
	if err != nil || n < 0 {
		return 0, usageError{errors.New("invalid --expires: use days like 90d, or never")}
	}
	if n == 0 {
		return 0, usageError{errors.New("--expires 0d would never expire; write --expires never if you mean that")}
	}
	return n, nil
}

func keyCreateInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	name, _ := f.GetString("name")
	role, _ := f.GetString("role")
	scopes, _ := f.GetStringSlice("scopes")
	exp, _ := f.GetString("expires")
	days, err := parseExpiryDays(exp)
	if err != nil {
		return nil, "", err
	}
	in := map[string]any{"name": name, "role": role, "expires_in_days": days}
	if len(scopes) > 0 {
		in["scopes"] = scopes
	}
	if f.Changed("dry-run-default") {
		v, _ := f.GetBool("dry-run-default")
		in["dry_run_default"] = v
	}
	b, err := json.Marshal(in)
	return b, "", err
}

func keyLine(k ops.KeyInfo) string {
	exp := "never expires"
	if t, err := time.Parse(time.RFC3339, k.ExpiresAt); err == nil {
		exp = "expires " + t.Local().Format("2006-01-02")
	}
	dry := "writing calls run as dry run by default"
	if !k.DryRunDefault {
		dry = "no dry-run default"
	}
	return fmt.Sprintf("role %s: %s; %s; %s", k.Role, strings.Join(k.Scopes, ", "), dry, exp)
}

func renderKeyCreate(w io.Writer, out any) error {
	o, ok := out.(ops.KeysCreateOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.DryRun {
		_, err := fmt.Fprintf(w, "dry run: would create key %s (%s)\n", o.Key.Name, keyLine(o.Key))
		return err
	}
	fmt.Fprintf(w, "key %s created (%s)\n", o.Key.Name, keyLine(o.Key))
	fmt.Fprintln(w, "This is the only time the key is shown. Store it now (e.g. in the MCP configuration as POSTKAUZ_KEY):")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s\n", o.Secret)
	fmt.Fprintln(w)
	_, err := fmt.Fprintf(w, "Only its hash is stored. Lost it? Revoke it (key revoke %s) and create a new one.\n", o.Key.Name)
	return err
}

func renderKeyList(w io.Writer, out any) error {
	o, ok := out.(ops.KeysListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Keys) == 0 {
		_, err := fmt.Fprintln(w, "No API keys yet; create one with: key create --name <name> --role reader")
		return err
	}
	day := func(s, none string) string {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Local().Format("2006-01-02")
		}
		return none
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tROLE\tSCOPES\tSTATUS\tDRY RUN\tCREATED\tEXPIRES\tLAST USED")
	for _, k := range o.Keys {
		dry := "default"
		if !k.DryRunDefault {
			dry = "off"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", k.Name, k.Role, strings.Join(k.Scopes, ","), k.Status, dry,
			day(k.CreatedAt, "-"), day(k.ExpiresAt, "never"), day(k.LastUsedAt, "never"))
	}
	return tw.Flush()
}

func renderKeyRevoke(w io.Writer, out any) error {
	o, ok := out.(ops.KeysRevokeOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	var err error
	switch {
	case o.AlreadyRevoked:
		_, err = fmt.Fprintf(w, "key %s was already revoked\n", o.Name)
	case o.DryRun:
		_, err = fmt.Fprintf(w, "dry run: would revoke key %s\n", o.Name)
	default:
		_, err = fmt.Fprintf(w, "key %s revoked; agents using it are refused from now on\n", o.Name)
	}
	return err
}

// --- audit (S1.8) ----------------------------------------------------------------

func auditFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.String("since", "", "from this time: duration back from now (10m, 24h, 7d), a date or RFC 3339 (default 24h)")
	f.String("until", "", "up to this time (exclusive), same forms as --since")
	f.String("actor", "", "only human | agent | system")
	f.String("key-id", "", "only this key as shown in the log, e.g. key:assistant-1 or cli")
	f.String("operation", "", "only this operation, e.g. messages.search")
	f.String("result", "", "only calls ending in ok | denied | needs_approval | error, or started (no end entry yet)")
	f.Bool("raw", false, "show every stored entry: start and end of a call as separate lines")
	f.Int("limit", ops.DefaultAuditLimit, fmt.Sprintf("entries per page (1-%d)", ops.MaxAuditLimit))
	f.Int64("after-id", 0, "continue after this entry id")
}

func auditInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	f := cmd.Flags()
	in := map[string]any{}
	for flag, field := range map[string]string{"since": "since", "until": "until", "actor": "actor", "key-id": "key",
		"operation": "operation", "result": "result"} {
		if v, _ := f.GetString(flag); v != "" {
			in[field] = v
		}
	}
	if f.Changed("limit") {
		n, _ := f.GetInt("limit")
		in["limit"] = n
	}
	if n, _ := f.GetInt64("after-id"); n > 0 {
		in["after_id"] = n
	}
	if raw, _ := f.GetBool("raw"); raw {
		in["raw"] = true
	}
	b, err := json.Marshal(in)
	return b, "", err
}

// auditJSON: NDJSON, eine Zeile je Eintrag.
func auditJSON(_ *cobra.Command, w io.Writer, out any) error {
	o, ok := out.(ops.AuditListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	enc := json.NewEncoder(w)
	for _, e := range o.Events {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return nil
}

func renderAudit(w io.Writer, out any) error {
	o, ok := out.(ops.AuditListOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Events) == 0 {
		_, err := fmt.Fprintln(w, "No audit entries in this period.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTIME\tACTOR\tKEY\tOPERATION\tRESULT\tTARGETS\tPARAMS")
	for _, e := range o.Events {
		ts := e.TS
		if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
			ts = t.Local().Format("2006-01-02 15:04:05")
		}
		res := e.Result
		if e.Reason != "" && e.Reason != "ok" {
			res += " (" + e.Reason + ")"
		}
		if e.DryRun {
			res += " dry-run"
		}
		if e.DurationMS > 0 {
			res += fmt.Sprintf(" %dms", e.DurationMS)
		}
		if e.RefID > 0 {
			res += fmt.Sprintf(" (end of #%d)", e.RefID)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.ID, ts, e.Actor, orDash(termLine(e.Key)), e.Operation, res,
			orDash(termLine(strings.Join(e.Targets, ","))), termLine(string(e.Params)))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if o.NextAfterID > 0 {
		_, err := fmt.Fprintf(w, "more: --after-id %d\n", o.NextAfterID)
		return err
	}
	return nil
}

func auditExportFlags(cmd *cobra.Command) {
	cmd.Flags().String("to", "", "export directory, ideally on another disk or computer (default: [audit] export_dir)")
}

func auditExportInput(cmd *cobra.Command, _ []string, _ *app) (json.RawMessage, secrets.Secret, error) {
	in := map[string]any{}
	if v, _ := cmd.Flags().GetString("to"); v != "" {
		in["to"] = v
	}
	b, err := json.Marshal(in)
	return b, "", err
}

func renderAuditExport(w io.Writer, out any) error {
	o, ok := out.(ops.AuditExportOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if len(o.Files) == 0 {
		_, err := fmt.Fprintln(w, "No audit entries to export yet.")
		return err
	}
	verb := map[string]string{"written": "written", "updated": "updated", "unchanged": "unchanged"}
	if o.DryRun {
		verb = map[string]string{"written": "would write", "updated": "would update", "unchanged": "unchanged"}
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DAY\tEVENTS\tSTATE\tFILE")
	for _, f := range o.Files {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", f.Day, f.Events, verb[f.State], termLine(f.File))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !o.DryRun {
		fmt.Fprintf(w, "manifest: %s\n", termLine(o.Manifest))
	}
	fmt.Fprintf(w, "chain head: %s\n", o.ChainHead)
	_, err := fmt.Fprintf(w, "%s in %s; check with: audit verify\n", plural(len(o.Files), "day", "days"), termLine(o.Location))
	return err
}

func renderAuditVerify(w io.Writer, out any) error {
	o, ok := out.(ops.AuditVerifyOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	if o.Status == "ok" {
		if o.DaysChecked == 0 {
			fmt.Fprintln(w, "ok: nothing exported yet (run: audit export --to <dir>)")
		} else if o.OriginDays == o.DaysChecked {
			fmt.Fprintf(w, "ok: %s checked; the database matches the recorded checksums\n", plural(o.DaysChecked, "exported day", "exported days"))
		} else {
			fmt.Fprintf(w, "ok: %s checked; the export files match their checksums, the hash chain, the manifest and the database\n", plural(o.DaysChecked, "exported day", "exported days"))
		}
	} else {
		fmt.Fprintf(w, "mismatch: %s in %s\n", plural(len(o.Problems), "problem", "problems"), plural(o.DaysChecked, "exported day", "exported days"))
		why := map[string]string{ops.AuditFileMissing: "export file is missing", ops.AuditFileChanged: "export file was changed",
			ops.AuditDatabaseChanged: "database rows differ from the export", ops.AuditChainBroken: "hash chain is broken",
			ops.AuditManifestMissing: "manifest is missing", ops.AuditManifestChanged: "manifest does not match the recorded exports"}
		for _, p := range o.Problems {
			msg := why[p.Problem]
			if p.Line > 0 {
				msg += fmt.Sprintf(" (line %d)", p.Line)
			}
			day := p.Day
			if day == "" {
				day = "all days"
			}
			fmt.Fprintf(w, "  %s: %s: %s\n", day, msg, termLine(p.File))
		}
	}
	if o.OriginDays > 0 {
		fmt.Fprintf(w, "%s exported by the data directory this one was restored from: only the database was checked against the recorded checksums; the files are with the original\n",
			plural(o.OriginDays, "day was", "days were"))
	}
	if o.LegacyDays > 0 {
		fmt.Fprintf(w, "%s without hash chain (exported before 0.1.0); the next audit export rewrites them\n", plural(o.LegacyDays, "day", "days"))
	}
	if o.Status == "ok" && o.ChainHead != "" {
		fmt.Fprintf(w, "chain head: %s\n", o.ChainHead)
	}
	if len(o.NotExported) > 0 {
		_, err := fmt.Fprintf(w, "not exported yet (new entries): %s\n", strings.Join(o.NotExported, ", "))
		return err
	}
	return nil
}

// errAuditMismatch: audit verify hat Abweichungen gefunden (Exit 1, Code mismatch).
var errAuditMismatch = errors.New("audit export does not match")

func checkAuditVerify(out any) error {
	if o, ok := out.(ops.AuditVerifyOutput); ok && o.Status != "ok" {
		return errAuditMismatch
	}
	return nil
}

// --- policy show ------------------------------------------------------------------

func renderPolicy(w io.Writer, out any) error {
	o, ok := out.(ops.PolicyOutput)
	if !ok {
		return fmt.Errorf("cli: unexpected output type %T", out)
	}
	exp := "never"
	if t, err := time.Parse(time.RFC3339, o.ExpiresAt); err == nil {
		exp = t.Local().Format("2006-01-02")
	}
	fmt.Fprintf(w, "key:             %s\nrole:            %s\nscopes:          %s\ndry-run default: %v\nexpires:         %s\n"+
		"max chars:       %d per text for agents\ndraft-only:      %v (nothing is ever sent)\noperations:\n",
		o.Key, orDash(o.Role), strings.Join(o.Scopes, ", "), o.DryRunDefault, exp, o.MaxChars, o.DraftOnly)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, op := range o.Operations {
		note := ""
		if op.NeedsApproval {
			note = "needs_approval"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", op.Name, op.Scope, op.Risk, note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(w, "mailbox actions: move %s, trash %s, confirm_count above %d ids, at most %d ids per call\n",
		o.Limits.MoveAgentMay, o.Limits.DeleteAgentMay, o.Limits.BulkConfirmThreshold, o.Limits.MaxIDs)
	// Entscheid 40: was der Schlüssel nie darf (nur der Mensch ohne --key).
	if len(o.HumanOnly) > 0 {
		fmt.Fprintln(w, "never with a key (human only):")
		for _, op := range o.HumanOnly {
			fmt.Fprintf(w, "  %s: human_only\n", op.Name)
		}
	}
	return nil
}

// plural: "1 day", "2 days".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
