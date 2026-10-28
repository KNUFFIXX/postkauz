package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // reiner Go-Treiber, registriert "sqlite"; FTS5 ist enthalten

	"github.com/KNUFFIXX/postkauz/internal/flock"
)

// ErrSchemaTooNew: Die Datenbank wurde von einer neueren Programmversion migriert.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this program")

// ErrMigrationBlocked: Eine Migration steht an, aber ein anderer Prozess
// hat die Datenbank offen (geteilte Prozesssperre, W1) oder ein Sync bzw.
// Import einer älteren Version hält eine Kontosperre im Datenverzeichnis
// (T1a Punkt 7). Zwei Programmversionen dürfen nie zugleich auf derselben
// Datenbank arbeiten; erst die anderen Prozesse beenden (die Aufgaben
// `sync --watch` und Audit-Export, den MCP-Server der Claude-App).
var ErrMigrationBlocked = errors.New("store: another process uses this database (sync, import, MCP server, audit export or another command); stop it before this version migrates the database")

// ErrDatabaseBusy: Ein anderer Prozess migriert die Datenbank gerade (hält
// die Prozesssperre exklusiv); Open hat openLockWait lang gewartet.
var ErrDatabaseBusy = errors.New("store: another process is migrating this database; try again when it has finished")

// ProcessLockPath ist die Prozesssperre neben der Datenbank (W1): Jeder
// Prozess, der die Datenbank öffnet, hält sie geteilt, solange er läuft;
// Migrate hält sie exklusiv. Erst unter der Sperre wird das Schema
// geprüft, sodass keine andere Version es danach noch ändern kann.
func ProcessLockPath(dbPath string) string { return dbPath + ".lock" }

// openLockWait: So lange wartet Open auf eine laufende Migration eines
// anderen Prozesses (Tests setzen es herab).
var openLockWait = 5 * time.Second

// ErrMigrationMismatch: Eine angewendete Migration trägt einen anderen
// Namen als die eingebettete mit derselben Nummer (T1a Punkt 7). Die
// Datenbank stammt dann von einem anderen Programmstand (z. B. einem
// Entwicklungszweig); dieses Programm fasst sie nicht an.
var ErrMigrationMismatch = errors.New("store: the database was migrated by a different program build")

