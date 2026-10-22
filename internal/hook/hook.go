// Package hook führt den Prüf-Hook vor der Vorlage eines Entwurfs aus
// (A02 E4, Schritt S2.4; docs/sicherheit.md, Abschnitt 5).
//
// Der Hook ist ein Programm des Betreibers (z. B. ein Linter für Stil und
// Pflichtangaben), eingetragen in der Policy-Datei als Liste:
//
//	[draft]
//	check_hook = ["<programm>", "<arg>", …]
//	check_hook_timeout = "30s"
//
// Regeln:
//
//   - **Kein Shell.** Das Programm wird direkt gestartet (os/exec), die
//     Argumente gehen unverändert als Liste mit. Das Programm ist ein
//     absoluter Pfad oder ein Name, der über PATH gefunden wird; ein
//     relativer Pfad mit Verzeichnis ist ein Fehler (das Arbeitsverzeichnis
//     des Hooks ist sein Temp-Verzeichnis, und das des Aufrufers ist
//     zufällig, etwa beim MCP-Server). Stapeldateien (.bat, .cmd) lehnt der
//     Runner ab, weil Windows sie über cmd.exe ausführt.
//   - **Entwurfsinhalt nur als Dateien, nie als Argumente.** In den
//     Argumenten werden die Platzhalter {eml} (die fertige Nachricht, genau
//     die Bytes, deren Hash die Freigabe bindet), {body_html}, {body_text}
//     und {meta_json} (Kopffelder, Anhangsnamen und -größen, Kontoname,
//     Identität; kein Geheimnis) durch Dateipfade ersetzt, auch mitten in
//     einem Argument („--file={eml}"). Unbekannte Platzhalter bleiben
//     stehen. Ersetzt wird in einem Durchgang (kein Platzhalter im Ersatz
//     wird erneut ersetzt).
//   - **Frisches Temp-Verzeichnis** je Lauf (Unix 0700, Dateien 0600; unter
//     Windows eine geschützte Zugriffsliste nur für den Nutzer), danach
//     gelöscht: auch nach Zeitüberschreitung, Fehler und Panic (defer).
//   - **Umgebung:** nur PATH, HOME, USERPROFILE, TEMP, TMP, SYSTEMROOT, LANG
//     (soweit gesetzt) und POSTKAUZ_DRAFT_EML, POSTKAUZ_DRAFT_BODY_HTML,
//     POSTKAUZ_DRAFT_BODY_TEXT, POSTKAUZ_DRAFT_META_JSON mit denselben
//     Pfaden wie die Platzhalter. Nie POSTKAUZ_KEY, nie
//     POSTKAUZ_PASSWORD_*, nichts sonst aus der Umgebung des Aufrufers.
//   - **Zeitgrenze** (Standard 30 s): danach wird der Prozess **samt
//     Kindern** beendet (Unix: eigene Prozessgruppe, SIGKILL an die Gruppe;
//     Windows: Job-Objekt, TerminateJobObject). Auch nach einem normalen
//     Ende werden übrig gebliebene Kinder beendet, damit niemand die
//     Dateien weiter liest. Unter Windows startet der Hook ohne eigenes
//     Konsolenfenster (CREATE_NO_WINDOW), damit beim MCP-Server kein
//     Fenster aufspringt.
//   - **Ergebnis:** Exit 0 = pass; jeder andere Exit-Code = fail; Zeit
//     abgelaufen = timeout; Programm fehlt oder startet nicht, Dateien nicht
//     schreibbar = error (nie pass). Output sind die ersten 4 KB von stdout
//     und stderr (ein gemeinsamer Kanal, in der Reihenfolge des Schreibens;
//     ungültiges UTF-8 ersetzt); was darüber hinausgeht, wird gelesen und
//     verworfen (der Hook bekommt keinen Schreibfehler), Truncated meldet
//     es. Der Output gehört ins Ergebnis von drafts.request_ready, nie ins
//     Audit oder Log (dort nur das Ergebnis pass|fail|timeout|error).
//   - Bricht der Aufrufer ab (Kontext beendet, z. B. Strg+C), wird der Hook
//     ebenso beendet und Run liefert den Fehler des Kontexts.
//   - **Hart beendeter Aufrufer** (Gegenprüfung S2.4/S2.5, K1): Stirbt die
//     CLI selbst (SIGKILL, Absturz), kann sie nicht aufräumen. Unter Linux
//     bekommt der Hook dann SIGKILL vom Kernel (Pdeathsig); seine Kinder
//     erreicht das nicht, sie laufen weiter, bis sie selbst enden (unter
//     macOS gibt es kein Gegenstück; unter Windows beendet das Schließen
//     des Job-Objekts beim Prozessende alle). Ein Kind, das mit setsid eine
//     eigene Sitzung und Prozessgruppe öffnet, entkommt unter Unix auch dem
//     Beenden der Gruppe nach Zeitüberschreitung; das ist eine Grenze,
//     keine Sandbox (docs/sicherheit.md, Abschnitt 5). Liegen gebliebene
//     Temp-Verzeichnisse (postkauz-hook-*, älter als StaleAge) löscht der
//     nächste Lauf; ein Fehler beim Löschen steht im Log (ohne Pfad).
//
// Das Paket kennt weder Datenbank noch Entwürfe; die Abbildung eines
// Entwurfs auf Input macht internal/ops (ops.NewHookChecker).
package hook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/brand"
)

