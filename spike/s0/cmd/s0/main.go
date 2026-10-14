// Befehl s0 ist der Phase-0-Spike fuer Mailwerk: Er prueft, ob Go mit
// github.com/emersion/go-imap/v2 als Protokollbasis taugt.
//
// Unterbefehle (jeweils JSON-Report auf stdout und optional in -out):
//
//	caps      Verbinden, Login, CAPABILITY, LIST + STATUS
//	sync      Erst- oder Inkrementalsync (je nach gespeichertem Zustand)
//	mutate    Von aussen (zweite Verbindung) Flags aendern, loeschen, anhaengen
//	draft     Antwort-Entwurf per APPEND in den Entwuerfe-Ordner, zuruecklesen
//	idle      IDLE 15 s mit Fremd-APPEND, danach Verbindungsabbruch + Wiederaufnahme
//	qresync   Pruefen, was go-imap nach ENABLE QRESYNC mit VANISHED macht
//	scenario  caps, sync (frisch), mutate, sync (inkrementell, Pruefung), draft, idle, qresync
//	parse     Offline: alle .eml mit go-message und enmime parsen, Threads bilden
//	fts       Offline: FTS5-Tokenizer und Umlaut-/Teilwortsuche pruefen
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"
)

// config haelt alle Kommandozeilenparameter.
type config struct {
	Server   string
	Addr     string
	TLS      bool
	Insecure bool
	User     string
	Pass     string
	DataDir  string
	Corpus   string
	Out      string
	Window   int
	KickCmd  string
	IdleSecs int
	Fresh    bool
	Verbose  bool
	PlainWD  bool
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Aufruf: s0 <caps|sync|mutate|draft|idle|qresync|scenario|parse|fts> [Flags]")
		os.Exit(2)
	}
	sub := os.Args[1]
	fs := flag.NewFlagSet(sub, flag.ExitOnError)
	var cfg config
	fs.StringVar(&cfg.Server, "server", "dovecot", "Bezeichnung des Servers im Report")
	fs.StringVar(&cfg.Addr, "addr", "127.0.0.1:31143", "IMAP-Adresse host:port")
	fs.BoolVar(&cfg.TLS, "tls", false, "implizites TLS (IMAPS)")
	fs.BoolVar(&cfg.Insecure, "insecure", false, "Zertifikat nicht pruefen (NUR Docker-Testserver)")
	fs.StringVar(&cfg.User, "user", "spike", "Benutzername")
	fs.StringVar(&cfg.Pass, "pass", "pass", "Passwort")
	fs.StringVar(&cfg.DataDir, "data", "data/dovecot", "Verzeichnis fuer SQLite + Blobs")
	fs.StringVar(&cfg.Corpus, "corpus", "corpus", "Korpusverzeichnis mit manifest.json")
	fs.StringVar(&cfg.Out, "out", "", "Report zusaetzlich in diese Datei schreiben")
	fs.IntVar(&cfg.Window, "window", 200, "UID-Fenstergroesse beim Erstsync")
	fs.StringVar(&cfg.KickCmd, "kick-cmd", "", "Shell-Befehl, der die Verbindung serverseitig kappt (z. B. doveadm kick)")
	fs.IntVar(&cfg.IdleSecs, "idle-secs", 15, "Dauer des IDLE-Tests in Sekunden")
	fs.BoolVar(&cfg.Fresh, "fresh", false, "Datenverzeichnis vor dem Lauf leeren")
	fs.BoolVar(&cfg.Verbose, "v", false, "Fortschritt auf stderr")
	fs.BoolVar(&cfg.PlainWD, "plain-word-decoder", false, "go-imap-Standard-WordDecoder (ohne Charset-Reader) verwenden")
	_ = fs.Parse(os.Args[2:])

	start := time.Now()
	var res any
	var err error
	switch sub {
	case "caps":
		res, err = runCaps(&cfg)
	case "sync":
		res, err = runSync(&cfg)
	case "mutate":
		res, err = runMutate(&cfg)
	case "draft":
		res, err = runDraft(&cfg)
	case "idle":
		res, err = runIdle(&cfg)
	case "qresync":
		res, err = runQResync(&cfg)
	case "scenario":
		res, err = runScenario(&cfg)
	case "parse":
		res, err = runParse(&cfg)
	case "fts":
		res, err = runFTS(&cfg)
	default:
		err = fmt.Errorf("unbekannter Unterbefehl %q", sub)
	}
	report := map[string]any{
		"command":    sub,
		"server":     cfg.Server,
		"started_at": start.Format(time.RFC3339),
		"duration_s": time.Since(start).Seconds(),
		"result":     res,
	}
	if err != nil {
		report["error"] = err.Error()
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(b))
	if cfg.Out != "" {
		_ = os.WriteFile(cfg.Out, b, 0o644)
	}
	if err != nil {
		os.Exit(1)
	}
}

// logf schreibt Fortschritt auf stderr, wenn -v gesetzt ist.
func logf(cfg *config, format string, args ...any) {
	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05.000 ")+format+"\n", args...)
	}
}
