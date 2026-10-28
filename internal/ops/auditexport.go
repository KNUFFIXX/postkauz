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
	"slices"
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

// auditClock liefert den Zeitpunkt eines Exports (exported_at; Tests
// setzen ihn, um Läufe über Mitternacht nachzustellen).
var auditClock = time.Now

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

// auditPlan ordnet jeden Audit-Eintrag genau einem Exporttag zu (T1a
// Punkt 4, A14). Grundsatz: Ein Tag, der abgeschlossen exportiert ist,
// ändert sich nie wieder. Abgeschlossen ist ein Tag mit Nachweis in
// audit_exports, wenn der Export nach seinem Ende lief (exported_at nach
// Mitternacht UTC) oder ein späterer Tag schon einen Nachweis hat (jeder
// Export schreibt alle Tage, also lief er nach dem Ende dieses Tages).
//
// Ein Eintrag gehört zu seinem Tag (ts, UTC). Trägt er den Zeitstempel
// eines abgeschlossenen Tages und eine größere ID als dessen letzter
// exportierter Eintrag (verspäteter Eintrag, z. B. der Abschluss eines
// `sync --watch`-Laufs, der vor Mitternacht begann), wandert er zum
// nächsten Tag, und so weiter bis zum ersten Tag, der nicht abgeschlossen
// ist; Tage ohne Nachweis vor dem letzten abgeschlossenen Tag werden dabei
// übersprungen (an ihnen lief schon ein Export ohne Einträge vorbei). Die
// Zuordnung eines Eintrags ändert sich danach nicht mehr: Export und
// `audit verify` berechnen sie gleich.
//
// Seit T1b (Nachprüfung T1a) lädt der Plan nicht mehr das ganze Protokoll:
// Abgeschlossene Tage ändern sich nie, ihre Einträge liegen im ID-Bereich
// ihres Nachweises (loadAuditDay, tageweise). Geladen werden nur die
// Kandidaten der offenen Tage: Einträge ab dem Ende des letzten
// abgeschlossenen Tages c oder mit größerer ID als dessen letzter
// exportierter Eintrag. Ein Eintrag mit früherem Zeitstempel kommt nur über
// c hinweg in einen offenen Tag, braucht also eine größere ID als last(c);
// einer mit späterem liegt ohnehin ab dem Ende von c.
type auditPlan struct {
	days   []string                       // offene Exporttage, aufsteigend
	events map[string][]store.AuditRecord // je offenem Exporttag, aufsteigend nach ID
	asg    auditAssigner
}

func auditDayEnd(day string) time.Time {
	t, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Time{}
	}
	return t.Add(24 * time.Hour)
}

func nextDay(day string) string {
	return auditDayEnd(day).Format("2006-01-02")
}

// auditAssigner ordnet Einträge nach den Nachweisen ihren Exporttagen zu.
type auditAssigner struct {
	records      map[string]store.AuditExport
	lastRecorded string
}

func newAuditAssigner(done []store.AuditExport) auditAssigner {
	a := auditAssigner{records: map[string]store.AuditExport{}}
	for _, e := range done {
		a.records[e.Day] = e
		a.lastRecorded = max(a.lastRecorded, e.Day)
	}
	return a
}

// closed meldet, ob ein Tag abgeschlossen exportiert ist, mit seiner
// letzten exportierten ID.
func (a auditAssigner) closed(day string) (int64, bool) {
	e, ok := a.records[day]
	if !ok {
		return 0, false
	}
	if day < a.lastRecorded || !e.ExportedAt.Before(auditDayEnd(day)) {
		return e.LastEventID, true
	}
	return 0, false
}

// day liefert den Exporttag eines Eintrags.
func (a auditAssigner) day(r store.AuditRecord) string {
	day := r.TS.UTC().Format("2006-01-02")
	for {
		if last, ok := a.closed(day); ok {
			if r.ID <= last {
				return day
			}
			day = nextDay(day)
			continue
		}
		if _, ok := a.records[day]; !ok && day < a.lastRecorded {
			day = nextDay(day)
			continue
		}
		return day
	}
}