// Ergebnisse eines Laufs (gleich den Werten von drafts.Hook*).
const (
	Pass    = "pass"
	Fail    = "fail"
	Timeout = "timeout"
	Error   = "error"
)

// Grenzen und Standards.
const (
	// DefaultTimeout gilt, wenn Runner.Timeout 0 ist (E4: 30 s).
	DefaultTimeout = 30 * time.Second
	// MaxOutput: so viele Bytes von stdout+stderr kommen ins Ergebnis.
	MaxOutput = 4096
	// waitDelay: so lange wartet Run nach dem Ende des Hooks (oder dem
	// Beenden) auf das Schließen der Ausgabekanäle.
	waitDelay = 2 * time.Second
	// StaleAge: So alt muss ein liegen gebliebenes Temp-Verzeichnis eines
	// Hooks sein (etwa nach SIGKILL der CLI), bevor ein späterer Lauf es
	// löscht. Die Zeitgrenze eines Hooks ist höchstens 10 Minuten; ein
	// laufender Hook eines anderen Prozesses verliert so nie seine Dateien.
	StaleAge = time.Hour
)

// Platzhalter in den Argumenten.
const (
	PlaceholderEML      = "{eml}"
	PlaceholderBodyHTML = "{body_html}"
	PlaceholderBodyText = "{body_text}"
	PlaceholderMetaJSON = "{meta_json}"
)

// Namen der Dateien im Temp-Verzeichnis.
const (
	FileEML      = "message.eml"
	FileBodyHTML = "body.html"
	FileBodyText = "body.txt"
	FileMetaJSON = "meta.json"
)

// Umgebungsvariablen mit den Pfaden der Dateien.
var (
	EnvEML      = brand.EnvPrefix + "_DRAFT_EML"
	EnvBodyHTML = brand.EnvPrefix + "_DRAFT_BODY_HTML"
	EnvBodyText = brand.EnvPrefix + "_DRAFT_BODY_TEXT"
	EnvMetaJSON = brand.EnvPrefix + "_DRAFT_META_JSON"
)

// passEnv sind die einzigen Variablen, die aus der Umgebung des Aufrufers
// an den Hook gehen (E4).
var passEnv = []string{"PATH", "HOME", "USERPROFILE", "TEMP", "TMP", "SYSTEMROOT", "LANG"}

