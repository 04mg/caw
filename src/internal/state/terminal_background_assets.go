package state

import "database/sql"

type TerminalBackgroundAsset struct {
	ID          string
	Filename    string
	MediaKind   string
	ContentType string
	FileExt     string
	SizeBytes   int64
	CreatedAt   string
}

func (s *Store) PutTerminalBackgroundAsset(asset TerminalBackgroundAsset) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	_, err := s.db.Exec(
		`INSERT INTO terminal_background_assets
		   (id, filename, media_kind, content_type, file_ext, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   filename = excluded.filename,
		   media_kind = excluded.media_kind,
		   content_type = excluded.content_type,
		   file_ext = excluded.file_ext,
		   size_bytes = excluded.size_bytes,
		   created_at = excluded.created_at`,
		asset.ID,
		asset.Filename,
		asset.MediaKind,
		asset.ContentType,
		asset.FileExt,
		asset.SizeBytes,
		asset.CreatedAt,
	)
	return err
}

func (s *Store) ListTerminalBackgroundAssets() ([]TerminalBackgroundAsset, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	rows, err := s.db.Query(
		`SELECT id, filename, media_kind, content_type, file_ext, size_bytes, created_at
		   FROM terminal_background_assets
		  ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := make([]TerminalBackgroundAsset, 0)
	for rows.Next() {
		var asset TerminalBackgroundAsset
		if err := rows.Scan(
			&asset.ID,
			&asset.Filename,
			&asset.MediaKind,
			&asset.ContentType,
			&asset.FileExt,
			&asset.SizeBytes,
			&asset.CreatedAt,
		); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return assets, nil
}

func (s *Store) GetTerminalBackgroundAsset(id string) (TerminalBackgroundAsset, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	var asset TerminalBackgroundAsset
	err := s.db.QueryRow(
		`SELECT id, filename, media_kind, content_type, file_ext, size_bytes, created_at
		   FROM terminal_background_assets
		  WHERE id = ?`,
		id,
	).Scan(
		&asset.ID,
		&asset.Filename,
		&asset.MediaKind,
		&asset.ContentType,
		&asset.FileExt,
		&asset.SizeBytes,
		&asset.CreatedAt,
	)
	return asset, err
}

func (s *Store) DeleteTerminalBackgroundAsset(id string) (bool, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	res, err := s.db.Exec("DELETE FROM terminal_background_assets WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) HasTerminalBackgroundAsset(id string) (bool, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	var v int
	err := s.db.QueryRow("SELECT 1 FROM terminal_background_assets WHERE id = ?", id).Scan(&v)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return v == 1, nil
}
