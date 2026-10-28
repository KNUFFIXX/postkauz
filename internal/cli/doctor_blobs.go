package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DoctorBlobs ist der Teil von `doctor --blobs` (A03 A5.4): Prüfung des
// Blob-Stores gegen die Tabelle blobs und die Anhänge der Entwürfe. Er
// nennt nur Zahlen und Hashes (höchstens blobExamples je Art), nie Inhalt,
// Dateinamen von Anhängen oder Betreffs. doctor ändert dabei nichts: keine
// Reparatur, kein Löschen, kein Eintrag in die Datenbank.
type DoctorBlobs struct {
	// Status: ok | problems | error (Prüfung abgebrochen) | not_checked.
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Mode: sample (blobSamplePercent %, mindestens ein Blob) | all.
	Mode string `json:"mode"`
	// Rows: Zeilen in blobs; Files: Blob-Dateien (verschiedene Hashes).
	Rows  int `json:"rows"`
	Files int `json:"files"`
	// Referenced: Dateien mit Zeile oder Verweis eines Entwurfs, aus denen
	// die Stichprobe gezogen wird (--blobs=all: alle). Dazu das Ergebnis
	// von blob.Store.Verify (Hash; Größe aus der Zeile, falls vorhanden).
	Referenced int `json:"referenced"`
	Checked    int `json:"checked"`
	OK         int `json:"ok"`
	Corrupt    int `json:"corrupt"`
	Missing    int `json:"missing"`    // zwischen Verzeichnislauf und Prüfung verschwunden
	Unreadable int `json:"unreadable"` // nicht lesbar (z. B. Rechte)
	// RowsWithoutFile: Zeilen in blobs ohne Datei (alle Zeilen, nicht nur
	// die Stichprobe). Ein Problem: Rohmail bzw. Anhang ist verloren.
	RowsWithoutFile int `json:"rows_without_file"`
	// FilesWithoutRow: Dateien ohne Zeile und ohne Verweis eines Entwurfs.
	// Unschädlich (Abbruch zwischen Datei und Zeile), Kandidaten für das
	// spätere blobs.gc; nur eine Warnung.
	FilesWithoutRow int `json:"files_without_row"`
	// DraftAttachments: Dateien ohne Zeile, die ein Entwurf als Anhang
	// verweist (Dateianhänge, A13.3); nicht verwaist.
	DraftAttachments int `json:"draft_attachments"`
	// DraftAttachmentsWithoutFile: Anhänge von Entwürfen ohne Zeile und
	// ohne Datei. Ein Problem: Der Entwurf lässt sich nicht mehr bauen.
	DraftAttachmentsWithoutFile int `json:"draft_attachments_without_file"`
	// UnknownFiles: Dateien mit ungültigem Namen oder am falschen Ort;
	// TempFiles: liegen gebliebene Temporärdateien unter tmp/.
	UnknownFiles int `json:"unknown_files"`
	TempFiles    int `json:"temp_files"`
	// Problems zählt corrupt, missing, unreadable, rows_without_file und
	// draft_attachments_without_file (Exit 1).
	Problems int `json:"problems"`
	// Examples: je Art höchstens blobExamples Hashes (sortiert).
	Examples   map[string][]string `json:"examples,omitempty"`
	DurationMS int64               `json:"duration_ms"`
}

// errBlobCheck: doctor --blobs hat ein Problem gefunden (Exit 1).
var errBlobCheck = errors.New("blob check failed")

const (
	blobSamplePercent = 1
	blobExamples      = 10
)

// parseBlobsFlag prüft den Wert von --blobs: "" = keine Prüfung, "sample"
// (--blobs allein) oder "all".
func parseBlobsFlag(v string) (string, error) {
	switch v {
	case "", "sample", "all":
		return v, nil
	}
	return "", usageError{errors.New(`invalid value for --blobs: use --blobs for a 1% sample or --blobs=all`)}
}