// Runner führt einen Hook aus.
type Runner struct {
	// Command: Programm und Argumente ([draft] check_hook). Leer = kein
	// Hook, Run liefert pass, ohne etwas zu starten oder zu schreiben.
	Command []string
	// Timeout: [draft] check_hook_timeout; 0 = DefaultTimeout.
	Timeout time.Duration
	// TempDir: Ort für das Temp-Verzeichnis je Lauf; "" = os.TempDir().
	TempDir string
	// Log bekommt Fehler beim Aufräumen der Temp-Verzeichnisse (ohne Pfad,
	// ohne Inhalt); nil = verwerfen.
	Log *slog.Logger
}

func (r Runner) log() *slog.Logger {
	if r.Log == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return r.Log
}

// Input ist, was der Hook prüft.
type Input struct {
	// EML: die fertige Nachricht (RFC 5322).
	EML []byte
	// BodyHTML und BodyText: die Textteile der Nachricht.
	BodyHTML string
	BodyText string
	// Meta: JSON mit Kopffeldern, Anhängen, Konto und Identität.
	Meta []byte
}

// Result ist das Ergebnis eines Laufs.
type Result struct {
	// Status: Pass, Fail, Timeout oder Error.
	Status string
	// Output: die ersten MaxOutput Bytes von stdout+stderr (gültiges
	// UTF-8); bei Error und Timeout mit einer eigenen ersten Zeile.
	Output string
	// Truncated: Der Hook schrieb mehr als MaxOutput Bytes.
	Truncated bool
	// ExitCode des Hooks; -1, wenn er nicht regulär endete oder nie lief.
	ExitCode int
}

// testAfterWrite läuft in Tests, nachdem die Dateien geschrieben sind
// (z. B. für einen Panic); nil im Betrieb.
var testAfterWrite func(dir string)

