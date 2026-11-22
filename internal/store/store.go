// Package store persists users and game scores in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNickTaken is returned when a nickname already belongs to another user.
var ErrNickTaken = errors.New("nickname already taken")

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Score is one leaderboard row.
type Score struct {
	Nick   string
	Best   int
	Played int
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	fingerprint TEXT PRIMARY KEY,
	nick        TEXT NOT NULL UNIQUE COLLATE NOCASE,
	created_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS scores (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	fingerprint TEXT NOT NULL REFERENCES users(fingerprint),
	game        TEXT NOT NULL,
	points      INTEGER NOT NULL,
	created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS scores_game ON scores(game, points DESC);
`

// migrations upgrade older databases; migrations[i] moves user_version i to i+1.
var migrations = []string{
	`ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE messages (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		nick       TEXT NOT NULL,
		text       TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE audit (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		fingerprint TEXT NOT NULL,
		command     TEXT NOT NULL,
		created_at  INTEGER NOT NULL
	)`,
	`CREATE TABLE bans (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		kind       TEXT NOT NULL,
		value      TEXT NOT NULL,
		until      INTEGER NOT NULL,
		created_by TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX bans_value ON bans(value)`,
}

// Message is one saved line of the chat lobby. Nick is the name as it was
// shown when the message was posted.
type Message struct {
	Time time.Time
	Nick string
	Text string
}

// Profile is a registered user's saved data.
type Profile struct {
	Nick  string
	Theme string
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for ; version < len(migrations); version++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[version]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", version+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, version+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Profile returns the saved profile for a key fingerprint. ok is false when
// the key hasn't registered yet.
func (s *Store) Profile(fingerprint string) (p Profile, ok bool, err error) {
	err = s.db.QueryRow(`SELECT nick, theme FROM users WHERE fingerprint = ?`, fingerprint).Scan(&p.Nick, &p.Theme)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, false, nil
	}
	return p, err == nil, err
}

// Register binds a nickname to a key fingerprint.
func (s *Store) Register(fingerprint, nick string) error {
	_, err := s.db.Exec(`INSERT INTO users (fingerprint, nick, created_at) VALUES (?, ?, ?)`,
		fingerprint, nick, time.Now().Unix())
	if isUniqueViolation(err) {
		return ErrNickTaken
	}
	return err
}

// Rename changes a user's nickname. Scores follow automatically, since they
// reference the fingerprint. Changing only the letter case of your own
// nickname is allowed.
func (s *Store) Rename(fingerprint, nick string) error {
	_, err := s.db.Exec(`UPDATE users SET nick = ? WHERE fingerprint = ?`, nick, fingerprint)
	if isUniqueViolation(err) {
		return ErrNickTaken
	}
	return err
}

// SetTheme saves a user's theme choice.
func (s *Store) SetTheme(fingerprint, theme string) error {
	_, err := s.db.Exec(`UPDATE users SET theme = ? WHERE fingerprint = ?`, theme, fingerprint)
	return err
}

// ClearScores deletes all of a user's scores in every game and returns how
// many rounds were removed.
func (s *Store) ClearScores(fingerprint string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM scores WHERE fingerprint = ?`, fingerprint)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AddScore records the result of one game round.
func (s *Store) AddScore(fingerprint, game string, points int) error {
	_, err := s.db.Exec(`INSERT INTO scores (fingerprint, game, points, created_at) VALUES (?, ?, ?, ?)`,
		fingerprint, game, points, time.Now().Unix())
	return err
}

// Top returns the best score per user for a game, highest first.
func (s *Store) Top(game string, limit int) ([]Score, error) {
	rows, err := s.db.Query(`
		SELECT u.nick, MAX(s.points) AS best, COUNT(*) AS played
		FROM scores s JOIN users u ON u.fingerprint = s.fingerprint
		WHERE s.game = ?
		GROUP BY s.fingerprint
		ORDER BY best DESC, MIN(s.created_at) ASC
		LIMIT ?`, game, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Score
	for rows.Next() {
		var sc Score
		if err := rows.Scan(&sc.Nick, &sc.Best, &sc.Played); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

// SaveMessage records one chat message.
func (s *Store) SaveMessage(m Message) error {
	_, err := s.db.Exec(`INSERT INTO messages (nick, text, created_at) VALUES (?, ?, ?)`,
		m.Nick, m.Text, m.Time.Unix())
	return err
}

// PruneMessages deletes chat messages posted before t and returns how many
// were deleted.
func (s *Store) PruneMessages(t time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM messages WHERE created_at < ?`, t.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecentMessages returns the newest chat messages, oldest first.
func (s *Store) RecentMessages(limit int) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT nick, text, created_at FROM (
			SELECT id, nick, text, created_at FROM messages ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var ts int64
		if err := rows.Scan(&m.Nick, &m.Text, &ts); err != nil {
			return nil, err
		}
		m.Time = time.Unix(ts, 0)
		out = append(out, m)
	}
	return out, rows.Err()
}

// Backup writes a consistent copy of the database to path, which must not
// exist yet. It is safe while the hub is running.
func (s *Store) Backup(path string) error {
	_, err := s.db.Exec(`VACUUM INTO ?`, path)
	return err
}

// Check runs SQLite's integrity check.
func (s *Store) Check() error {
	var res string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("integrity check: %s", res)
	}
	return nil
}

// AuditEntry is one admin command, as recorded.
type AuditEntry struct {
	Time        time.Time
	Fingerprint string
	Command     string
}

// Audit records an admin command. The log is only ever appended to.
func (s *Store) Audit(e AuditEntry) error {
	_, err := s.db.Exec(`INSERT INTO audit (fingerprint, command, created_at) VALUES (?, ?, ?)`,
		e.Fingerprint, e.Command, e.Time.Unix())
	return err
}

// RecentAudit returns the newest admin commands, newest first.
func (s *Store) RecentAudit(limit int) ([]AuditEntry, error) {
	rows, err := s.db.Query(`SELECT fingerprint, command, created_at FROM audit ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		var ts int64
		if err := rows.Scan(&e.Fingerprint, &e.Command, &ts); err != nil {
			return nil, err
		}
		e.Time = time.Unix(ts, 0)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Ban kinds: a ban keeps out an SSH key or a host (see limit.Host).
const (
	BanKey  = "key"
	BanHost = "host"
)

// Ban keeps a key or a host out of the hub.
type Ban struct {
	ID      int64
	Kind    string // BanKey or BanHost
	Value   string // key fingerprint or host
	Until   time.Time
	Forever bool // Until doesn't apply
	By      string
	Created time.Time
}

func (b Ban) activeAt(now time.Time) bool { return b.Forever || b.Until.After(now) }

// AddBan stores a ban and returns its id. It replaces any earlier ban of the
// same key or host, so banning again changes how long the ban lasts.
func (s *Store) AddBan(b Ban) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM bans WHERE kind = ? AND value = ?`, b.Kind, b.Value); err != nil {
		return 0, err
	}
	var until int64
	if !b.Forever {
		until = b.Until.Unix()
	}
	res, err := tx.Exec(`INSERT INTO bans (kind, value, until, created_by, created_at) VALUES (?, ?, ?, ?, ?)`,
		b.Kind, b.Value, until, b.By, b.Created.Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// Unban lifts a ban and reports whether it existed.
func (s *Store) Unban(id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM bans WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Bans lists the bans in force at now, oldest first.
func (s *Store) Bans(now time.Time) ([]Ban, error) {
	return s.bans(now, `SELECT id, kind, value, until, created_by, created_at FROM bans ORDER BY id`)
}

// Banned returns the ban in force at now that keeps out a key fingerprint
// (empty for guests) or a host, if any.
func (s *Store) Banned(fingerprint, host string, now time.Time) (Ban, bool, error) {
	bans, err := s.bans(now, `SELECT id, kind, value, until, created_by, created_at FROM bans
		WHERE (kind = ? AND value = ? AND value != '') OR (kind = ? AND value = ?)`,
		BanKey, fingerprint, BanHost, host)
	if err != nil || len(bans) == 0 {
		return Ban{}, false, err
	}
	return bans[0], true, nil
}

func (s *Store) bans(now time.Time, query string, args ...any) ([]Ban, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ban
	for rows.Next() {
		var b Ban
		var until, created int64
		if err := rows.Scan(&b.ID, &b.Kind, &b.Value, &until, &b.By, &created); err != nil {
			return nil, err
		}
		b.Forever = until == 0
		if !b.Forever {
			b.Until = time.Unix(until, 0)
		}
		b.Created = time.Unix(created, 0)
		if b.activeAt(now) {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// FingerprintOf returns the key fingerprint registered under a nickname.
func (s *Store) FingerprintOf(nick string) (string, bool, error) {
	var fp string
	err := s.db.QueryRow(`SELECT fingerprint FROM users WHERE nick = ?`, nick).Scan(&fp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return fp, err == nil, err
}
