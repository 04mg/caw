package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestStoreMigratesLegacyQuotaSettings(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".quota-state-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE quota_settings (
			provider TEXT NOT NULL,
			key      TEXT NOT NULL,
			value    TEXT NOT NULL,
			PRIMARY KEY (provider, key)
		);
		INSERT INTO quota_settings (provider, key, value) VALUES
			('copilot', 'token', 'legacy-token'),
			('copilot', 'enterpriseHost', 'github.example');
	`); err != nil {
		t.Fatalf("seed legacy quota_settings: %v", err)
	}
	_ = db.Close()

	store := NewStore(dbPath)
	defer store.Close()

	accounts, err := store.GetQuotaAccounts()
	if err != nil {
		t.Fatalf("GetQuotaAccounts: %v", err)
	}
	got := accounts["copilot"]
	if len(got) != 1 {
		t.Fatalf("got %d copilot accounts, want 1", len(got))
	}
	if got[0].Name != DefaultQuotaAccountName {
		t.Fatalf("account name = %q, want %q", got[0].Name, DefaultQuotaAccountName)
	}
	if got[0].Config["token"] != "legacy-token" {
		t.Fatalf("token = %q, want legacy-token", got[0].Config["token"])
	}
	if got[0].Config["enterpriseHost"] != "github.example" {
		t.Fatalf("enterpriseHost = %q, want github.example", got[0].Config["enterpriseHost"])
	}
}

func TestSaveQuotaSettingsPreservesNamedAccounts(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".quota-state-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	store := NewStore(filepath.Join(dir, "state.db"))
	defer store.Close()

	if err := store.SaveQuotaAccounts(map[string][]QuotaAccount{
		"copilot": {
			{Name: DefaultQuotaAccountName, Config: map[string]string{"token": "default-token"}},
			{Name: "work", Config: map[string]string{"token": "work-token"}},
		},
	}); err != nil {
		t.Fatalf("SaveQuotaAccounts: %v", err)
	}

	if err := store.SaveQuotaSettings(map[string]map[string]string{
		"copilot": {"token": "updated-default"},
	}); err != nil {
		t.Fatalf("SaveQuotaSettings: %v", err)
	}

	accounts, err := store.GetQuotaAccounts()
	if err != nil {
		t.Fatalf("GetQuotaAccounts: %v", err)
	}
	got := accounts["copilot"]
	if len(got) != 2 {
		t.Fatalf("got %d copilot accounts, want 2", len(got))
	}

	found := map[string]QuotaAccount{}
	for _, account := range got {
		found[account.Name] = account
	}
	if found[DefaultQuotaAccountName].Config["token"] != "updated-default" {
		t.Fatalf("default token = %q, want updated-default", found[DefaultQuotaAccountName].Config["token"])
	}
	if found["work"].Config["token"] != "work-token" {
		t.Fatalf("work token = %q, want work-token", found["work"].Config["token"])
	}

	settings, err := store.GetQuotaSettings()
	if err != nil {
		t.Fatalf("GetQuotaSettings: %v", err)
	}
	if settings["copilot"]["token"] != "updated-default" {
		t.Fatalf("legacy token = %q, want updated-default", settings["copilot"]["token"])
	}
}
