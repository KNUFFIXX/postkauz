package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // reiner Go-SQLite-Treiber, kein CGO
)

// store kapselt SQLite-Datenbank und Blob-Verzeichnis.
type store struct {
	db       *sql.DB
	blobsDir string
}

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
CREATE TABLE IF NOT EXISTS mailboxes (
  name TEXT PRIMARY KEY,
  role TEXT,
  uidvalidity INTEGER NOT NULL,
  uidnext INTEGER NOT NULL,
  highestmodseq INTEGER NOT NULL DEFAULT 0,
  synced_at TEXT
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY,
  blob_sha256 TEXT NOT NULL UNIQUE,
  message_id TEXT,
  in_reply_to TEXT,
  refs TEXT,
  subject TEXT,
  from_addr TEXT,
  to_addr TEXT,
  date TEXT,
  size INTEGER,
  thread_id TEXT
);
CREATE INDEX IF NOT EXISTS idx_messages_mid ON messages(message_id);
CREATE TABLE IF NOT EXISTS message_mailboxes (
  mailbox TEXT NOT NULL,
  uid INTEGER NOT NULL,
  message_rowid INTEGER NOT NULL REFERENCES messages(id),
  flags TEXT NOT NULL,
  modseq INTEGER NOT NULL DEFAULT 0,
  internaldate TEXT,
  PRIMARY KEY (mailbox, uid)
);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  subject, from_addr, to_addr, body,
  content='', contentless_delete=1,
  tokenize='unicode61 remove_diacritics 2'
);
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts_tri USING fts5(
  subject, body, content='', contentless_delete=1, tokenize='trigram'
);
`

func openStore(dir string) (*store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "mailwerk.db"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &store{db: db, blobsDir: filepath.Join(dir, "blobs")}, nil
}

func (s *store) Close() error { return s.db.Close() }

// putBlob legt die Rohmail unter blobs/<sha256>.eml ab (idempotent).
func (s *store) putBlob(raw []byte) (string, error) {
	sum := sha256.Sum256(raw)
	h := hex.EncodeToString(sum[:])
	p := filepath.Join(s.blobsDir, h+".eml")
	if _, err := os.Stat(p); err == nil {
		return h, nil
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return "", err
	}
	return h, os.Rename(tmp, p)
}

func (s *store) readBlob(h string) ([]byte, error) {
	return os.ReadFile(filepath.Join(s.blobsDir, h+".eml"))
}

// mailboxState ist der gespeicherte Synczustand eines Ordners.
type mailboxState struct {
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
}

func (s *store) getMailbox(name string) (*mailboxState, error) {
	var st mailboxState
	err := s.db.QueryRow(`SELECT uidvalidity, uidnext, highestmodseq FROM mailboxes WHERE name=?`, name).
		Scan(&st.UIDValidity, &st.UIDNext, &st.HighestModSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &st, err
}

func (s *store) putMailbox(name, role string, st mailboxState) error {
	_, err := s.db.Exec(`INSERT INTO mailboxes(name, role, uidvalidity, uidnext, highestmodseq, synced_at)
		VALUES(?,?,?,?,?,datetime('now'))
		ON CONFLICT(name) DO UPDATE SET role=excluded.role, uidvalidity=excluded.uidvalidity,
		uidnext=excluded.uidnext, highestmodseq=excluded.highestmodseq, synced_at=excluded.synced_at`,
		name, role, st.UIDValidity, st.UIDNext, st.HighestModSeq)
	return err
}

// localFlags liefert UID -> Flags fuer einen Ordner.
func (s *store) localFlags(mailbox string) (map[uint32]string, error) {
	rows, err := s.db.Query(`SELECT uid, flags FROM message_mailboxes WHERE mailbox=?`, mailbox)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[uint32]string{}
	for rows.Next() {
		var uid uint32
		var f string
		if err := rows.Scan(&uid, &f); err != nil {
			return nil, err
		}
		m[uid] = f
	}
	return m, rows.Err()
}