func doctorBlobs(ctx context.Context, a *app, rep DoctorReport, mode string) *DoctorBlobs {
	start := time.Now()
	out := &DoctorBlobs{Mode: mode}
	defer func() { out.DurationMS = time.Since(start).Milliseconds() }()
	switch {
	case rep.ConfigError != "":
		out.Status, out.Reason = "not_checked", "invalid configuration"
		return out
	case rep.Database == "ok", rep.Database == "needs_migration" && rep.SchemaVersion > 0:
	default:
		out.Status, out.Reason = "not_checked", "database "+rep.Database
		return out
	}
	fail := func(what string, err error) *DoctorBlobs {
		out.Status, out.Reason = "error", what+": "+err.Error()
		out.Problems++
		return out
	}
	db, err := store.Open(a.paths.Database)
	if err != nil {
		return fail("database", err)
	}
	defer db.Close()

	// Erst die Zeilen und Verweise, dann das Verzeichnis: Datei und Zeile
	// entstehen in dieser Reihenfolge (Put, dann Zeile). Was ein
	// gleichzeitiger Sync danach schreibt, erscheint höchstens als Datei
	// ohne Zeile (unschädlich), nie als Zeile ohne Datei.
	rows, err := db.BlobRows(ctx)
	if err != nil {
		return fail("table blobs", err)
	}
	draftRefs, err := db.DraftAttachmentHashes(ctx)
	if err != nil {
		return fail("drafts", err)
	}
	out.Rows = len(rows)

	files := map[string]bool{}
	bs, err := blob.OpenExisting(a.paths.Blobs, blob.Options{})
	switch {
	case errors.Is(err, blob.ErrNotFound):
		// Kein Blob-Verzeichnis: keine Dateien (jede Zeile fehlt dann).
	case err != nil:
		return fail("blob store", err)
	default:
		err := bs.Scan(func(e blob.Entry) {
			switch e.Kind {
			case blob.EntryBlob:
				files[e.SHA256] = true
			case blob.EntryTemp:
				out.TempFiles++
			default:
				out.UnknownFiles++
			}
		})
		if err != nil {
			return fail("blob store", err)
		}
	}
	out.Files = len(files)

	ex := map[string][]string{}
	note := func(kind, sum string) {
		if !blob.ValidHash(sum) {
			// Eine Zeile oder ein Verweis mit kaputtem Hash: der Wert bleibt weg.
			sum = "(invalid hash)"
		}
		ex[kind] = append(ex[kind], sum)
	}

	// Prüfmenge: vorhandene Dateien mit Zeile (Größe aus der Zeile) und
	// Dateianhänge von Entwürfen (nur Hash; ihre Größe steht in keiner Zeile).
	type target struct {
		sum  string
		size int64
	}
	var pool []target
	hasRow := make(map[string]bool, len(rows))
	for _, r := range rows {
		hasRow[r.SHA256] = true
		if files[r.SHA256] {
			pool = append(pool, target{r.SHA256, r.Size})
		} else {
			out.RowsWithoutFile++
			note("rows_without_file", r.SHA256)
		}
	}
	byDraft := map[string]bool{}
	for _, sum := range draftRefs {
		if hasRow[sum] {
			continue // Anhang aus dem Spiegel: hat eine Zeile
		}
		byDraft[sum] = true
		if files[sum] {
			pool = append(pool, target{sum, 0})
		} else {
			out.DraftAttachmentsWithoutFile++
			note("draft_attachments_without_file", sum)
		}
	}
	for sum := range files {
		switch {
		case hasRow[sum]:
		case byDraft[sum]:
			out.DraftAttachments++
		default:
			out.FilesWithoutRow++
			note("files_without_row", sum)
		}
	}

	out.Referenced = len(pool)
	n := len(pool)
	if mode != "all" {
		n = blobSampleSize(n)
		rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	}
	for _, t := range pool[:n] {
		if err := ctx.Err(); err != nil {
			return fail("blob check", err)
		}
		out.Checked++
		switch err := bs.Verify(t.sum, t.size); {
		case err == nil:
			out.OK++
		case errors.Is(err, blob.ErrCorrupt):
			out.Corrupt++
			note("corrupt", t.sum)
		case errors.Is(err, blob.ErrNotFound):
			out.Missing++
			note("missing", t.sum)
		default:
			// Die Meldung bleibt weg (sie nennt Pfade); der Hash genügt.
			out.Unreadable++
			note("unreadable", t.sum)
		}
	}

	out.Problems += out.Corrupt + out.Missing + out.Unreadable + out.RowsWithoutFile + out.DraftAttachmentsWithoutFile
	for kind, list := range ex {
		slices.Sort(list)
		list = slices.Compact(list)
		ex[kind] = list[:min(len(list), blobExamples)]
	}
	if len(ex) > 0 {
		out.Examples = ex
	}
	out.Status = "ok"
	if out.Problems > 0 {
		out.Status = "problems"
	}
	return out
}

// blobSampleSize: blobSamplePercent % von n, abgerundet, mindestens 1
// (wenn es etwas zu prüfen gibt).
func blobSampleSize(n int) int {
	if n == 0 {
		return 0
	}
	return max(1, n*blobSamplePercent/100)
}

func printDoctorBlobs(w io.Writer, b *DoctorBlobs) {
	fmt.Fprintln(w, "blobs:")
	if b.Status == "not_checked" {
		fmt.Fprintln(w, termLine("  result:  not checked ("+b.Reason+")"))
		return
	}
	mode := fmt.Sprintf("sample (%d%%)", blobSamplePercent)
	if b.Mode == "all" {
		mode = "all"
	}
	if b.Status == "error" {
		fmt.Fprintln(w, termLine("  error:   "+b.Reason))
	} else {
		fmt.Fprintf(w, "  mode:    %s, %d of %s verified in %dms\n", mode, b.Checked, plural(b.Referenced, "referenced file", "referenced files"), b.DurationMS)
		fmt.Fprintf(w, "  verify:  ok %d, corrupt %d, missing %d, unreadable %d\n", b.OK, b.Corrupt, b.Missing, b.Unreadable)
		fmt.Fprintf(w, "  rows:    %s in table blobs, %d without file\n", plural(b.Rows, "row", "rows"), b.RowsWithoutFile)
		fmt.Fprintf(w, "  files:   %s, %s (no row, referenced by drafts), %d without row (harmless, left for a later blob gc)\n",
			plural(b.Files, "blob file", "blob files"), plural(b.DraftAttachments, "draft attachment", "draft attachments"), b.FilesWithoutRow)
		if b.DraftAttachmentsWithoutFile > 0 {
			fmt.Fprintf(w, "  drafts:  %s without file\n", plural(b.DraftAttachmentsWithoutFile, "draft attachment", "draft attachments"))
		}
		fmt.Fprintf(w, "  other:   %s, %s in tmp/ (left as they are)\n", plural(b.UnknownFiles, "unknown file", "unknown files"), plural(b.TempFiles, "temp file", "temp files"))
		for _, kind := range []string{"corrupt", "missing", "unreadable", "rows_without_file", "draft_attachments_without_file", "files_without_row"} {
			if list := b.Examples[kind]; len(list) > 0 {
				fmt.Fprintf(w, "  %s: %s\n", strings.ReplaceAll(kind, "_", " "), strings.Join(list, ", "))
			}
		}
	}
	if b.Problems == 0 {
		fmt.Fprintln(w, "  result:  ok")
	} else {
		fmt.Fprintf(w, "  result:  %d problems\n", b.Problems)
	}
}