// KnownSchemaVersion ist die höchste eingebettete Migration.
func KnownSchemaVersion() int {
	ms, err := Migrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

func unixMilliUTC(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// SQLite ist die Store-Implementierung auf modernc.org/sqlite.
type SQLite struct {
	db   *sql.DB
	path string // "" bei :memory:
	// plock ist die gehaltene Prozesssperre (geteilt; exklusiv nur während
	// Migrate); nil bei :memory:.
	plock *flock.Lock
	// LastSnapshot ist der Pfad des Snapshots, den das letzte Migrate vor
	// einer Migration angelegt hat ("" = keiner nötig).
	LastSnapshot string
}

var _ Store = (*SQLite)(nil)

// Open öffnet (und erzeugt) die Datenbank unter path. ":memory:" ergibt eine
// private In-Memory-Datenbank (Tests). Die Datenbank darf nie in einem
// Cloud-synchronisierten Ordner liegen (docs/datenmodell.md).
//
// Open hält ab hier bis Close die Prozesssperre neben der Datenbank geteilt
// (ProcessLockPath); migriert ein anderer Prozess gerade, wartet es bis
// openLockWait und meldet sonst ErrDatabaseBusy.
func Open(path string) (st *SQLite, err error) {
	return OpenWith(path, Options{})
}

// Options sind Einstellungen für OpenWith; der Nullwert entspricht Open.
type Options struct {
	// BusyTimeout: so lange wartet eine Verbindung auf die Schreibsperre
	// eines anderen Schreibers (0 = DefaultBusyTimeout). Tests mit
	// absichtlich hoher Konkurrenz setzen mehr (Zusatz (d) zu T3: unter
	// Volllast reichten 5 s nicht immer).
	BusyTimeout time.Duration
}

// DefaultBusyTimeout ist die Wartezeit auf die Schreibsperre im Betrieb.
const DefaultBusyTimeout = 5 * time.Second

// OpenWith öffnet wie Open, mit Einstellungen.
func OpenWith(path string, o Options) (st *SQLite, err error) {
	busy := o.BusyTimeout
	if busy <= 0 {
		busy = DefaultBusyTimeout
	}
	var plock *flock.Lock
	if path != ":memory:" {
		if plock, err = lockShared(path); err != nil {
			return nil, err
		}
		defer func() {
			if err != nil {
				plock.Release()
			}
		}()
	}
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()))
	// Schreibende Transaktionen beginnen mit BEGIN IMMEDIATE: Sie holen die
	// Schreibsperre sofort und warten sonst bis busy_timeout. Mit dem
	// Standard (DEFERRED) liest eine Transaktion erst und scheitert beim
	// ersten Schreiben sofort mit SQLITE_BUSY, wenn ein anderer Prozess
	// (z. B. `sync --watch` neben `account test`) inzwischen geschrieben
	// hat; busy_timeout hilft dann nicht. Alle BeginTx im Store schreiben.
	q.Set("_txlock", "immediate")
	if path != ":memory:" {
		q.Add("_pragma", "journal_mode(WAL)")
		// Memory-mapped I/O: Jede Verbindung hat ihren eigenen Seiten-Cache
		// (2 MB); kurzlebige Prozesse (search) und die parallelen
		// Verbindungen der Suche starten kalt. Mit mmap lesen sie direkt aus
		// dem Seiten-Cache des Betriebssystems, ohne Kopie (gemessen bei
		// 100.000 Nachrichten: häufige Suchwörter von rund 450 auf 250 ms;
		// docs/suche_messung.md). Bis 1 GiB der Datei, darüber normales Lesen.
		q.Add("_pragma", "mmap_size(1073741824)")
	}
	dsn := "file:" + path + "?" + q.Encode()
	if path == ":memory:" {
		dsn = "file::memory:?" + q.Encode()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1) // jede Verbindung hätte sonst eine eigene Datenbank
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if path != ":memory:" {
		// Nur für den Nutzer lesbar (docs/sicherheit.md, Abschnitt 10);
		// SQLite legt Dateien sonst nach umask an (meist 0644).
		for _, f := range []string{path, path + "-wal", path + "-shm"} {
			_ = os.Chmod(f, 0o600)
		}
	}
	st = &SQLite{db: db, plock: plock}
	if path != ":memory:" {
		st.path = path
	}
	return st, nil
}

