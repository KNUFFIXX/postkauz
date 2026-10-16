package ops

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Audit-Export mit Prüfsumme an einen zweiten Ort und Prüfung (S1.8, Kette
// und Manifest seit S1.11; docs/sicherheit.md, Abschnitt 6).
//
// Format 2 (seit S1.11): je UTC-Tag eine JSONL-Datei, je Eintrag eine Zeile
//
//	{"event":{…},"prev":"<hex>","chain":"<hex>"}
//
// mit chain = SHA-256(prev + "\n" + event), wobei event die Bytes des
// Eintrags genau so sind, wie sie in der Zeile stehen. Die Kette läuft über
// alle Tage (erste Zeile eines Tages: prev = letzte chain des Vortags; ganz
// am Anfang der feste Startwert auditChainGenesis). Daneben liegt das
// Manifest (auditManifestName) mit Prüfsumme und Kettenende je Datei. Damit
// lässt sich der Export auch ohne Datenbank prüfen (jede Zeile, Reihenfolge,
// fehlende Tage), und `audit verify` nennt die erste veränderte Zeile.
//
// Format 1 (S1.8 bis S1.10): eine Zeile je Eintrag, nur der Eintrag. Solche
// Exporte prüft verify weiter (Prüfsumme gegen Datenbank); der nächste
// Export schreibt sie in Format 2 neu.

// AuditExportFormat ist das Format, das audit.export schreibt.
const AuditExportFormat = 2

// auditChainSeed ergibt den Startwert der Kette. Bewusst unabhängig vom
// Programmnamen (internal/brand), damit eine Umbenennung keine Kette bricht.
const auditChainSeed = "audit-chain-v1"

// auditChainGenesis ist der Startwert der Kette (hex).
var auditChainGenesis = sha256Hex([]byte(auditChainSeed))

// auditFileName ist der Name der Exportdatei eines Tages.
func auditFileName(day string) string { return brand.Name + "-audit-" + day + ".jsonl" }

// auditManifestName ist der Name des Manifests im Exportordner.
func auditManifestName() string { return brand.Name + "-audit-manifest.json" }

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func fileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

// chainNext ist ein Schritt der Kette.
func chainNext(prev string, event []byte) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write([]byte{'\n'})
	h.Write(event)
	return hex.EncodeToString(h.Sum(nil))
}

// auditChainLine ist eine Zeile im Format 2. Event bleibt RawMessage, damit
// die Prüfung genau die Bytes der Datei hasht.
type auditChainLine struct {
	Event json.RawMessage `json:"event"`
	Prev  string          `json:"prev"`
	Chain string          `json:"chain"`
}

// auditDay ist der Inhalt eines Tages, wie ihn die Datenbank ergibt.
type auditDay struct {
	content     []byte
	first, last int64
	events      int
	head        string // letzte chain (Format 2), sonst ""
}

// auditDayFromDB erzeugt den Inhalt der Exportdatei eines Tages aus der
// Datenbank: aufsteigend nach ID, höchstens bis lastID (0 = alle). Format 2
// braucht den Kettenwert davor (prev). Dieselbe Funktion dient Export und
// Prüfung.
func auditDayFromDB(ctx context.Context, repo AuditRepo, day string, lastID int64, format int, prev string) (auditDay, error) {
	start, err := time.Parse("2006-01-02", day)
	if err != nil {
		return auditDay{}, err
	}
	list, err := repo.AuditQuery(ctx, store.AuditFilter{Since: start, Until: start.Add(24 * time.Hour)})
	if err != nil {
		return auditDay{}, err
	}
	var buf bytes.Buffer
	d := auditDay{head: prev}
	for _, r := range list {
		if lastID > 0 && r.ID > lastID {
			break
		}
		ev, err := json.Marshal(auditEventOut(r))
		if err != nil {
			return auditDay{}, err
		}
		if format == 1 {
			buf.Write(ev)
		} else {
			next := chainNext(d.head, ev)
			line, err := json.Marshal(auditChainLine{Event: ev, Prev: d.head, Chain: next})
			if err != nil {
				return auditDay{}, err
			}
			buf.Write(line)
			d.head = next
		}
		buf.WriteByte('\n')
		if d.first == 0 {
			d.first = r.ID
		}
		d.last = r.ID
		d.events++
	}
	d.content = buf.Bytes()
	if format == 1 {
		d.head = ""
	}
	return d, nil
}

