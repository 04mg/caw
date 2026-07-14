package state

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

type Store struct {
	Mu sync.RWMutex
	db *sql.DB
}

func NewStore(dbPath string) *Store {
	_ = os.MkdirAll(filepath.Dir(dbPath), 0o755)
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	s := &Store{db: db}
	s.migrate()
	return s
}

func (s *Store) migrate() {
	const schema = `
	CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE IF NOT EXISTS workspaces (
		id                TEXT PRIMARY KEY,
		path              TEXT NOT NULL,
		name              TEXT NOT NULL,
		emoji             TEXT DEFAULT '',
		active_tab_index  INTEGER DEFAULT 0,
		active_pane_id    TEXT DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS tab_layouts (
		id            TEXT PRIMARY KEY,
		workspace_id  TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
		name          TEXT NOT NULL,
		sort_order    INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS layout_nodes (
		id           TEXT PRIMARY KEY,
		tab_id       TEXT NOT NULL REFERENCES tab_layouts(id) ON DELETE CASCADE,
		parent_id    TEXT REFERENCES layout_nodes(id) ON DELETE CASCADE,
		sort_order   INTEGER NOT NULL DEFAULT 0,
		type         TEXT NOT NULL,
		cwd          TEXT DEFAULT '',
		cmd          TEXT DEFAULT '[]',
		agent_id     TEXT DEFAULT '',
		orientation  TEXT DEFAULT '',
		sizes        TEXT DEFAULT '[]',
		file_path    TEXT DEFAULT '',
		is_diff      INTEGER DEFAULT 0,
		agent_branch TEXT DEFAULT '',
		base_branch  TEXT DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS quota_settings (
		provider TEXT NOT NULL,
		key      TEXT NOT NULL,
		value    TEXT NOT NULL,
		PRIMARY KEY (provider, key)
	);
	CREATE TABLE IF NOT EXISTS agent_sessions (
		leaf_id    TEXT PRIMARY KEY,
		agent_id   TEXT NOT NULL DEFAULT '',
		cwd        TEXT NOT NULL DEFAULT '',
		started_at TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS push_subscriptions (
		endpoint   TEXT PRIMARY KEY,
		p256dh     TEXT NOT NULL,
		auth       TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := s.db.Exec(schema); err != nil {
		log.Fatalf("failed to create schema: %v", err)
	}
	_, _ = s.db.Exec("ALTER TABLE workspaces ADD COLUMN enable_worktrees INTEGER DEFAULT 1")
	_, _ = s.db.Exec("ALTER TABLE layout_nodes ADD COLUMN agent_branch TEXT DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE layout_nodes ADD COLUMN base_branch TEXT DEFAULT ''")
	_, _ = s.db.Exec("ALTER TABLE workspaces ADD COLUMN tab_groups_json TEXT DEFAULT ''")
}

func (s *Store) Get() AppState {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	as := AppState{Workspaces: []Workspace{}}

	// Load active workspace ID
	row := s.db.QueryRow("SELECT value FROM settings WHERE key = 'active_workspace_id'")
	var activeID string
	if err := row.Scan(&activeID); err == nil {
		as.ActiveWorkspaceID = activeID
	}

	// Load workspaces
	wRows, err := s.db.Query("SELECT id, path, name, emoji, active_tab_index, active_pane_id, enable_worktrees, COALESCE(tab_groups_json, '') FROM workspaces")
	if err != nil {
		return as
	}
	defer wRows.Close()

	for wRows.Next() {
		var w Workspace
		var enableWorktrees int
		if err := wRows.Scan(&w.ID, &w.Path, &w.Name, &w.Emoji, &w.ActiveTabIndex, &w.ActivePaneID, &enableWorktrees, &w.TabGroupsJSON); err != nil {
			continue
		}
		w.EnableWorktrees = enableWorktrees != 0
		w.Layouts = s.loadTabLayouts(w.ID)
		as.Workspaces = append(as.Workspaces, w)
	}
	return as
}

func (s *Store) loadTabLayouts(workspaceID string) []TabLayout {
	rows, err := s.db.Query("SELECT id, name, sort_order FROM tab_layouts WHERE workspace_id = ? ORDER BY sort_order", workspaceID)
	if err != nil {
		return []TabLayout{}
	}
	defer rows.Close()

	var layouts []TabLayout
	for rows.Next() {
		var tl TabLayout
		if err := rows.Scan(&tl.ID, &tl.Name, new(int)); err != nil {
			continue
		}
		tl.Layout = s.loadLayoutTree(tl.ID, "", true)
		layouts = append(layouts, tl)
	}
	if layouts == nil {
		layouts = []TabLayout{}
	}
	return layouts
}

func (s *Store) loadLayoutTree(tabID, nodeID string, isRoot bool) LayoutNode {
	var ln LayoutNode
	var cmdJSON, sizesJSON string
	var isDiff int

	var row *sql.Row
	if isRoot {
		row = s.db.QueryRow(
			"SELECT id, type, cwd, cmd, agent_id, orientation, sizes, file_path, is_diff, agent_branch, base_branch FROM layout_nodes WHERE tab_id = ? AND parent_id IS NULL",
			tabID,
		)
	} else {
		row = s.db.QueryRow(
			"SELECT id, type, cwd, cmd, agent_id, orientation, sizes, file_path, is_diff, agent_branch, base_branch FROM layout_nodes WHERE tab_id = ? AND id = ?",
			tabID, nodeID,
		)
	}

	err := row.Scan(&ln.ID, &ln.Type, &ln.Cwd, &cmdJSON, &ln.AgentID, &ln.Orientation, &sizesJSON, &ln.FilePath, &isDiff, &ln.AgentBranch, &ln.BaseBranch)
	if err != nil {
		ln.Type = "empty"
		return ln
	}
	ln.IsDiff = isDiff != 0
	_ = json.Unmarshal([]byte(cmdJSON), &ln.Cmd)
	_ = json.Unmarshal([]byte(sizesJSON), &ln.Sizes)

	// Load children
	childRows, err := s.db.Query(
		"SELECT id FROM layout_nodes WHERE tab_id = ? AND parent_id = ? ORDER BY sort_order",
		tabID, ln.ID,
	)
	if err != nil {
		return ln
	}
	defer childRows.Close()

	for childRows.Next() {
		var childID string
		if err := childRows.Scan(&childID); err != nil {
			continue
		}
		child := s.loadLayoutTree(tabID, childID, false)
		ln.Children = append(ln.Children, child)
	}
	if ln.Children == nil {
		ln.Children = []LayoutNode{}
	}
	return ln
}

func (s *Store) Set(as AppState) {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	tx.Exec("DELETE FROM layout_nodes")
	tx.Exec("DELETE FROM tab_layouts")
	tx.Exec("DELETE FROM workspaces")

	// Preserve push-related settings (VAPID keys, push prefs) that must
	// survive workspace state saves. Store.Set() does DELETE FROM settings,
	// which would wipe them otherwise.
	var preservedSettings [][2]string
	for _, key := range []string{
		"vapid_public_key", "vapid_private_key",
		"push_enabled", "push_needs_input", "push_finished",
	} {
		var val string
		if err := tx.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&val); err == nil {
			preservedSettings = append(preservedSettings, [2]string{key, val})
		}
	}

	tx.Exec("DELETE FROM settings")

	if as.Workspaces == nil {
		as.Workspaces = []Workspace{}
	}

	// Save active workspace ID
	tx.Exec("INSERT INTO settings (key, value) VALUES ('active_workspace_id', ?)", as.ActiveWorkspaceID)

	// Restore push-related settings that were preserved across the DELETE.
	for _, kv := range preservedSettings {
		tx.Exec("INSERT INTO settings (key, value) VALUES (?, ?)", kv[0], kv[1])
	}

	for _, w := range as.Workspaces {
		if w.Layouts == nil {
			w.Layouts = []TabLayout{}
		}
		enableWT := 0
		if w.EnableWorktrees {
			enableWT = 1
		}
		tx.Exec(
			"INSERT INTO workspaces (id, path, name, emoji, active_tab_index, active_pane_id, enable_worktrees, tab_groups_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			w.ID, w.Path, w.Name, w.Emoji, w.ActiveTabIndex, w.ActivePaneID, enableWT, w.TabGroupsJSON,
		)
		for i, tl := range w.Layouts {
			tx.Exec(
				"INSERT INTO tab_layouts (id, workspace_id, name, sort_order) VALUES (?, ?, ?, ?)",
				tl.ID, w.ID, tl.Name, i,
			)
			s.saveLayoutTree(tx, tl.ID, "", tl.Layout, 0)
		}
	}

	tx.Commit()
}

func (s *Store) saveLayoutTree(tx *sql.Tx, tabID, parentID string, ln LayoutNode, order int) {
	cmdJSON, _ := json.Marshal(ln.Cmd)
	sizesJSON, _ := json.Marshal(ln.Sizes)
	isDiff := 0
	if ln.IsDiff {
		isDiff = 1
	}
	var parentPtr *string
	if parentID != "" {
		parentPtr = &parentID
	}

	tx.Exec(
		`INSERT INTO layout_nodes (id, tab_id, parent_id, sort_order, type, cwd, cmd, agent_id, orientation, sizes, file_path, is_diff, agent_branch, base_branch)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ln.ID, tabID, parentPtr, order, ln.Type, ln.Cwd, string(cmdJSON), ln.AgentID, ln.Orientation, string(sizesJSON), ln.FilePath, isDiff, ln.AgentBranch, ln.BaseBranch,
	)

	if ln.Children == nil {
		ln.Children = []LayoutNode{}
	}
	for i, child := range ln.Children {
		s.saveLayoutTree(tx, tabID, ln.ID, child, i)
	}
}