// lockShared holt die Prozesssperre geteilt; hält ein anderer Prozess sie
// exklusiv (Migration), wartet es bis openLockWait.
func lockShared(dbPath string) (*flock.Lock, error) {
	deadline := time.Now().Add(openLockWait)
	for {
		l, err := flock.TryLockShared(ProcessLockPath(dbPath))
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, flock.ErrLocked) {
			return nil, fmt.Errorf("store: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, ErrDatabaseBusy
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lockExclusive steigt für eine Migration auf die exklusive Prozesssperre
// um: Die geteilte wird freigegeben (Umwandeln ist nicht portabel, siehe
// flock.TryLockShared), dann exklusiv gesperrt, ohne zu warten. Hat ein
// anderer Prozess die Datenbank offen, kommt ErrMigrationBlocked und die
// geteilte Sperre wird wieder geholt.
func (s *SQLite) lockExclusive() error {
	s.plock.Release()
	s.plock = nil
	if testHookUnlocked != nil {
		testHookUnlocked()
	}
	x, err := flock.TryLock(ProcessLockPath(s.path))
	if err == nil {
		s.plock = x
		return nil
	}
	if l, e := lockShared(s.path); e == nil {
		s.plock = l
	}
	if errors.Is(err, flock.ErrLocked) {
		return ErrMigrationBlocked
	}
	return fmt.Errorf("store: %w", err)
}

// testHookUnlocked läuft in lockExclusive zwischen Freigabe der geteilten
// und Versuch der exklusiven Sperre (nur Tests: ein anderer Prozess
// migriert genau in diesem Fenster).
var testHookUnlocked func()

// lockSharedAgain kehrt nach einer Migration zur geteilten Sperre zurück.
// Zwischen Freigabe und neuer Sperre kann ein anderer Prozess
// dazwischenkommen; deshalb liest der Aufrufer das Schema danach neu.
func (s *SQLite) lockSharedAgain() error {
	s.plock.Release()
	s.plock = nil
	l, err := lockShared(s.path)
	if err != nil {
		return err
	}
	s.plock = l
	return nil
}

// Path ist der Pfad der Datenbankdatei ("" bei :memory:).
func (s *SQLite) Path() string { return s.path }

// Close schließt die Datenbank und gibt danach die Prozesssperre frei.
func (s *SQLite) Close() error {
	err := s.db.Close()
	s.plock.Release()
	s.plock = nil
	return err
}

// Migrate wendet ausstehende Migrationen je in einer eigenen Transaktion an.
// Hat die Datenbank schon ein Schema (Bestand), legt es vorher einen
// Snapshot per VACUUM INTO neben der Datenbankdatei an
// (<db>.pre-NNNN-<Zeit>.bak, 0600; docs/datenmodell.md, Migrationen).
func (s *SQLite) Migrate(ctx context.Context) error {
	return s.migrateTo(ctx, 0)
}

// fkOffDirective in der ersten Zeile einer Migration schaltet die
// Fremdschlüssel für deren Dauer ab (Tabellenumbau ohne CASCADE-Folgen);
// danach muss PRAGMA foreign_key_check leer sein.
const fkOffDirective = "-- postkauz:foreign_keys=off"

// migrateTo migriert bis einschließlich Version max (0 = alle).
//
// Ablauf (W1): Schema unter der geteilten Prozesssperre aus Open prüfen;
// steht nichts an, bleibt es dabei. Sonst exklusiv sperren (scheitert,
// solange ein anderer Prozess die Datenbank offen hat), das Schema unter
// der Sperre neu lesen und prüfen, migrieren, zur geteilten Sperre
// zurückkehren und das Schema noch einmal prüfen.
func (s *SQLite) migrateTo(ctx context.Context, max int) (err error) {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return err
	}
	ms, err := Migrations()
	if err != nil {
		return err
	}
	if max <= 0 || max > len(ms) {
		max = len(ms)
	}
	have, err := s.checkSchema(ctx, ms)
	if err != nil || have >= max {
		return err
	}
	s.LastSnapshot = ""
	if s.path != "" {
		if err := s.lockExclusive(); err != nil {
			return err
		}
		defer func() {
			if e := s.lockSharedAgain(); e != nil && err == nil {
				err = e
				return
			}
			if err == nil {
				// Ein anderer Prozess kann zwischen Freigabe und neuer
				// Sperre migriert haben.
				_, err = s.checkSchema(ctx, ms)
			}
		}()
		// Unter der exklusiven Sperre neu lesen: Ein anderer Prozess kann
		// seit der ersten Prüfung migriert haben.
		if have, err = s.checkSchema(ctx, ms); err != nil || have >= max {
			return err
		}
		if have > 0 {
			// Ältere Versionen (bis 0.2.0-rc8) kennen die Prozesssperre
			// nicht; ihr Sync oder Import hält nur die Kontosperre. Die
			// Sperren bleiben bis zum Ende gehalten, damit keiner
			// mittendrin beginnt.
			release, err := flock.AccountLocks(filepath.Dir(s.path))
			if errors.Is(err, flock.ErrLocked) {
				return ErrMigrationBlocked
			}
			if err != nil {
				return err
			}
			defer release()
			if s.LastSnapshot, err = s.snapshot(ctx, have+1); err != nil {
				return err
			}
		}
	}
	for _, m := range ms {
		if m.Version <= have || m.Version > max {
			continue
		}
		if err := s.apply(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// checkSchema liest die Schemaversion und prüft sie gegen die
// eingebetteten Migrationen (zu neu, fremder Name).
func (s *SQLite) checkSchema(ctx context.Context, ms []Migration) (int, error) {
	have, err := s.SchemaVersion(ctx)
	if err != nil {
		return 0, err
	}
	// Schutz vor stiller Beschädigung: Eine ältere Binärdatei fasst eine
	// Datenbank mit neuerem Schema nicht an (docs/datenmodell.md, Migrationen).
	if have > len(ms) {
		return have, fmt.Errorf("%w: database has schema version %d, this program knows up to %d; use a newer version",
			ErrSchemaTooNew, have, len(ms))
	}
	if err := s.checkMigrationNames(ctx, ms); err != nil {
		return have, err
	}
	return have, nil
}

// checkMigrationNames vergleicht die angewendeten Migrationen mit den
// eingebetteten: gleiche Nummer, anderer Name ist ErrMigrationMismatch.
func (s *SQLite) checkMigrationNames(ctx context.Context, ms []Migration) error {
	rows, err := s.db.QueryContext(ctx, `SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		var name string
		if err := rows.Scan(&v, &name); err != nil {
			return err
		}
		if v >= 1 && v <= len(ms) && ms[v-1].Name != name {
			return fmt.Errorf("%w: migration %04d is %q in the database, %q in this program", ErrMigrationMismatch, v, name, ms[v-1].Name)
		}
	}
	return rows.Err()
}

// snapshot schreibt eine konsistente Kopie der Datenbank (VACUUM INTO).
func (s *SQLite) snapshot(ctx context.Context, next int) (string, error) {
	name := fmt.Sprintf("%s.pre-%04d-%s.bak", s.path, next, time.Now().UTC().Format("20060102T150405.000Z"))
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return "", fmt.Errorf("store: snapshot before migration %04d: %w", next, err)
	}
	_ = os.Chmod(name, 0o600)
	return filepath.Clean(name), nil
}

// migrationChecks prüft vor einer Migration, ob die Daten passen; ein
// Fehler bricht sie ab, bevor etwas geändert ist (keine stille
// Bereinigung).
var migrationChecks = map[int]func(context.Context, *sql.Tx) error{
	17: func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM message_mailboxes WHERE uid <= 0`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%d mailbox entries have UID 0 or less; nothing was changed, please report this (A6)", n)
		}
		return nil
	},
	// 0020 baut tags und message_tags mit den Regeln aus E5 neu: Bestehende
	// Zeilen müssen sie schon erfüllen (bis S3 schrieb nichts in diese
	// Tabellen; eine Zeile, die nicht passt, wird nicht still geändert).
	20: func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM tags
			WHERE length(name) NOT BETWEEN 1 AND 64 OR name GLOB '*[^a-z0-9._-]*')
			+ (SELECT count(*) FROM message_tags WHERE set_by IS NULL OR NOT (set_by = 'cli' OR set_by GLOB 'key:?*'))`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%d tag entries do not match the tag rules (E5); nothing was changed, please report this", n)
		}
		return nil
	},
}

// migrationRewrites passt das SQL einer Migration an einen Bestand an,
// den ein früherer Zwischenbuild hinterlassen hat; die Datei auf main bleibt
// unverändert (Rahmen 4: eine Migration auf main wird nie geändert). Die
// Funktion läuft in der Transaktion der Migration, nach migrationChecks.
var migrationRewrites = map[int]func(context.Context, *sql.Tx, string) (string, error){
	// Zusatz (b) der Prüfsitzung DRACO-PK09 zu T3: Eine Datenbank aus einem
	// Zwischenbuild von T2 hat mailboxes.virtual_view schon auf Schema 20;
	// dort scheiterte 0021 mit „duplicate column name“. Steht die Spalte
	// schon da, entfällt nur das ALTER; die Kennzeichnung nach Namen läuft
	// wie sonst. Werte außerhalb von 0/1 brechen ab (keine stille Bereinigung).
	21: func(ctx context.Context, tx *sql.Tx, src string) (string, error) {
		has, err := hasColumn(ctx, tx, "mailboxes", "virtual_view")
		if err != nil || !has {
			return src, err
		}
		if !strings.Contains(src, alter0021) {
			return "", fmt.Errorf("column virtual_view exists, but the ALTER statement of this migration is unknown; nothing was changed")
		}
		var bad int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM mailboxes WHERE virtual_view IS NULL OR virtual_view NOT IN (0,1)`).Scan(&bad); err != nil {
			return "", err
		}
		if bad > 0 {
			return "", fmt.Errorf("column virtual_view exists with %d values other than 0 or 1; nothing was changed, please report this", bad)
		}
		return strings.Replace(src, alter0021, "", 1), nil
	},
}

// alter0021 ist die Anweisung aus 0021_virtual_view.sql, die
// migrationRewrites[21] auslässt, wenn die Spalte schon existiert.
const alter0021 = "ALTER TABLE mailboxes ADD COLUMN virtual_view INTEGER NOT NULL DEFAULT 0 CHECK (virtual_view IN (0,1));"

// hasColumn sagt, ob eine Tabelle eine Spalte hat (Namen nur aus festen
// Konstanten, nie aus Eingaben).
func hasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0, err
}

