// Package store persists users and game scores in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
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

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Nick returns the nickname registered for a key fingerprint, or "" if none.
func (s *Store) Nick(fingerprint string) (string, error) {
	var nick string
	err := s.db.QueryRow(`SELECT nick FROM users WHERE fingerprint = ?`, fingerprint).Scan(&nick)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return nick, err
}

// Register binds a nickname to a key fingerprint.
func (s *Store) Register(fingerprint, nick string) error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE nick = ?`, nick).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return ErrNickTaken
	}
	_, err := s.db.Exec(`INSERT INTO users (fingerprint, nick, created_at) VALUES (?, ?, ?)`,
		fingerprint, nick, time.Now().Unix())
	return err
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