// checkChain prüft die Kette einer Datei im Format 2 ab prev. Ergebnis: das
// Kettenende und die erste Zeile (ab 1), an der die Kette bricht (0 = keine).
func checkChain(content []byte, prev string) (head string, badLine int) {
	sc := bufio.NewScanner(bytes.NewReader(content))
	sc.Buffer(make([]byte, 64*1024), 16<<20)
	head, n := prev, 0
	for sc.Scan() {
		n++
		var l auditChainLine
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil || len(l.Event) == 0 || l.Prev != head || chainNext(head, l.Event) != l.Chain {
			return head, n
		}
		head = l.Chain
	}
	if sc.Err() != nil {
		return head, n + 1
	}
	return head, 0
}

// looksLikeChain: Die erste Zeile hat die Felder des Formats 2.
func looksLikeChain(content []byte) bool {
	first, _, _ := bytes.Cut(content, []byte{'\n'})
	var l auditChainLine
	return json.Unmarshal(first, &l) == nil && len(l.Event) > 0 && l.Chain != ""
}

// firstDiffLine nennt die erste Zeile (ab 1), in der sich a und b
// unterscheiden (0 = gleich).
func firstDiffLine(a, b []byte) int {
	la, lb := bytes.Split(a, []byte{'\n'}), bytes.Split(b, []byte{'\n'})
	for i := 0; i < len(la) || i < len(lb); i++ {
		if i >= len(la) || i >= len(lb) || !bytes.Equal(la[i], lb[i]) {
			return i + 1
		}
	}
	return 0
}

// auditManifest liegt neben den Exportdateien. Es enthält keine Zeitangabe:
// Aus den Nachweisen in der Datenbank ergibt es sich Byte für Byte, so kann
// verify es vergleichen.
type auditManifest struct {
	Format  int                `json:"format"`
	Program string             `json:"program"`
	Chain   string             `json:"chain"`
	Genesis string             `json:"genesis"`
	Head    string             `json:"head"`
	Days    []auditManifestDay `json:"days"`
}

type auditManifestDay struct {
	Day          string `json:"day"`
	File         string `json:"file"`
	Events       int    `json:"events"`
	FirstEventID int64  `json:"first_event_id"`
	LastEventID  int64  `json:"last_event_id"`
	SHA256       string `json:"sha256"`
	ChainHead    string `json:"chain_head"`
}

const auditChainRule = `each line {"event":E,"prev":P,"chain":C} with C = hex(SHA-256(P + "\n" + E)); P of the first line = chain of the last line of the previous day, at the very start the genesis value = hex(SHA-256("` + auditChainSeed + `"))`