func (s *Store) GetQuotaSettings() (map[string]map[string]string, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()

	rows, err := s.db.Query("SELECT provider, key, value FROM quota_settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := make(map[string]map[string]string)
	for rows.Next() {
		var provider, key, val string
		if err := rows.Scan(&provider, &key, &val); err != nil {
			return nil, err
		}
		if _, ok := res[provider]; !ok {
			res[provider] = make(map[string]string)
		}
		res[provider][key] = val
	}
	return res, nil
}

func (s *Store) SaveQuotaSettings(settings map[string]map[string]string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM quota_settings"); err != nil {
		return err
	}

	for provider, kv := range settings {
		for key, val := range kv {
			if val == "" {
				continue
			}
			_, err := tx.Exec("INSERT INTO quota_settings (provider, key, value) VALUES (?, ?, ?)", provider, key, val)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (s *Store) Close() {
	s.db.Close()
}

func DefaultDBPath() string {
	if p := os.Getenv("CAW_DB_PATH"); p != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home, err = os.UserConfigDir()
		if err != nil {
			home, _ = os.Getwd()
		}
	}
	p := filepath.Join(home, ".caw", "caw.db")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return p
}

func DefaultStatePath() string {
	if p := os.Getenv("CAW_STATE_PATH"); p != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home, err = os.UserConfigDir()
		if err != nil {
			home, _ = os.Getwd()
		}
	}
	p := filepath.Join(home, ".caw", "state.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	return p
}
