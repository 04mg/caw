package state

// PushSubscription represents a browser push subscription stored in the DB.
type PushSubscription struct {
	Endpoint    string
	P256dh      string
	Auth        string
	CreatedAt   string
	DeviceID    string
	DeviceName  string
	Enabled     bool
	NeedsInput  bool
	Finished    bool
}

// AddPushSubscription inserts or replaces a push subscription.
func (s *Store) AddPushSubscription(endpoint, p256dh, auth, deviceID, deviceName string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO push_subscriptions (endpoint, p256dh, auth, device_id, device_name, prefs_enabled, prefs_needs_input, prefs_finished)
		 VALUES (?, ?, ?, ?, ?, 1, 1, 1)
		 ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, device_id = excluded.device_id, device_name = excluded.device_name`,
		endpoint, p256dh, auth, deviceID, deviceName,
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

// RemovePushSubscriptionByDeviceID deletes a push subscription by device_id.
func (s *Store) RemovePushSubscriptionByDeviceID(deviceID string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	_, err := s.db.Exec("DELETE FROM push_subscriptions WHERE device_id = ?", deviceID)
	return err
}

// GetPushSubscriptions returns all stored push subscriptions.
func (s *Store) GetPushSubscriptions() ([]PushSubscription, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	rows, err := s.db.Query("SELECT endpoint, p256dh, auth, created_at, COALESCE(device_id, ''), COALESCE(device_name, ''), COALESCE(prefs_enabled, 0), COALESCE(prefs_needs_input, 1), COALESCE(prefs_finished, 1) FROM push_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subs []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		var enabled, needsInput, finished int
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.CreatedAt, &sub.DeviceID, &sub.DeviceName, &enabled, &needsInput, &finished); err != nil {
			return nil, err
		}
		sub.Enabled = enabled != 0
		sub.NeedsInput = needsInput != 0
		sub.Finished = finished != 0
		subs = append(subs, sub)
	}
	return subs, nil
}

// UpdatePushSubscriptionPrefs updates the per-device push preferences for a subscription.
func (s *Store) UpdatePushSubscriptionPrefs(deviceID string, enabled, needsInput, finished *bool, deviceName *string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if deviceName != nil {
		if _, err := tx.Exec("UPDATE push_subscriptions SET device_name = ? WHERE device_id = ?", *deviceName, deviceID); err != nil {
			return err
		}
	}
	if enabled != nil {
		v := 0
		if *enabled {
			v = 1
		}
		if _, err := tx.Exec("UPDATE push_subscriptions SET prefs_enabled = ? WHERE device_id = ?", v, deviceID); err != nil {
			return err
		}
	}
	if needsInput != nil {
		v := 0
		if *needsInput {
			v = 1
		}
		if _, err := tx.Exec("UPDATE push_subscriptions SET prefs_needs_input = ? WHERE device_id = ?", v, deviceID); err != nil {
			return err
		}
	}
	if finished != nil {
		v := 0
		if *finished {
			v = 1
		}
		if _, err := tx.Exec("UPDATE push_subscriptions SET prefs_finished = ? WHERE device_id = ?", v, deviceID); err != nil {
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