func buildManifest(days []auditManifestDay) []byte {
	m := auditManifest{Format: AuditExportFormat, Program: brand.Name, Chain: auditChainRule, Genesis: auditChainGenesis,
		Head: auditChainGenesis, Days: days}
	if m.Days == nil {
		m.Days = []auditManifestDay{}
	}
	if len(days) > 0 {
		m.Head = days[len(days)-1].ChainHead
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}

// AuditExportFile beschreibt eine geschriebene (oder geprüfte) Exportdatei.
type AuditExportFile struct {
	Day          string `json:"day"`
	File         string `json:"file"`
	Events       int    `json:"events"`
	FirstEventID int64  `json:"first_event_id"`
	LastEventID  int64  `json:"last_event_id"`
	SHA256       string `json:"sha256"`
	ChainHead    string `json:"chain_head"`
	State        string `json:"state"` // written | updated | unchanged
}

// AuditExportOutput ist die Ausgabe von audit.export.
type AuditExportOutput struct {
	Location string            `json:"location"`
	Format   int               `json:"format"`
	Manifest string            `json:"manifest"`
	Files    []AuditExportFile `json:"files"`
	// ChainHead ist das Ende der Kette über alle Tage (wie im Manifest);
	// wer es sich notiert, kann später jeden Export dagegen prüfen.
	ChainHead string `json:"chain_head"`
	DryRun    bool   `json:"dry_run"`
}

// ErrAuditExportChanged: Eine früher exportierte Datei passt nicht mehr zu
// ihrer Prüfsumme; der Export überschreibt sie nicht (sonst verschwände der
// Beleg einer Manipulation).
var ErrAuditExportChanged = errors.New("audit export file does not match its recorded checksum")

// AuditExport schreibt das Audit tageweise als JSONL mit Hash-Kette an einen
// zweiten Ort, dazu das Manifest, und hält Prüfsumme und Ort in
// audit_exports fest (R1, admin). Ein noch laufender Tag wird bei jedem
// Export neu geschrieben. Gedacht als tägliche Aufgabe (deploy/windows,
// deploy/systemd).
func AuditExport(repo AuditRepo, defaultDir string) Operation {
	return Operation{
		Name:    "audit.export",
		Scope:   policy.ScopeAdmin,
		Risk:    policy.R1,
		Summary: "Export the audit log day by day as JSONL files with a SHA-256 hash chain and a manifest to a second location.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "to": {"type": "string", "description": "export directory (default: [audit] export_dir in the configuration)"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "location": {"type": "string"},
    "format": {"type": "integer"},
    "manifest": {"type": "string"},
    "files": {"type": "array", "items": {"type": "object", "properties": {
      "day": {"type": "string"}, "file": {"type": "string"}, "events": {"type": "integer"},
      "first_event_id": {"type": "integer"}, "last_event_id": {"type": "integer"}, "sha256": {"type": "string"},
      "chain_head": {"type": "string"},
      "state": {"type": "string", "enum": ["written", "updated", "unchanged"]}},
      "required": ["day", "file", "events", "first_event_id", "last_event_id", "sha256", "chain_head", "state"]}},
    "chain_head": {"type": "string"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["location", "format", "manifest", "files", "chain_head", "dry_run"]
}`),
		// Der Pfad kann den Nutzernamen enthalten: ins Audit nur Feldnamen.
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			params, _ := redactAccountInput(raw)
			return params, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				To string `json:"to,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			dir := in.To
			if dir == "" {
				dir = defaultDir
			}
			if dir == "" {
				return nil, inputErr("no export directory: give one (audit export --to <dir>) or set [audit] export_dir in the configuration")
			}
			dir, err := filepath.Abs(dir)
			if err != nil {
				return nil, err
			}
			days, err := repo.AuditDays(ctx)
			if err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			prev := map[string]store.AuditExport{}
			for _, e := range done {
				prev[e.Day] = e
			}
			out := AuditExportOutput{Location: dir, Format: AuditExportFormat, Manifest: filepath.Join(dir, auditManifestName()),
				Files: []AuditExportFile{}, ChainHead: auditChainGenesis, DryRun: call.DryRun}
			if !call.DryRun {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					return nil, err
				}
			}
			// Erst alles prüfen, dann schreiben: Eine veränderte frühere
			// Datei bricht den Export ab, bevor irgendetwas ersetzt wird.
			type pending struct {
				f       AuditExportFile
				content []byte
			}
			var todo []pending
			var manifest []auditManifestDay
			head := auditChainGenesis
			for _, day := range days {
				d, err := auditDayFromDB(ctx, repo, day, 0, AuditExportFormat, head)
				if err != nil {
					return nil, err
				}
				head = d.head
				f := AuditExportFile{Day: day, File: filepath.Join(dir, auditFileName(day)), Events: d.events, FirstEventID: d.first,
					LastEventID: d.last, SHA256: sha256Hex(d.content), ChainHead: d.head, State: "written"}
				if p, ok := prev[day]; ok {
					// Die frühere Datei muss unverändert sein, bevor sie ersetzt wird.
					old := filepath.Join(p.Location, auditFileName(day))
					got, err := fileSHA256(old)
					switch {
					case err == nil && got != p.SHA256:
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditExportChanged, old)
					case err == nil && got == f.SHA256 && p.Location == dir:
						f.State = "unchanged"
					case err != nil && !errors.Is(err, fs.ErrNotExist):
						return nil, err
					default:
						f.State = "updated"
					}
				}
				todo = append(todo, pending{f, d.content})
				manifest = append(manifest, auditManifestDay{Day: day, File: auditFileName(day), Events: d.events,
					FirstEventID: d.first, LastEventID: d.last, SHA256: f.SHA256, ChainHead: d.head})
			}
			out.ChainHead = head
			for _, p := range todo {
				out.Files = append(out.Files, p.f)
				if call.DryRun || p.f.State == "unchanged" {
					continue
				}
				if err := writeFileAtomic(p.f.File, p.content); err != nil {
					return nil, err
				}
				if err := repo.SaveAuditExport(ctx, store.AuditExport{Day: p.f.Day, FirstEventID: p.f.FirstEventID, LastEventID: p.f.LastEventID,
					SHA256: p.f.SHA256, Location: dir, ExportedAt: time.Now()}); err != nil {
					return nil, err
				}
			}
			if !call.DryRun {
				if err := writeFileAtomic(out.Manifest, buildManifest(manifest)); err != nil {
					return nil, err
				}
			}
			return out, nil
		},
	}
}

