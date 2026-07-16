package registry

// Job history: completed and failed jobs are logged here (the queue
// itself never persists across restarts).

type JobEntry struct {
	ID           int64
	ConnectionID int64
	ConnName     string
	Kind         string
	Target       string
	StartedAt    string
	FinishedAt   string
	OK           bool
	Detail       string
}

func (r *Registry) AddJob(e JobEntry) error {
	_, err := r.db.Exec(`
		INSERT INTO jobs_history (connection_id, kind, target, started_at, finished_at, ok, detail)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ConnectionID, e.Kind, e.Target, e.StartedAt, e.FinishedAt, boolInt(e.OK), e.Detail)
	return err
}

// Jobs returns job-history entries newest-first.
func (r *Registry) Jobs(limit int) ([]JobEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.Query(`
		SELECT j.id, COALESCE(j.connection_id, 0), COALESCE(c.name, ''),
		       COALESCE(j.kind, ''), COALESCE(j.target, ''),
		       COALESCE(j.started_at, ''), COALESCE(j.finished_at, ''),
		       COALESCE(j.ok, 0), COALESCE(j.detail, '')
		FROM jobs_history j
		LEFT JOIN connections c ON c.id = j.connection_id
		ORDER BY j.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobEntry
	for rows.Next() {
		var e JobEntry
		var ok int
		if err := rows.Scan(&e.ID, &e.ConnectionID, &e.ConnName, &e.Kind, &e.Target,
			&e.StartedAt, &e.FinishedAt, &ok, &e.Detail); err != nil {
			return nil, err
		}
		e.OK = ok != 0
		out = append(out, e)
	}
	return out, rows.Err()
}