// Run führt den Hook einmal aus. Der Fehler ist nur gesetzt, wenn ctx
// endet (Abbruch durch den Aufrufer); alles andere ist ein Result.
func (r Runner) Run(ctx context.Context, in Input) (Result, error) {
	if len(r.Command) == 0 {
		return Result{Status: Pass, ExitCode: -1}, nil
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	prog, err := resolveProgram(r.Command[0])
	if err != nil {
		return errResult(err.Error()), nil
	}
	base := r.TempDir
	if base == "" {
		base = os.TempDir()
	}
	cleanupStale(base, time.Now(), r.log())
	dir, err := os.MkdirTemp(base, tempPrefix)
	if err != nil {
		return errResult("could not create the temporary directory"), nil
	}
	defer func() {
		if err := removeAll(dir); err != nil {
			r.log().Warn("check hook: its temporary directory could not be removed; a later run removes it after an hour",
				"reason", pathless(err))
		}
	}()
	if err := restrictDir(dir); err != nil {
		return errResult("could not restrict the temporary directory to the current user"), nil
	}
	paths, err := writeFiles(dir, in)
	if err != nil {
		return errResult("could not write the files for the hook"), nil
	}
	if testAfterWrite != nil {
		testAfterWrite(dir)
	}
	return r.exec(ctx, prog, dir, paths)
}

// files sind die Pfade der vier Dateien eines Laufs.
type files struct{ eml, html, text, meta string }

func writeFiles(dir string, in Input) (files, error) {
	f := files{eml: filepath.Join(dir, FileEML), html: filepath.Join(dir, FileBodyHTML),
		text: filepath.Join(dir, FileBodyText), meta: filepath.Join(dir, FileMetaJSON)}
	for _, w := range []struct {
		path string
		data []byte
	}{{f.eml, in.EML}, {f.html, []byte(in.BodyHTML)}, {f.text, []byte(in.BodyText)}, {f.meta, in.Meta}} {
		if err := writeNew(w.path, w.data); err != nil {
			return files{}, err
		}
	}
	return f, nil
}

// writeNew legt eine neue Datei nur für den Nutzer an (O_EXCL: nie eine
// vorhandene oder einen Symlink überschreiben).
func writeNew(path string, data []byte) error {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := fh.Write(data); err != nil {
		_ = fh.Close()
		return err
	}
	return fh.Close()
}

// resolveProgram prüft das Programm und sucht es über PATH. Meldungen
// nennen den Pfad nicht (er steht in der Policy-Datei des Betreibers; das
// Ergebnis kann über MCP an einen Agenten gehen).
func resolveProgram(prog string) (string, error) {
	if strings.TrimSpace(prog) == "" {
		return "", errors.New("the program (first entry of check_hook) is empty")
	}
	if !filepath.IsAbs(prog) && strings.ContainsAny(prog, `/\`) {
		return "", errors.New("the program must be an absolute path or a name found on PATH")
	}
	path, err := exec.LookPath(prog)
	if err != nil {
		if errors.Is(err, exec.ErrDot) {
			return "", errors.New("the program was found only in the current directory; give an absolute path")
		}
		return "", errors.New("program not found (check [draft] check_hook in policy.toml)")
	}
	if isBatch(prog) || isBatch(path) {
		return "", errors.New("batch files (.bat, .cmd) run through cmd.exe, a shell; start powershell.exe -File or a program instead")
	}
	return path, nil
}

// isBatch erkennt Stapeldateien an der Endung. Windows entfernt Punkte und
// Leerzeichen am Ende eines Dateinamens („lint.bat." und „lint.bat " sind
// lint.bat), deshalb zählen sie hier nicht mit (Gegenprüfung S2.4/S2.5, K2).
func isBatch(p string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimRight(p, ". "))) {
	case ".bat", ".cmd":
		return true
	}
	return false
}

// args ersetzt die Platzhalter in den Argumenten (ein Durchgang).
func args(cmd []string, f files) []string {
	rep := strings.NewReplacer(PlaceholderEML, f.eml, PlaceholderBodyHTML, f.html, PlaceholderBodyText, f.text, PlaceholderMetaJSON, f.meta)
	out := make([]string, 0, len(cmd)-1)
	for _, a := range cmd[1:] {
		out = append(out, rep.Replace(a))
	}
	return out
}

// env ist die knappe Umgebung des Hooks.
func env(f files) []string {
	var out []string
	for _, k := range passEnv {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return append(out, EnvEML+"="+f.eml, EnvBodyHTML+"="+f.html, EnvBodyText+"="+f.text, EnvMetaJSON+"="+f.meta)
}

func (r Runner) exec(ctx context.Context, prog, dir string, f files) (Result, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tree, err := newTree()
	if err != nil {
		return errResult("could not prepare the process control for the hook"), nil
	}
	defer tree.close()

	cmd := exec.CommandContext(runCtx, prog, args(r.Command, f)...)
	cmd.Dir = dir
	cmd.Env = env(f)
	cmd.Stdin = nil // leer (os.DevNull)
	out := &capture{max: MaxOutput}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = waitDelay
	tree.prepare(cmd)
	cmd.Cancel = func() error { return tree.kill(cmd.Process) }

	if err := cmd.Start(); err != nil {
		return errResult("the program could not be started"), nil
	}
	if err := tree.attach(cmd.Process); err != nil {
		_ = tree.kill(cmd.Process)
		_ = cmd.Wait()
		return errResult("could not place the hook under process control"), nil
	}
	_ = cmd.Wait() // Ergebnis steht in ProcessState und runCtx
	// Übrig gebliebene Kinder beenden (auch nach normalem Ende).
	_ = tree.kill(cmd.Process)

	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	res := Result{Output: out.text(), Truncated: out.truncated, ExitCode: -1}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		res.Status = Timeout
		res.Output, res.Truncated = withNote(fmt.Sprintf("check hook timed out after %s (process and its children ended); its output so far:", timeout), res.Output, res.Truncated)
	case cmd.ProcessState != nil && cmd.ProcessState.Success():
		res.Status = Pass
	case cmd.ProcessState != nil:
		res.Status = Fail
		if strings.TrimSpace(res.Output) == "" {
			res.Output = fmt.Sprintf("check hook failed with exit code %d and no output", res.ExitCode)
		}
	default:
		// Wait scheiterte ohne Prozessstatus (sollte nicht vorkommen).
		return errResult("the hook ended without an exit status"), nil
	}
	return res, nil
}

// withNote stellt eine eigene Zeile vor die Ausgabe und hält die Grenze
// MaxOutput ein (gekürzt wird die Ausgabe des Hooks, nie die Zeile).
func withNote(note, output string, truncated bool) (string, bool) {
	s := note + "\n" + output
	if len(s) <= MaxOutput {
		return s, truncated
	}
	return cutUTF8(s, MaxOutput), true
}

func errResult(msg string) Result {
	return Result{Status: Error, Output: "check hook error: " + msg, ExitCode: -1}
}

// capture nimmt höchstens max Bytes und verwirft den Rest, ohne dem Hook
// einen Schreibfehler zu melden (sonst würde ein gesprächiger Hook mit
// Exit 0 an einem SIGPIPE scheitern).
type capture struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *capture) Write(p []byte) (int, error) {
	if rest := c.max - c.buf.Len(); len(p) > rest {
		c.buf.Write(p[:max(rest, 0)])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// text liefert die Ausgabe als gültiges UTF-8 (ein an der Grenze
// angeschnittenes Zeichen fällt weg).
func (c *capture) text() string {
	b := c.buf.Bytes()
	if c.truncated {
		for i := 1; i <= utf8.UTFMax && i <= len(b); i++ {
			if utf8.RuneStart(b[len(b)-i]) {
				if !utf8.FullRune(b[len(b)-i:]) {
					b = b[:len(b)-i]
				}
				break
			}
		}
	}
	return strings.ToValidUTF8(string(b), "\uFFFD")
}

// cutUTF8 kürzt s auf höchstens n Bytes an einer Zeichengrenze.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// tempPrefix beginnt den Namen jedes Temp-Verzeichnisses eines Hooks.
var tempPrefix = brand.Name + "-hook-"

// removeAll löscht das Temp-Verzeichnis. Unter Windows kann eine Datei
// kurz nach dem Beenden eines Prozesses noch gesperrt sein; dann noch
// einige Male versuchen. Liefert den letzten Fehler.
func removeAll(dir string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.RemoveAll(dir); err == nil || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return err
}

// cleanupStale löscht in base liegen gebliebene Temp-Verzeichnisse von
// Hooks (Name postkauz-hook-*, kein Symlink, älter als StaleAge, unter
// Unix im Besitz des Nutzers), etwa nach SIGKILL der CLI (K1). Ein Versuch
// je Verzeichnis; Fehler kommen ohne Pfad ins Log.
func cleanupStale(base string, now time.Time, log *slog.Logger) {
	ents, err := os.ReadDir(base)
	if err != nil {
		return
	}
	removed, failed := 0, 0
	var reason string
	for _, e := range ents {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempPrefix) {
			continue
		}
		p := filepath.Join(base, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.IsDir() || now.Sub(fi.ModTime()) < StaleAge || !ownedByUser(fi) {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			failed++
			reason = pathless(err)
			continue
		}
		removed++
	}
	if removed > 0 {
		log.Info("check hook: removed temporary directories left behind by earlier runs", "count", removed)
	}
	if failed > 0 {
		log.Warn("check hook: temporary directories left behind by earlier runs could not be removed", "count", failed,
			"reason", reason)
	}
}

// pathless ist der Fehlertext ohne Pfad (der Pfad nennt den Nutzer und
// gehört nicht ins Log).
func pathless(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Op + ": " + pe.Err.Error()
	}
	return "unknown error"
}