// writeFileAtomic schreibt über eine Temporärdatei im selben Verzeichnis.
func writeFileAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".export-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Befunde von audit.verify.
const (
	AuditFileMissing     = "file_missing"     // Exportdatei fehlt
	AuditFileChanged     = "file_changed"     // Datei passt nicht zu ihrer Prüfsumme
	AuditDatabaseChanged = "database_changed" // Datenbankzeilen passen nicht zum Export
	AuditChainBroken     = "chain_broken"     // Hash-Kette in der Datei gebrochen
	AuditManifestMissing = "manifest_missing" // Manifest fehlt
	AuditManifestChanged = "manifest_changed" // Manifest passt nicht zu den Nachweisen
)

// AuditProblem ist ein Befund von audit.verify.
type AuditProblem struct {
	Day     string `json:"day"`
	File    string `json:"file"`
	Problem string `json:"problem"`
	// Line ist die erste betroffene Zeile der Datei (ab 1), wenn bekannt.
	Line int `json:"line,omitempty"`
}

// AuditVerifyOutput ist die Ausgabe von audit.verify.
type AuditVerifyOutput struct {
	Status      string         `json:"status"` // ok | mismatch
	DaysChecked int            `json:"days_checked"`
	Problems    []AuditProblem `json:"problems"`
	// NotExported nennt Tage mit Einträgen, die noch nicht (vollständig)
	// exportiert sind; das ist kein Fehler, nur ein Hinweis.
	NotExported []string `json:"not_exported"`
	// ChainHead ist das Kettenende des letzten geprüften Tages (Format 2).
	ChainHead string `json:"chain_head,omitempty"`
	// LegacyDays sind Tage im Format 1 (ohne Kette); der nächste Export
	// schreibt sie neu.
	LegacyDays int `json:"legacy_days,omitempty"`
}