// lastClosed liefert den letzten abgeschlossenen Tag mit Nachweis.
func (a auditAssigner) lastClosed() (store.AuditExport, bool) {
	var best store.AuditExport
	found := false
	for d, e := range a.records {
		if _, ok := a.closed(d); ok && (!found || d > best.Day) {
			best, found = e, true
		}
	}
	return best, found
}

// buildAuditPlan ordnet die Einträge der offenen Tage nach den Nachweisen
// done (audit_exports) zu; abgeschlossene Tage lädt loadAuditDay.
func buildAuditPlan(ctx context.Context, repo AuditRepo, done []store.AuditExport) (auditPlan, error) {
	asg := newAuditAssigner(done)
	f := store.AuditFilter{}
	if c, ok := asg.lastClosed(); ok {
		f.OpenSince, f.OpenAfterID = auditDayEnd(c.Day), c.LastEventID
	}
	list, err := repo.AuditQuery(ctx, f)
	if err != nil {
		return auditPlan{}, err
	}
	plan := auditPlan{events: map[string][]store.AuditRecord{}, asg: asg}
	for _, r := range list {
		day := asg.day(r)
		if _, ok := asg.closed(day); ok {
			continue // gehört zu einem abgeschlossenen Tag (ID nicht größer als dessen letzte)
		}
		if _, ok := plan.events[day]; !ok {
			plan.days = append(plan.days, day)
		}
		plan.events[day] = append(plan.events[day], r)
	}
	slices.Sort(plan.days)
	return plan, nil
}

// loadAuditDay lädt die Einträge eines Tages mit Nachweis rec: nur den
// ID-Bereich des Nachweises, davon die, die zu diesem Tag gehören.
func loadAuditDay(ctx context.Context, repo AuditRepo, asg auditAssigner, rec store.AuditExport) ([]store.AuditRecord, error) {
	list, err := repo.AuditQuery(ctx, store.AuditFilter{AfterID: rec.FirstEventID - 1, UpToID: rec.LastEventID})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(list, func(r store.AuditRecord) bool { return asg.day(r) != rec.Day }), nil
}

// auditStrayRows findet Zeilen, die weder ein abgeschlossener Tag noch der
// Plan der offenen Tage sieht (Nachbesserung T1b, A-F4): etwa eine
// nachträglich eingeschobene Zeile mit freier niedriger ID und dem
// Zeitstempel eines abgeschlossenen Tages. Zählabgleich bis L, der größten
// letzten ID über alle abgeschlossenen Tage (nicht nur des zeitlich
// letzten: rc8 hat verspätete Einträge in alte Tage exportiert, deren
// Bereich dann über ihn hinausreicht; Nachprüfung der Nachbesserung):
// Zeilen mit id <= L = Einträge der abgeschlossenen Tage (closedRows,
// tageweise geladen, alle mit id <= L) + Zeilen des Plans mit id <= L. Nur bei Abweichung lädt sie die Zeilen bis L und nennt die
// Exporttage der fremden Zeilen (sonst kostet die Prüfung eine Zählung
// über den Primärschlüssel).
func auditStrayRows(ctx context.Context, repo AuditRepo, plan auditPlan, closedRows int) ([]string, error) {
	c, ok := plan.asg.lastClosed()
	if !ok {
		return nil, nil
	}
	upTo := int64(0)
	for d := range plan.asg.records {
		if last, closed := plan.asg.closed(d); closed {
			upTo = max(upTo, last)
		}
	}
	want := closedRows
	for _, d := range plan.days {
		for _, r := range plan.events[d] {
			if r.ID <= upTo {
				want++
			}
		}
	}
	n, err := repo.AuditCountUpTo(ctx, upTo)
	if err != nil || n == want {
		return nil, err
	}
	list, err := repo.AuditQuery(ctx, store.AuditFilter{UpToID: upTo})
	if err != nil {
		return nil, err
	}
	end := auditDayEnd(c.Day)
	var days []string
	for _, r := range list {
		day := plan.asg.day(r)
		// Offener Tag, aber nicht im Plan (der lädt ts ab dem Ende von c
		// oder id über der letzten ID von c).
		stray := r.TS.Before(end) && r.ID <= c.LastEventID
		if _, closed := plan.asg.closed(day); closed {
			rec := plan.asg.records[day]
			stray = r.ID < rec.FirstEventID || r.ID > rec.LastEventID
		}
		if stray && !slices.Contains(days, day) {
			days = append(days, day)
		}
	}
	if len(days) == 0 {
		// Zahl weicht ab, ohne dass eine Zeile zuzuordnen ist (z. B. eine
		// Zeile mit ID <= 0, die die Abfrage nicht liefert): dem letzten
		// abgeschlossenen Tag zuschreiben.
		days = append(days, c.Day)
	}
	slices.Sort(days)
	return days, nil
}

