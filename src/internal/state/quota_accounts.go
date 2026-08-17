package state

import (
	"database/sql"
	"sort"
	"strings"
)

const DefaultQuotaAccountName = "default"

type QuotaAccount struct {
	ID     string            `json:"id,omitempty"`
	Name   string            `json:"name"`
	Config map[string]string `json:"config,omitempty"`
}

func (s *Store) migrateQuotaAccounts() {
	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM quota_accounts").Scan(&count); err != nil {
		return
	}
	if count > 0 {
		_ = tx.Commit()
		return
	}

	if _, err := tx.Exec(
		"INSERT INTO quota_accounts (provider, name) SELECT DISTINCT provider, ? FROM quota_settings",
		DefaultQuotaAccountName,
	); err != nil {
		return
	}
	if _, err := tx.Exec(
		`INSERT INTO quota_account_settings (provider, account_name, key, value)
		 SELECT provider, ?, key, value FROM quota_settings`,
		DefaultQuotaAccountName,
	); err != nil {
		return
	}

	_ = tx.Commit()
}

func (s *Store) GetQuotaAccounts() (map[string][]QuotaAccount, error) {
	s.Mu.RLock()
	defer s.Mu.RUnlock()
	return loadQuotaAccounts(s.db)
}

func (s *Store) GetQuotaSettings() (map[string]map[string]string, error) {
	accounts, err := s.GetQuotaAccounts()
	if err != nil {
		return nil, err
	}

	settings := make(map[string]map[string]string, len(accounts))
	for provider, providerAccounts := range accounts {
		account, ok := selectLegacyQuotaAccount(providerAccounts)
		if !ok {
			continue
		}
		settings[provider] = cloneQuotaConfig(account.Config)
	}
	return settings, nil
}

func (s *Store) SaveQuotaSettings(settings map[string]map[string]string) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for provider, config := range settings {
		clean := normalizeQuotaConfig(config)
		if _, err := tx.Exec(
			"DELETE FROM quota_account_settings WHERE provider = ? AND account_name = ?",
			provider,
			DefaultQuotaAccountName,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(
			"DELETE FROM quota_accounts WHERE provider = ? AND name = ?",
			provider,
			DefaultQuotaAccountName,
		); err != nil {
			return err
		}
		if len(clean) > 0 {
			if err := insertQuotaAccount(tx, provider, QuotaAccount{Name: DefaultQuotaAccountName, Config: clean}); err != nil {
				return err
			}
		}
		if err := syncLegacyQuotaSettingsForProvider(tx, provider, clean); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) SaveQuotaAccounts(accounts map[string][]QuotaAccount) error {
	s.Mu.Lock()
	defer s.Mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM quota_account_settings"); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM quota_accounts"); err != nil {
		return err
	}

	normalized := make(map[string][]QuotaAccount, len(accounts))
	for provider, providerAccounts := range accounts {
		canonical := canonicalizeQuotaAccounts(providerAccounts)
		normalized[provider] = canonical
		for _, account := range canonical {
			if err := insertQuotaAccount(tx, provider, account); err != nil {
				return err
			}
		}
	}

	if err := syncLegacyQuotaSettings(tx, normalized); err != nil {
		return err
	}

	return tx.Commit()
}