// AuditVerify vergleicht die exportierten Dateien mit ihren Prüfsummen, mit
// den Zeilen der Datenbank, mit der Hash-Kette und mit dem Manifest (R0,
// audit:read).
func AuditVerify(repo AuditRepo) Operation {
	return Operation{
		Name:       "audit.verify",
		Scope:      policy.ScopeAuditRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Check exported audit files against their checksums, the hash chain, the manifest and the database.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "status": {"type": "string", "enum": ["ok", "mismatch"]},
    "days_checked": {"type": "integer"},
    "problems": {"type": "array", "items": {"type": "object", "properties": {
      "day": {"type": "string"}, "file": {"type": "string"},
      "problem": {"type": "string", "enum": ["file_missing", "file_changed", "database_changed", "chain_broken", "manifest_missing", "manifest_changed"]},
      "line": {"type": "integer"}},
      "required": ["day", "file", "problem"]}},
    "not_exported": {"type": "array", "items": {"type": "string"}},
    "chain_head": {"type": "string"},
    "legacy_days": {"type": "integer"}
  },
  "required": ["status", "days_checked", "problems", "not_exported"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			out := AuditVerifyOutput{Status: "ok", Problems: []AuditProblem{}, NotExported: []string{}}
			add := func(day, file, problem string, line int) {
				out.Problems = append(out.Problems, AuditProblem{Day: day, File: file, Problem: problem, Line: line})
			}
			exported := map[string]store.AuditExport{}
			// Manifest je Ort; nur geprüft, wenn dort ein Tag im Format 2 liegt.
			manifests := map[string][]auditManifestDay{}
			chained := map[string]bool{}
			var locations []string
			head := auditChainGenesis
			for _, e := range done {
				exported[e.Day] = e
				out.DaysChecked++
				if _, ok := manifests[e.Location]; !ok {
					locations = append(locations, e.Location)
					manifests[e.Location] = nil
				}
				file := filepath.Join(e.Location, auditFileName(e.Day))
				fileBytes, ferr := os.ReadFile(file)
				if ferr != nil && !errors.Is(ferr, fs.ErrNotExist) {
					return nil, ferr
				}
				fileOK := ferr == nil && sha256Hex(fileBytes) == e.SHA256

				// Welches Format ergibt die Prüfsumme des Nachweises?
				d2, err := auditDayFromDB(ctx, repo, e.Day, e.LastEventID, 2, head)
				if err != nil {
					return nil, err
				}
				format, db := 0, d2
				if sha256Hex(d2.content) == e.SHA256 {
					format = 2
				} else if d1, err := auditDayFromDB(ctx, repo, e.Day, e.LastEventID, 1, ""); err != nil {
					return nil, err
				} else if sha256Hex(d1.content) == e.SHA256 {
					format, db = 1, d1
				} else if ferr == nil && !looksLikeChain(fileBytes) {
					db = d1 // zum Vergleich mit einer Datei im Format 1
				}

				switch {
				case ferr != nil:
					add(e.Day, file, AuditFileMissing, 0)
				case !fileOK:
					add(e.Day, file, AuditFileChanged, firstDiffLine(fileBytes, db.content))
				}
				if format == 0 {
					line := 0
					if ferr == nil {
						line = firstDiffLine(db.content, fileBytes)
					}
					add(e.Day, file, AuditDatabaseChanged, line)
				}
				// Die Kette der Datei selbst (auch ohne Datenbank prüfbar).
				dayHead := d2.head
				if ferr == nil && format != 1 && looksLikeChain(fileBytes) {
					fh, bad := checkChain(fileBytes, head)
					if bad > 0 {
						add(e.Day, file, AuditChainBroken, bad)
					} else if fileOK {
						// Datei unverändert: Ihr Kettenende ist das exportierte
						// (so zieht eine geänderte Datenbankzeile keine
						// Folgefehler in späteren Tagen nach sich).
						dayHead = fh
					}
				}
				if format == 1 {
					out.LegacyDays++
				} else {
					chained[e.Location] = true
					head = dayHead
					out.ChainHead = head
				}
				events := db.events
				if fileOK {
					events = bytes.Count(fileBytes, []byte{'\n'})
				}
				manifests[e.Location] = append(manifests[e.Location], auditManifestDay{Day: e.Day, File: auditFileName(e.Day),
					Events: events, FirstEventID: e.FirstEventID, LastEventID: e.LastEventID, SHA256: e.SHA256, ChainHead: dayHead})
			}
			for _, loc := range locations {
				if !chained[loc] {
					continue
				}
				path := filepath.Join(loc, auditManifestName())
				b, err := os.ReadFile(path)
				switch {
				case errors.Is(err, fs.ErrNotExist):
					add("", path, AuditManifestMissing, 0)
				case err != nil:
					return nil, err
				default:
					if want := buildManifest(manifests[loc]); !bytes.Equal(b, want) {
						add("", path, AuditManifestChanged, firstDiffLine(b, want))
					}
				}
			}
			days, err := repo.AuditDays(ctx)
			if err != nil {
				return nil, err
			}
			for _, d := range days {
				e, ok := exported[d]
				if !ok {
					out.NotExported = append(out.NotExported, d)
					continue
				}
				all, err := auditDayFromDB(ctx, repo, d, 0, 1, "")
				if err != nil {
					return nil, err
				}
				if all.last > e.LastEventID {
					out.NotExported = append(out.NotExported, d)
				}
			}
			if len(out.Problems) > 0 {
				out.Status = "mismatch"
			}
			return out, nil
		},
	}
}
