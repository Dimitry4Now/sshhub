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
