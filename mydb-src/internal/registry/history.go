package registry

// Query history: every execution is appended fire-and-forget and pruned
// to a global retention limit.

// HistoryEntry is one recorded execution.
type HistoryEntry struct {
	ID           int64
	ConnectionID int64
	ConnName     string // joined for display; "" if the connection is gone
	SQL          string
	StartedAt    string
	DurationMs   int64
	Rows         int64
	OK           bool
	Error        string
}

// AddHistory records an execution and prunes the table to limit rows
// (0 = keep everything).
func (r *Registry) AddHistory(e HistoryEntry, limit int) error {
	_, err := r.db.Exec(`
		INSERT INTO query_history (connection_id, sql, started_at, duration_ms, rows, ok, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ConnectionID, e.SQL, e.StartedAt, e.DurationMs, e.Rows, boolInt(e.OK), e.Error)
	if err != nil {
		return err
	}
	if limit > 0 {
		_, err = r.db.Exec(`
			DELETE FROM query_history
			WHERE id NOT IN (SELECT id FROM query_history ORDER BY id DESC LIMIT ?)`, limit)
	}
	return err
}

// History returns entries newest-first. connID scopes to one connection;
// 0 returns all.
func (r *Registry) History(connID int64, limit int) ([]HistoryEntry, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := r.db.Query(`
		SELECT h.id, COALESCE(h.connection_id, 0), COALESCE(c.name, ''),
		       h.sql, h.started_at, COALESCE(h.duration_ms, 0),
		       COALESCE(h.rows, 0), COALESCE(h.ok, 0), COALESCE(h.error, '')
		FROM query_history h
		LEFT JOIN connections c ON c.id = h.connection_id
		WHERE (?1 = 0 OR h.connection_id = ?1)
		ORDER BY h.id DESC
		LIMIT ?2`, connID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryEntry
	for rows.Next() {
		var e HistoryEntry
		var ok int
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.ConnName, &e.SQL, &e.StartedAt,
			&e.DurationMs, &e.Rows, &ok, &e.Error); err != nil {
			return nil, err
		}
		e.OK = ok != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
