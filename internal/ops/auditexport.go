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
	"github.com/KNUFFIXX/postkauz/internal/flock"
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
	Format  int    `json:"format"`
	Program string `json:"program"`
	// DataDirID ist die volle Kennung des Datenverzeichnisses, das den
	// Ordner beschreibt (Nachbesserung T3, Punkt 2): Eine wiederhergestellte
	// Kopie (neue Kennung) erkennt so den Ordner des Originals. Fehlt in
	// Manifesten vor 0.3.0-alpha.7.
	DataDirID string             `json:"datadir_id,omitempty"`
	Chain     string             `json:"chain"`
	Genesis   string             `json:"genesis"`
	Head      string             `json:"head"`
	Days      []auditManifestDay `json:"days"`
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

func buildManifest(days []auditManifestDay, dataDirID string) []byte {
	m := auditManifest{Format: AuditExportFormat, Program: brand.Name, DataDirID: dataDirID, Chain: auditChainRule, Genesis: auditChainGenesis,
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

// ErrAuditExportRunning: Ein anderer Export dieses Datenverzeichnisses
// läuft (Sperrdatei audit-export.lock neben der Datenbank).
var ErrAuditExportRunning = errors.New("another audit export of this data directory is running")

// ErrAuditExportOtherInstance: Das Manifest am Ziel stammt von einem
// anderen Datenverzeichnis, etwa dem Original einer Wiederherstellung
// (Nachbesserung T3, Punkt 2). Der Export bricht ab, bevor er etwas
// schreibt.
var ErrAuditExportOtherInstance = errors.New("audit export: the target directory belongs to another data directory")

// auditExportJournal ist der Teil des Stores für die Merker eines
// laufenden Exports (store.SQLite; Nachbesserung T3, Punkt 1). Ein
// AuditRepo ohne ihn (Testattrappen) schreibt ohne Merker.
type auditExportJournal interface {
	AuditExportPending(ctx context.Context) ([]store.AuditExportPending, error)
	AddAuditExportPending(ctx context.Context, p store.AuditExportPending) error
}

// auditPendingSet sind die Merker abgebrochener Exporte: Eine Datei mit
// genau dieser Prüfsumme an diesem Ort für diesen Tag hat dieses
// Programm geschrieben, ihr Nachweis fehlt nur.
type auditPendingSet []store.AuditExportPending

func loadAuditPending(ctx context.Context, repo AuditRepo) (auditPendingSet, error) {
	j, ok := repo.(auditExportJournal)
	if !ok {
		return nil, nil
	}
	return j.AuditExportPending(ctx)
}

func (ps auditPendingSet) has(day, location, sum string) bool {
	return sum != "" && slices.Contains(ps, store.AuditExportPending{Day: day, Location: location, SHA256: sum})
}

// auditDataDirID liefert die volle Kennung des Datenverzeichnisses (""
// ohne Store, etwa in Testattrappen, oder ohne Kennung).
func auditDataDirID(ctx context.Context, repo AuditRepo) string {
	r, ok := repo.(interface {
		DataDirIDFull(ctx context.Context) (string, error)
	})
	if !ok {
		return ""
	}
	id, err := r.DataDirIDFull(ctx)
	if err != nil {
		return ""
	}
	return id
}

// lockBesideDB holt die Sperrdatei name neben der Datenbank, ohne zu
// warten (flock: fällt mit dem Prozess). Ohne Datenbankdatei (Attrappen)
// gibt es keine Sperre. Belegt → busy.
func lockBesideDB(repo any, name string, busy error) (func(), error) {
	r, ok := repo.(interface{ Path() string })
	if !ok || r.Path() == "" {
		return func() {}, nil
	}
	l, err := flock.TryLock(filepath.Join(filepath.Dir(r.Path()), name))
	if errors.Is(err, flock.ErrLocked) {
		return nil, busy
	}
	if err != nil {
		return nil, err
	}
	return l.Release, nil
}

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
			// Nur ein Export je Datenverzeichnis zugleich (Nachbesserung T3,
			// Punkt 1d); der Trockenlauf schreibt nichts und sperrt nicht.
			if !call.DryRun {
				unlock, err := lockBesideDB(repo, "audit-export.lock", ErrAuditExportRunning)
				if err != nil {
					return nil, err
				}
				defer unlock()
			}
			// Archivierte Tage (audit rotate) liest der Export aus ihrem
			// Archiv, als stünden sie noch in der Datenbank.
			src, closeArchives, err := openAuditSource(ctx, repo)
			defer closeArchives()
			if err != nil {
				return nil, err
			}
			done, err := repo.AuditExports(ctx)
			if err != nil {
				return nil, err
			}
			plan, err := buildAuditPlan(ctx, src.repo, done)
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
			pend, err := loadAuditPending(ctx, repo)
			if err != nil {
				return nil, err
			}
			dataDirID := auditDataDirID(ctx, repo)
			own, err := ownExportFiles(dir, dataDirID, done)
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
					sum := ""
					if ferr == nil {
						sum = sha256Hex(b)
					}
					switch {
					case ferr == nil && sum != p.SHA256 && !pend.has(day, dir, sum):
						// Weder Nachweis noch Merker eines abgebrochenen
						// Exports (der wird unten neu geschrieben).
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditExportChanged, file)
					case ferr != nil && !errors.Is(ferr, fs.ErrNotExist):
						return nil, ferr
					}
					if ferr == nil && sum == p.SHA256 && looksLikeChain(b) {
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
					if a := src.archiveOf(day); a != nil && a.problem != "" {
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditArchive, a.path)
					}
					if evs, err = loadAuditDay(ctx, src.repo, plan.asg, p); err != nil {
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
				if recorded && p.Location == "" {
					// Nachweis aus dem Original einer Wiederherstellung (A10,
					// Ort geleert): Dessen Datei gehört nicht hierher.
					f.State = "updated"
				} else if recorded {
					// Die frühere Datei muss unverändert sein, bevor sie ersetzt wird.
					// Eine Datei mit der Prüfsumme eines Merkers stammt von
					// einem abgebrochenen Export dieses Programms (Punkt 1).
					old := filepath.Join(p.Location, auditFileName(day))
					got, err := fileSHA256(old)
					switch {
					case err == nil && got != p.SHA256 && !pend.has(day, p.Location, got):
						return nil, fmt.Errorf("%w: %s (run audit verify)", ErrAuditExportChanged, old)
					case err == nil && got == f.SHA256 && got == p.SHA256 && p.Location == dir:
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
					if err := checkOwnFile(f.File, own, p, dir, pend); err != nil {
						return nil, err
					}
				}
				todo = append(todo, pending{f, d.content})
				manifest = append(manifest, auditManifestDay{Day: day, File: auditFileName(day), Events: d.events,
					FirstEventID: d.first, LastEventID: d.last, SHA256: f.SHA256, ChainHead: d.head})
			}
			out.ChainHead = head
			// partial ist das Manifest nach dem Nachweis von todo[upTo]: Tage
			// bis dahin und unveränderte Tage mit den neuen Werten, noch
			// nicht geschriebene mit ihrem Eintrag im bisherigen Manifest,
			// wenn ihr Nachweis an diesem Ort genau diese Datei belegt (so
			// rechnet audit verify das Manifest aus den Nachweisen nach).
			partial := func(upTo int) []auditManifestDay {
				var list []auditManifestDay
				for j, t := range todo {
					if j <= upTo || t.f.State == "unchanged" {
						list = append(list, manifest[j])
						continue
					}
					rec, recorded := prev[t.f.Day]
					if d, ok := own[auditFileName(t.f.Day)]; ok && recorded && rec.Location == dir && d.Day == rec.Day && d.SHA256 == rec.SHA256 {
						list = append(list, d)
					}
				}
				return list
			}
			for i, p := range todo {
				out.Files = append(out.Files, p.f)
				if call.DryRun || p.f.State == "unchanged" {
					continue
				}
				// Erst der Merker, dann die Datei, dann Nachweis und Löschen
				// des Merkers in einer Transaktion (Punkt 1a): Bricht der
				// Lauf dazwischen ab, weist der Merker die Datei als eigene
				// aus.
				if j, ok := repo.(auditExportJournal); ok {
					if err := j.AddAuditExportPending(ctx, store.AuditExportPending{Day: p.f.Day, Location: dir, SHA256: p.f.SHA256}); err != nil {
						return nil, err
					}
				}
				if err := writeFileAtomic(p.f.File, p.content); err != nil {
					return nil, err
				}
				// Der Nachweis zur geschriebenen Datei kommt auch nach
				// Strg+C noch in die Datenbank, mit eigener Frist (Punkt 1c).
				sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), auditGrace)
				err := repo.SaveAuditExport(sctx, store.AuditExport{Day: p.f.Day, FirstEventID: p.f.FirstEventID, LastEventID: p.f.LastEventID,
					SHA256: p.f.SHA256, Location: dir, ExportedAt: auditClock()})
				cancel()
				if err != nil {
					return nil, err
				}
				// Manifest des Stands nach diesem Nachweis (Nachbesserung
				// T3 2, Punkt 1): Bricht der Lauf danach ab (Strg+C, Fehler,
				// Rechner aus), passt das Manifest trotzdem zu den
				// Nachweisen, und audit verify bleibt ok.
				if err := writeFileAtomic(out.Manifest, buildManifest(partial(i), dataDirID)); err != nil {
					return nil, err
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if !call.DryRun {
				if err := writeFileAtomic(out.Manifest, buildManifest(manifest, dataDirID)); err != nil {
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
// Programm stammt, ist ErrAuditExportForeign. Seit der Nachbesserung T3
// (Punkt 2) muss das Manifest auch zu diesem Datenverzeichnis gehören
// (dataDirID, checkManifestOwner), sonst ErrAuditExportOtherInstance. Ohne
// bekannte Kennung (Attrappen) entfällt der Vergleich.
func ownExportFiles(dir, dataDirID string, done []store.AuditExport) (map[string]auditManifestDay, error) {
	b, err := os.ReadFile(filepath.Join(dir, auditManifestName()))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]auditManifestDay{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m auditManifest
	if json.Unmarshal(b, &m) != nil || m.Program != brand.Name || m.Format < 2 {
		return nil, fmt.Errorf("%w: %s", ErrAuditExportForeign, auditManifestName())
	}
	if dataDirID != "" {
		if err := checkManifestOwner(dir, dataDirID, m, done); err != nil {
			return nil, err
		}
	}
	own := map[string]auditManifestDay{}
	for _, d := range m.Days {
		own[d.File] = d
	}
	return own, nil
}

// checkManifestOwner: Gehört das Manifest m am Ort dir zu diesem
// Datenverzeichnis?
//
//   - Mit Kennung: nur dieselbe. Zweite Nachbesserung T3, Punkt 7c: Auch bei
//     derselben Kennung (Wiederherstellung mit --keep-identity) nicht, wenn
//     diese Datenbank an diesem Ort keinen Nachweis hat, aber Nachweise des
//     Originals ohne Ort (A10).
//   - Ohne Kennung (Manifest vor 0.3.0-alpha.7): wenn diese Datenbank einen
//     Nachweis genau an diesem Ort hat oder (Punkt 3) jeder Tag des
//     Manifests einem ihrer Nachweise mit gleichem Tag, gleicher Prüfsumme
//     und einem Ort entspricht, gleich in welcher Schreibweise (unter
//     Windows Groß- und Kleinschreibung, ein neuer Laufwerksbuchstabe von
//     Google Drive). Eine wiederhergestellte Kopie hat nur Nachweise ohne
//     Ort und bleibt abgewiesen.
func checkManifestOwner(dir, dataDirID string, m auditManifest, done []store.AuditExport) error {
	recordedHere := slices.ContainsFunc(done, func(e store.AuditExport) bool { return e.Location == dir })
	switch {
	case m.DataDirID != "" && m.DataDirID != dataDirID:
		return fmt.Errorf("%w: %s (its manifest names another data directory; use an export directory of its own for each data directory)",
			ErrAuditExportOtherInstance, dir)
	case m.DataDirID != "":
		if !recordedHere && slices.ContainsFunc(done, func(e store.AuditExport) bool { return e.Location == "" }) {
			return fmt.Errorf("%w: %s (this database is a restored copy with the identity of the data directory that exports there; "+
				"use an export directory of its own)", ErrAuditExportOtherInstance, dir)
		}
		return nil
	case recordedHere || legacyManifestMatches(m, done):
		return nil
	}
	last := ""
	for _, e := range done {
		if e.Location != "" {
			last = e.Location // nach Tag sortiert: der Ort des jüngsten Nachweises
		}
	}
	if last == "" {
		return fmt.Errorf("%w: %s (its manifest has no data directory id, and this database has no audit export recorded there, "+
			"as after a restore; use an export directory of its own)", ErrAuditExportOtherInstance, dir)
	}
	return fmt.Errorf("%w: %s (its manifest has no data directory id and does not match the audit exports of this database, "+
		"last recorded at %s; if that is the same folder, run audit export --to %q once, otherwise use an export directory of its own)",
		ErrAuditExportOtherInstance, dir, last, last)
}

// legacyManifestMatches: Jeder Tag des Manifests entspricht einem Nachweis
// dieser Datenbank (gleicher Tag, gleiche Prüfsumme, Ort nicht leer). Ein
// Manifest ohne Tage belegt nichts.
func legacyManifestMatches(m auditManifest, done []store.AuditExport) bool {
	if len(m.Days) == 0 {
		return false
	}
	rec := map[string]store.AuditExport{}
	for _, e := range done {
		rec[e.Day] = e
	}
	for _, d := range m.Days {
		e, ok := rec[d.Day]
		if !ok || e.Location == "" || e.SHA256 != d.SHA256 || d.File != auditFileName(d.Day) {
			return false
		}
	}
	return true
}

// checkOwnFile: Darf der Export path ersetzen? Ja, wenn dort nichts liegt,
// wenn der Nachweis rec dieses Tages genau diese Datei an diesem Ort
// belegt (Prüfsumme schon vorher geprüft), wenn ein Merker eines
// abgebrochenen Exports sie belegt (gleicher Tag, gleicher Ort) oder wenn
// das Manifest am Ziel sie mit ihrer Prüfsumme nennt.
func checkOwnFile(path string, own map[string]auditManifestDay, rec store.AuditExport, dir string, pend auditPendingSet) error {
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
	if day, ok := auditDayOfFile(filepath.Base(path)); ok && pend.has(day, dir, got) {
		return nil
	}
	if d, ok := own[filepath.Base(path)]; ok && d.SHA256 == got {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrAuditExportForeign, filepath.Base(path))
}

// auditDayOfFile liest den Tag aus dem Namen einer Exportdatei.
func auditDayOfFile(name string) (string, bool) {
	prefix, suffix := brand.Name+"-audit-", ".jsonl"
	if len(name) != len(prefix)+10+len(suffix) || name[:len(prefix)] != prefix || name[len(name)-len(suffix):] != suffix {
		return "", false
	}
	return name[len(prefix) : len(prefix)+10], true
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
	// AuditExportIncomplete: Die Datei stammt von einem Export, der vor
	// seinem Nachweis abbrach (Merker, Nachbesserung T3, Punkt 1). Kein
	// Manipulationsbefund; der nächste audit export schließt ihn ab.
	AuditExportIncomplete = "export_incomplete"
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
	// OriginDays sind Tage, die das Original vor einer Wiederherstellung
	// exportiert hat (A10, Ort geleert): Nur die Datenbank wird gegen die
	// Prüfsumme des Nachweises geprüft; die Dateien liegen beim Original.
	OriginDays int `json:"origin_days,omitempty"`
	// Incomplete nennt Dateien eines abgebrochenen Exports (Problemcode
	// export_incomplete): nicht vollständig exportiert, kein Befund; der
	// Status bleibt ok, der nächste audit export schließt sie ab.
	Incomplete []AuditProblem `json:"incomplete,omitempty"`
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
      "problem": {"type": "string", "enum": ["file_missing", "file_changed", "database_changed", "chain_broken", "manifest_missing", "manifest_changed", "archive_missing", "archive_changed"]},
      "line": {"type": "integer"}},
      "required": ["day", "file", "problem"]}},
    "not_exported": {"type": "array", "items": {"type": "string"}},
    "chain_head": {"type": "string"},
    "legacy_days": {"type": "integer"},
    "origin_days": {"type": "integer"},
    "incomplete": {"type": "array", "items": {"type": "object", "properties": {
      "day": {"type": "string"}, "file": {"type": "string"}, "problem": {"type": "string", "enum": ["export_incomplete"]}},
      "required": ["day", "file", "problem"]}}
  },
  "required": ["status", "days_checked", "problems", "not_exported"]
}`),
		Handler: func(ctx context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			src, closeArchives, err := openAuditSource(ctx, repo)
			defer closeArchives()
			if err != nil {
				return nil, err
			}
			out, _, err := verifyAudit(ctx, src)
			if err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

// verifyAudit ist audit verify über Datenbank und Archive (src). heads
// nennt je exportiertem Tag den Kettenwert nach ihm (audit rotate: Anker).
// Seit T3 (E8, A14): Ein Archiv, das fehlt oder sich verändert hat, ist ein
// Befund (archive_missing, archive_changed); seine Tage prüft verify dann
// nur an den Exportdateien, und die Kette läuft ab dem gespeicherten Anker
// weiter. Bei einem verwendbaren Archiv muss die Kette am Ende seines
// letzten Tages den Anker ergeben, sonst chain_broken.
func verifyAudit(ctx context.Context, src auditSource) (AuditVerifyOutput, map[string]string, error) {
	repo := src.repo
	heads := map[string]string{}
	out := AuditVerifyOutput{Status: "ok", Problems: []AuditProblem{}, NotExported: []string{}}
	add := func(day, file, problem string, line int) {
		out.Problems = append(out.Problems, AuditProblem{Day: day, File: file, Problem: problem, Line: line})
	}
	for _, a := range src.archives {
		if a.problem != "" {
			add(a.FromDay+".."+a.ToDay, a.path, a.problem, 0)
		}
	}
	done, err := repo.AuditExports(ctx)
	if err != nil {
		return out, nil, err
	}
	plan, err := buildAuditPlan(ctx, repo, done)
	if err != nil {
		return out, nil, err
	}
	pend, err := loadAuditPending(ctx, src.base)
	if err != nil {
		return out, nil, err
	}
	dataDirID := auditDataDirID(ctx, src.base)
	exported := map[string]store.AuditExport{}
	// Manifest je Ort; nur geprüft, wenn dort ein Tag im Format 2 liegt.
	manifests := map[string][]auditManifestDay{}
	chained := map[string]bool{}
	var locations []string
	head := auditChainGenesis
	closedRows := 0       // Einträge der abgeschlossenen Tage (Zählabgleich, A-F4)
	originBroken := false // ein Tag des Originals passt nicht mehr (A10)
	for _, e := range done {
		exported[e.Day] = e
		out.DaysChecked++
		if e.Location == "" {
			// Wiederhergestellt (A10): Der Export gehört zum Original,
			// sein Ort ist hier unbekannt. Geprüft werden nur die
			// Zeilen der Datenbank gegen die Prüfsumme des Nachweises.
			evs, err := loadAuditDay(ctx, repo, plan.asg, e)
			if err != nil {
				return out, nil, err
			}
			if _, ok := plan.asg.closed(e.Day); ok {
				closedRows += len(evs)
			}
			d2, err := auditDayContent(evs, e.LastEventID, 2, head)
			if err != nil {
				return out, nil, err
			}
			switch {
			case sha256Hex(d2.content) == e.SHA256:
				head = d2.head
				out.ChainHead = head
			default:
				d1, err := auditDayContent(evs, e.LastEventID, 1, "")
				if err != nil {
					return out, nil, err
				}
				switch {
				case sha256Hex(d1.content) == e.SHA256:
					out.LegacyDays++
				case originBroken:
					// Ohne Datei ist das Kettenende des Vortags nur aus der
					// Datenbank bekannt; nach einer veränderten Zeile lässt
					// sich dieser Tag nicht mehr prüfen.
					add(e.Day, "", AuditChainBroken, 0)
				default:
					add(e.Day, "", AuditDatabaseChanged, 0)
					originBroken = true
				}
				head = d2.head
			}
			// Archiv der Rotation (T3) in einer Wiederherstellung: Kette am
			// Ende seines letzten Tages gegen den Anker, wie unten.
			if arc := src.archiveOf(e.Day); arc != nil && e.Day == arc.ToDay {
				if arc.problem != "" {
					head = arc.ChainHead
				} else if head != arc.ChainHead {
					add(e.Day, arc.path, AuditChainBroken, 0)
				}
				out.ChainHead = head
			}
			heads[e.Day] = head
			out.OriginDays++
			continue
		}
		if _, ok := manifests[e.Location]; !ok {
			locations = append(locations, e.Location)
			manifests[e.Location] = nil
		}
		file := filepath.Join(e.Location, auditFileName(e.Day))
		fileBytes, ferr := os.ReadFile(file)
		if ferr != nil && !errors.Is(ferr, fs.ErrNotExist) {
			return out, nil, ferr
		}
		fileOK := ferr == nil && sha256Hex(fileBytes) == e.SHA256

		// Welches Format ergibt die Prüfsumme des Nachweises? Die
		// Einträge des Tages werden tageweise geladen (nur der
		// ID-Bereich des Nachweises), nie das ganze Protokoll.
		evs, err := loadAuditDay(ctx, repo, plan.asg, e)
		if err != nil {
			return out, nil, err
		}
		if _, ok := plan.asg.closed(e.Day); ok {
			closedRows += len(evs)
		}
		d2, err := auditDayContent(evs, e.LastEventID, 2, head)
		if err != nil {
			return out, nil, err
		}
		format, db := 0, d2
		if sha256Hex(d2.content) == e.SHA256 {
			format = 2
		} else if d1, err := auditDayContent(evs, e.LastEventID, 1, ""); err != nil {
			return out, nil, err
		} else if sha256Hex(d1.content) == e.SHA256 {
			format, db = 1, d1
		} else if ferr == nil && !looksLikeChain(fileBytes) {
			db = d1 // zum Vergleich mit einer Datei im Format 1
		}

		switch {
		case ferr != nil:
			add(e.Day, file, AuditFileMissing, 0)
		case !fileOK && pend.has(e.Day, e.Location, sha256Hex(fileBytes)):
			// Abgebrochener Export (Merker): unten als export_incomplete.
		case !fileOK:
			add(e.Day, file, AuditFileChanged, firstDiffLine(fileBytes, db.content))
		}
		// Tag eines Archivs, das fehlt oder verändert ist: Die
		// Zeilen sind nicht lesbar, das meldet der Befund des Archivs.
		arc := src.archiveOf(e.Day)
		unusable := arc != nil && arc.problem != ""
		if format == 0 && !unusable {
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
		if arc != nil && e.Day == arc.ToDay {
			switch {
			case unusable:
				dayHead = arc.ChainHead // weiter ab dem Anker
			case dayHead != arc.ChainHead:
				add(e.Day, arc.path, AuditChainBroken, 0)
			}
		}
		if format == 1 {
			out.LegacyDays++
		} else {
			chained[e.Location] = true
			head = dayHead
			out.ChainHead = head
		}
		heads[e.Day] = head
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
			return out, nil, err
		default:
			// Ein Manifest ohne Kennung (vor 0.3.0-alpha.7) gilt bis zum
			// nächsten Export weiter.
			want := buildManifest(manifests[loc], dataDirID)
			if !bytes.Equal(b, want) && !bytes.Equal(b, buildManifest(manifests[loc], "")) {
				add("", path, AuditManifestChanged, firstDiffLine(b, want))
			}
		}
	}
	// Dateien abgebrochener Exporte (Merker), die noch am Ort liegen.
	// Zweite Nachbesserung T3: Ein Merker für einen Tag, dessen Nachweis an
	// einem anderen Ort steht, gehört zu einem aufgegebenen Ordner und wird
	// übergangen (Punkt 7b). Liegt am Ort eines Merkers eine Datei, die
	// weder zu ihm noch zu einem anderen Merker dieses Tages und Ortes passt,
	// und hat der Tag dort keinen Nachweis (den prüft die Schleife oben),
	// ist sie verändert (Punkt 6).
	seen := map[string]bool{}
	for _, p := range pend {
		e, recorded := exported[p.Day]
		if recorded && e.Location != p.Location {
			continue
		}
		file := filepath.Join(p.Location, auditFileName(p.Day))
		if seen[file] {
			continue
		}
		got, err := fileSHA256(file)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue // Abbruch vor dem Umbenennen der Datei
		case err != nil:
			return out, nil, err
		}
		seen[file] = true
		switch {
		case pend.has(p.Day, p.Location, got):
			if !recorded || e.SHA256 != got {
				out.Incomplete = append(out.Incomplete, AuditProblem{Day: p.Day, File: file, Problem: AuditExportIncomplete})
			}
		case !recorded:
			add(p.Day, file, AuditFileChanged, 0)
		}
	}
	stray, err := auditStrayRows(ctx, repo, plan, closedRows)
	if err != nil {
		return out, nil, err
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
	return out, heads, nil
}