func loadQuotaAccounts(q interface {
	Query(query string, args ...any) (*sql.Rows, error)
}) (map[string][]QuotaAccount, error) {
	rows, err := q.Query(
		`SELECT a.provider, a.name, s.key, s.value
		 FROM quota_accounts a
		 LEFT JOIN quota_account_settings s
		   ON s.provider = a.provider AND s.account_name = a.name
		 ORDER BY a.provider, a.name, s.key`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	grouped := make(map[string]map[string]map[string]string)
	for rows.Next() {
		var provider, name string
		var key, value sql.NullString
		if err := rows.Scan(&provider, &name, &key, &value); err != nil {
			return nil, err
		}
		if grouped[provider] == nil {
			grouped[provider] = make(map[string]map[string]string)
		}
		if grouped[provider][name] == nil {
			grouped[provider][name] = make(map[string]string)
		}
		if key.Valid && value.Valid {
			grouped[provider][name][key.String] = value.String
		}
	}

	res := make(map[string][]QuotaAccount, len(grouped))
	for provider, accounts := range grouped {
		names := make([]string, 0, len(accounts))
		for name := range accounts {
			names = append(names, name)
		}
		sort.Strings(names)
		providerAccounts := make([]QuotaAccount, 0, len(names))
		for _, name := range names {
			providerAccounts = append(providerAccounts, QuotaAccount{
				ID:     name,
				Name:   name,
				Config: cloneQuotaConfig(accounts[name]),
			})
		}
		res[provider] = providerAccounts
	}

	return res, rows.Err()
}

func canonicalizeQuotaAccounts(accounts []QuotaAccount) []QuotaAccount {
	merged := make(map[string]map[string]string)
	for _, account := range accounts {
		name := strings.TrimSpace(account.Name)
		if name == "" {
			name = DefaultQuotaAccountName
		}
		merged[name] = normalizeQuotaConfig(account.Config)
	}

	names := make([]string, 0, len(merged))
	for name := range merged {
		names = append(names, name)
	}
	sort.Strings(names)

	res := make([]QuotaAccount, 0, len(names))
	for _, name := range names {
		res = append(res, QuotaAccount{
			ID:     name,
			Name:   name,
			Config: cloneQuotaConfig(merged[name]),
		})
	}
	return res
}

func selectLegacyQuotaAccount(accounts []QuotaAccount) (QuotaAccount, bool) {
	if len(accounts) == 0 {
		return QuotaAccount{}, false
	}
	for _, account := range accounts {
		if account.Name == DefaultQuotaAccountName {
			return QuotaAccount{Name: account.Name, Config: cloneQuotaConfig(account.Config)}, true
		}
	}
	account := accounts[0]
	return QuotaAccount{Name: account.Name, Config: cloneQuotaConfig(account.Config)}, true
}

func insertQuotaAccount(tx *sql.Tx, provider string, account QuotaAccount) error {
	if _, err := tx.Exec(
		"INSERT INTO quota_accounts (provider, name) VALUES (?, ?)",
		provider,
		account.Name,
	); err != nil {
		return err
	}
	for key, value := range normalizeQuotaConfig(account.Config) {
		if _, err := tx.Exec(
			"INSERT INTO quota_account_settings (provider, account_name, key, value) VALUES (?, ?, ?, ?)",
			provider,
			account.Name,
			key,
			value,
		); err != nil {
			return err
		}
	}
	return nil
}

func syncLegacyQuotaSettings(tx *sql.Tx, accounts map[string][]QuotaAccount) error {
	if _, err := tx.Exec("DELETE FROM quota_settings"); err != nil {
		return err
	}
	for provider, providerAccounts := range accounts {
		account, ok := selectLegacyQuotaAccount(providerAccounts)
		if !ok {
			continue
		}
		if err := syncLegacyQuotaSettingsForProvider(tx, provider, account.Config); err != nil {
			return err
		}
	}
	return nil
}

func syncLegacyQuotaSettingsForProvider(tx *sql.Tx, provider string, config map[string]string) error {
	if _, err := tx.Exec("DELETE FROM quota_settings WHERE provider = ?", provider); err != nil {
		return err
	}
	for key, value := range normalizeQuotaConfig(config) {
		if _, err := tx.Exec(
			"INSERT INTO quota_settings (provider, key, value) VALUES (?, ?, ?)",
			provider,
			key,
			value,
		); err != nil {
			return err
		}
	}
	return nil
}

func normalizeQuotaConfig(config map[string]string) map[string]string {
	if len(config) == 0 {
		return map[string]string{}
	}
	clean := make(map[string]string, len(config))
	for key, value := range config {
		if key == "" || key == "installed" || value == "" {
			continue
		}
		clean[key] = value
	}
	return clean
}

func cloneQuotaConfig(config map[string]string) map[string]string {
	if len(config) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(config))
	for key, value := range config {
		cloned[key] = value
	}
	return cloned
}
