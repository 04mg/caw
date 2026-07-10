package state

import "database/sql"

// PushSubscription represents a browser push subscription stored in the DB.
type PushSubscription struct {
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt string
}

// AddPushSubscription inserts or replaces a push subscription.
func (s *Store) AddPushSubscription(endpoint, p256dh, auth string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO push_subscriptions (endpoint, p256dh, auth)
		 VALUES (?, ?, ?)
		 ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth`,
		endpoint, p256dh, auth,
	)
	return err
}

// RemovePushSubscription deletes a push subscription by endpoint.
func (s *Store) RemovePushSubscription(endpoint string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, err := s.db.Exec("DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// GetPushSubscriptions returns all stored push subscriptions.
func (s *Store) GetPushSubscriptions() ([]PushSubscription, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	rows, err := s.db.Query("SELECT endpoint, p256dh, auth, created_at FROM push_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, nil
}

// PushPrefs holds the user's push notification preferences.
type PushPrefs struct {
	Enabled     bool `json:"enabled"`
	NeedsInput  bool `json:"needsInput"`
	Finished    bool `json:"finished"`
}

// GetPushPrefs reads push preferences from the settings KV table.
func (s *Store) GetPushPrefs() (PushPrefs, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	prefs := PushPrefs{Enabled: false, NeedsInput: true, Finished: true}
	rows, err := s.db.Query(
		`SELECT key, value FROM settings WHERE key IN ('push_enabled', 'push_needs_input', 'push_finished')`,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return prefs, nil
		}
		return prefs, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			continue
		}
		switch key {
		case "push_enabled":
			prefs.Enabled = val == "1"
		case "push_needs_input":
			prefs.NeedsInput = val == "1"
		case "push_finished":
			prefs.Finished = val == "1"
		}
	}
	return prefs, nil
}

// SavePushPrefs persists push preferences to the settings KV table.
func (s *Store) SavePushPrefs(prefs PushPrefs) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	enabled := "0"
	if prefs.Enabled {
		enabled = "1"
	}
	needsInput := "0"
	if prefs.NeedsInput {
		needsInput = "1"
	}
	finished := "0"
	if prefs.Finished {
		finished = "1"
	}

	for _, kv := range [][2]string{
		{"push_enabled", enabled},
		{"push_needs_input", needsInput},
		{"push_finished", finished},
	} {
		if _, err := tx.Exec(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			kv[0], kv[1],
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetSetting retrieves a single value from the settings KV table.
func (s *Store) GetSetting(key string) (string, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	var val string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val)
	return val, err
}

// SetSetting inserts or updates a single key/value in the settings KV table.
func (s *Store) SetSetting(key, val string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, val,
	)
	return err
}