// fkCheckTables nennt je Migration mit fkOffDirective die Tabellen, die
// sie neu baut; nur diese prüft PRAGMA foreign_key_check (K1): Eine
// Prüfung der ganzen Datenbank dauert bei großen Beständen und bräche
// eine Migration wegen eines Altfehlers in einer Tabelle ab, die sie gar
// nicht anfasst. Ohne Eintrag (0002, 0010, 0014, längst angewendet): die
// ganze Datenbank, wie bisher.
var fkCheckTables = map[int][]string{
	17: {"message_mailboxes"},
	20: {"jobs", "actions", "action_targets", "tags", "message_tags"},
}

// foreignKeyCheck prüft die Fremdschlüssel der Tabellen (nil = alle) und
// nennt beim ersten Verstoß Tabelle, Zeile und Zieltabelle, nie Inhalte.
func foreignKeyCheck(ctx context.Context, tx *sql.Tx, tables []string) error {
	queries := []string{`PRAGMA foreign_key_check`}
	if tables != nil {
		queries = queries[:0]
		for _, t := range tables {
			// Tabellennamen stammen aus fkCheckTables, nie aus Eingaben.
			queries = append(queries, `PRAGMA foreign_key_check("`+t+`")`)
		}
	}
	for _, q := range queries {
		rows, err := tx.QueryContext(ctx, q)
		if err != nil {
			return err
		}
		var table, parent string
		var rowid sql.NullInt64
		var fkid int64
		bad := rows.Next()
		if bad {
			err = rows.Scan(&table, &rowid, &parent, &fkid)
		}
		_ = rows.Close()
		if err != nil {
			return err
		}
		if bad {
			return fmt.Errorf("foreign key check failed: table %s, row %d refers to a missing row in %s; nothing was changed",
				table, rowid.Int64, parent)
		}
	}
	return nil
}

// apply wendet eine Migration an: eigene Verbindung (PRAGMA foreign_keys
// gilt je Verbindung und nur außerhalb einer Transaktion), eine Transaktion.
func (s *SQLite) apply(ctx context.Context, m Migration) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	fkOff := strings.HasPrefix(m.SQL, fkOffDirective)
	if fkOff {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return err
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`) }()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if check := migrationChecks[m.Version]; check != nil {
		if err := check(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	src := m.SQL
	if rewrite := migrationRewrites[m.Version]; rewrite != nil {
		if src, err = rewrite(ctx, tx, src); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, src); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
	}
	if fkOff {
		if err := foreignKeyCheck(ctx, tx, fkCheckTables[m.Version]); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?,?,?)`,
		m.Version, m.Name, time.Now().UnixMilli()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SchemaVersion liefert die höchste angewendete Migration.
func (s *SQLite) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		// Tabelle fehlt noch: leere Datenbank.
		var n int
		if e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='schema_migrations'`).Scan(&n); e == nil && n == 0 {
			return 0, nil
		}
		return 0, err
	}
	return int(v.Int64), nil
}
