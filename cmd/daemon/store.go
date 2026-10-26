package main

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go driver: no CGO / gcc needed on Windows
)

// Schema notes
//   - Timestamps are unix seconds (UTC): compact, sortable, trivial to bucket
//     by local day in the dashboard.
//   - One row = one continuous stretch of time in one (app, url|title) pair.
//   - duration_s is a virtual generated column, so it costs no disk space.
//   - Indexes cover the dashboard's two access patterns: "everything in a
//     time range" and "category X in a time range".
const schemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
	id         INTEGER PRIMARY KEY,
	start_ts   INTEGER NOT NULL,
	end_ts     INTEGER NOT NULL,
	app        TEXT    NOT NULL,
	title      TEXT    NOT NULL DEFAULT '',
	url        TEXT    NOT NULL DEFAULT '',
	category   TEXT    NOT NULL DEFAULT 'Uncategorized',
	duration_s INTEGER GENERATED ALWAYS AS (end_ts - start_ts) VIRTUAL,
	CHECK (end_ts >= start_ts)
);
CREATE INDEX IF NOT EXISTS idx_sessions_start    ON sessions (start_ts);
CREATE INDEX IF NOT EXISTS idx_sessions_category ON sessions (category, start_ts);

-- v2: AI classification cache, one row per distinct activity
-- (see activityKey). Joined with sessions at query time, so
-- labels apply retroactively and can be cleared and redone.
CREATE TABLE IF NOT EXISTS ai_labels (
	key        TEXT PRIMARY KEY,
	kind       TEXT NOT NULL,
	category   TEXT NOT NULL DEFAULT '',
	subject    TEXT NOT NULL DEFAULT '',
	topic      TEXT NOT NULL DEFAULT '',
	model      TEXT NOT NULL DEFAULT '',
	created_ts INTEGER NOT NULL
);
`

const schemaVersion = 2

type Store struct {
	db *sql.DB
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// A single connection serialises writes, keeps per-connection PRAGMAs
	// alive for the process lifetime, and keeps RAM use minimal.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	// WAL lets the Next.js dashboard read while the daemon writes.
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode = WAL").Scan(&mode); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}
	for _, p := range []string{
		"PRAGMA synchronous = NORMAL", // safe with WAL, far fewer fsyncs
		"PRAGMA busy_timeout = 5000",
		"PRAGMA cache_size = -2000", // ~2 MB page cache
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (st *Store) Insert(s *session) (int64, error) {
	res, err := st.db.Exec(
		`INSERT INTO sessions (start_ts, end_ts, app, title, url, category)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		s.start, s.end, s.app, s.title, s.url, s.category,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (st *Store) Update(s *session) error {
	_, err := st.db.Exec(
		`UPDATE sessions SET end_ts = ?, title = ?, category = ? WHERE id = ?`,
		s.end, s.title, s.category, s.id,
	)
	return err
}

func (st *Store) Delete(id int64) error {
	_, err := st.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// sessionRow is a session as read back for analysis.
type sessionRow struct {
	Start, End      int64
	App, Title, URL string
	Category        string
}

// SessionsBetween returns sessions overlapping [from, to),
// ordered by start time. Rows are fully read before returning:
// the store has a single connection, so callers must never run
// another query while iterating.
func (st *Store) SessionsBetween(from, to int64) ([]sessionRow, error) {
	rows, err := st.db.Query(
		`SELECT start_ts, end_ts, app, title, url, category FROM sessions
		 WHERE end_ts > ? AND start_ts < ? ORDER BY start_ts`,
		from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sessionRow
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.Start, &s.End, &s.App, &s.Title, &s.URL, &s.Category); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// activityTotal is total time per distinct (app, title, url).
type activityTotal struct {
	App, Title, URL string
	Secs            int64
}

func (st *Store) ActivityTotals(since int64) ([]activityTotal, error) {
	rows, err := st.db.Query(
		`SELECT app, title, url, SUM(end_ts - start_ts) FROM sessions
		 WHERE start_ts >= ? GROUP BY app, title, url`,
		since,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []activityTotal
	for rows.Next() {
		var a activityTotal
		var secs sql.NullInt64
		if err := rows.Scan(&a.App, &a.Title, &a.URL, &secs); err != nil {
			return nil, err
		}
		a.Secs = secs.Int64
		out = append(out, a)
	}
	return out, rows.Err()
}

func (st *Store) Labels() (map[string]labelInfo, error) {
	rows, err := st.db.Query(`SELECT key, kind, category, subject, topic FROM ai_labels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]labelInfo)
	for rows.Next() {
		var key string
		var l labelInfo
		if err := rows.Scan(&key, &l.Kind, &l.Category, &l.Subject, &l.Topic); err != nil {
			return nil, err
		}
		out[key] = l
	}
	return out, rows.Err()
}

func (st *Store) SaveLabel(key string, l labelInfo, model string, now int64) error {
	_, err := st.db.Exec(
		`INSERT INTO ai_labels (key, kind, category, subject, topic, model, created_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   kind = excluded.kind, category = excluded.category,
		   subject = excluded.subject, topic = excluded.topic,
		   model = excluded.model, created_ts = excluded.created_ts`,
		key, l.Kind, l.Category, l.Subject, l.Topic, model, now,
	)
	return err
}

func (st *Store) ClearLabels() error {
	_, err := st.db.Exec(`DELETE FROM ai_labels`)
	return err
}

func (st *Store) Close() error {
	var a, b, c int
	_ = st.db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&a, &b, &c)
	return st.db.Close()
}