// AuditUnexportedDays liefert die Exporttage, deren Einträge noch nicht
// (vollständig) exportiert sind, nach derselben Zuordnung wie audit.export
// (für doctor).
func AuditUnexportedDays(ctx context.Context, repo AuditRepo) ([]string, error) {
	done, err := repo.AuditExports(ctx)
	if err != nil {
		return nil, err
	}
	plan, err := buildAuditPlan(ctx, repo, done)
	if err != nil {
		return nil, err
	}
	last := map[string]int64{}
	for _, e := range done {
		last[e.Day] = e.LastEventID
	}
	var out []string
	for _, d := range plan.days {
		evs := plan.events[d]
		if l, ok := last[d]; !ok || evs[len(evs)-1].ID > l {
			out = append(out, d)
		}
	}
	return out, nil // abgeschlossene Tage sind vollständig exportiert
}

// auditDayContent erzeugt den Inhalt der Exportdatei eines Tages aus
// seinen Einträgen (aufsteigend nach ID), höchstens bis lastID (0 = alle).
// Format 2 braucht den Kettenwert davor (prev). Dieselbe Funktion dient
// Export und Prüfung.
func auditDayContent(list []store.AuditRecord, lastID int64, format int, prev string) (auditDay, error) {
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
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			plan, err := buildAuditPlan(ctx, repo, done)
			if err != nil {
				return nil, err
			}
			prev := map[string]store.AuditExport{}
			for _, e := range done {
				prev[e.Day] = e
			}
			// Alle Tage: abgeschlossene mit Nachweis und die offenen des Plans.
			days := slices.Clone(plan.days)
			for _, e := range done {
				if _, ok := plan.asg.closed(e.Day); ok {
					days = append(days, e.Day)
				}
			}
			slices.Sort(days)
			days = slices.Compact(days)
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
			own, err := ownExportFiles(dir)
			if err != nil {
				return nil, err
			}
			head := auditChainGenesis
			for _, day := range days {
				p, recorded := prev[day]
				_, closed := plan.asg.closed(day)
				if closed && p.Location == dir {
					// Abgeschlossener Tag an diesem Ort: Ist die Datei
					// unverändert und setzt ihre Kette am bisherigen Ende fort,
					// bleibt sie stehen, ohne die Datenbank zu lesen.
					file := filepath.Join(dir, auditFileName(day))
					b, ferr := os.ReadFile(file)
					switch {
					case ferr == nil && sha256Hex(b) != p.SHA256:
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditExportChanged, file)
					case ferr != nil && !errors.Is(ferr, fs.ErrNotExist):
						return nil, ferr
					}
					if ferr == nil && looksLikeChain(b) {
						if fh, bad := checkChain(b, head); bad == 0 {
							f := AuditExportFile{Day: day, File: file, Events: bytes.Count(b, []byte{'\n'}), FirstEventID: p.FirstEventID,
								LastEventID: p.LastEventID, SHA256: p.SHA256, ChainHead: fh, State: "unchanged"}
							head = fh
							todo = append(todo, pending{f, nil})
							manifest = append(manifest, auditManifestDay{Day: day, File: auditFileName(day), Events: f.Events,
								FirstEventID: f.FirstEventID, LastEventID: f.LastEventID, SHA256: f.SHA256, ChainHead: fh})
							continue
						}
					}
				}
				evs := plan.events[day]
				if closed {
					// Neu schreiben (anderer Ort, Datei fehlt, altes Format,
					// Kette davor neu): Einträge aus dem Bereich des Nachweises.
					if evs, err = loadAuditDay(ctx, repo, plan.asg, p); err != nil {
						return nil, err
					}
				}
				d, err := auditDayContent(evs, 0, AuditExportFormat, head)
				if err != nil {
					return nil, err
				}
				head = d.head
				f := AuditExportFile{Day: day, File: filepath.Join(dir, auditFileName(day)), Events: d.events, FirstEventID: d.first,
					LastEventID: d.last, SHA256: sha256Hex(d.content), ChainHead: d.head, State: "written"}
				if recorded {
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
				// Am Ziel nur eigene Dateien ersetzen (A12.4): eine Datei,
				// die weder der Nachweis dieses Tages an diesem Ort noch das
				// Manifest dort als eigene ausweist, bleibt unberührt.
				if f.State != "unchanged" {
					if err := checkOwnFile(f.File, own, p, dir); err != nil {
						return nil, err
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
					SHA256: p.f.SHA256, Location: dir, ExportedAt: auditClock()}); err != nil {
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

// ErrAuditExportForeign: Am Exportziel liegt unter einem Namen, den der
// Export schreiben will, eine Datei, die er nicht selbst geschrieben hat
// (A12.4). Der Export bricht ab, bevor er etwas ersetzt.
var ErrAuditExportForeign = errors.New("audit export: the target directory holds a file this program did not write")

// ownExportFiles liest das Manifest am Ziel: Tag-Dateien, die es mit
// Prüfsumme nennt, gelten als eigene. Ein Manifest, das nicht von diesem
// Programm stammt, ist ErrAuditExportForeign.
func ownExportFiles(dir string) (map[string]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, auditManifestName()))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m auditManifest
	if json.Unmarshal(b, &m) != nil || m.Program != brand.Name || m.Format < 2 {
		return nil, fmt.Errorf("%w: %s", ErrAuditExportForeign, auditManifestName())
	}
	own := map[string]string{}
	for _, d := range m.Days {
		own[d.File] = d.SHA256
	}
	return own, nil
}

// checkOwnFile: Darf der Export path ersetzen? Ja, wenn dort nichts liegt,
// wenn der Nachweis rec dieses Tages genau diese Datei an diesem Ort
// belegt (Prüfsumme schon vorher geprüft) oder wenn das Manifest am Ziel
// sie mit ihrer Prüfsumme nennt.
func checkOwnFile(path string, own map[string]string, rec store.AuditExport, dir string) error {
	got, err := fileSHA256(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if rec.Location == dir && rec.SHA256 == got {
		return nil
	}
	if sum, ok := own[filepath.Base(path)]; ok && sum == got {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrAuditExportForeign, filepath.Base(path))
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
			plan, err := buildAuditPlan(ctx, repo, done)
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
			closedRows := 0 // Einträge der abgeschlossenen Tage (Zählabgleich, A-F4)
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

				// Welches Format ergibt die Prüfsumme des Nachweises? Die
				// Einträge des Tages werden tageweise geladen (nur der
				// ID-Bereich des Nachweises), nie das ganze Protokoll.
				evs, err := loadAuditDay(ctx, repo, plan.asg, e)
				if err != nil {
					return nil, err
				}
				if _, ok := plan.asg.closed(e.Day); ok {
					closedRows += len(evs)
				}
				d2, err := auditDayContent(evs, e.LastEventID, 2, head)
				if err != nil {
					return nil, err
				}
				format, db := 0, d2
				if sha256Hex(d2.content) == e.SHA256 {
					format = 2
				} else if d1, err := auditDayContent(evs, e.LastEventID, 1, ""); err != nil {
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
			stray, err := auditStrayRows(ctx, repo, plan, closedRows)
			if err != nil {
				return nil, err
			}
			for _, day := range stray {
				file := ""
				if e, ok := exported[day]; ok {
					file = filepath.Join(e.Location, auditFileName(day))
				}
				add(day, file, AuditDatabaseChanged, 0)
			}
			for _, d := range plan.days {
				e, ok := exported[d]
				evs := plan.events[d]
				if !ok || evs[len(evs)-1].ID > e.LastEventID {
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
