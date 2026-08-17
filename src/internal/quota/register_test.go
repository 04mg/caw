package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/04mg/caw/internal/state"
)

type quotaTestProvider struct{}

func (quotaTestProvider) GetQuotas(config map[string]string) (*QuotaResponse, error) {
	token := config["token"]
	if token == "" {
		return nil, fmt.Errorf("missing token")
	}
	return &QuotaResponse{
		FiveHour: Quota{
			Used:  float64(len(token)),
			Limit: 100,
			Unit:  "count",
		},
	}, nil
}

func TestServiceQuotasReturnsNamedAccounts(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".quota-service-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	store := state.NewStore(filepath.Join(dir, "state.db"))
	defer store.Close()

	if err := store.SaveQuotaAccounts(map[string][]state.QuotaAccount{
		"quota-test": {
			{Name: "work", Config: map[string]string{"token": "work-token"}},
			{Name: state.DefaultQuotaAccountName, Config: map[string]string{"token": "default-token"}},
		},
	}); err != nil {
		t.Fatalf("SaveQuotaAccounts: %v", err)
	}

	previous, hadPrevious := registry["quota-test"]
	registry["quota-test"] = quotaTestProvider{}
	defer func() {
		if hadPrevious {
			registry["quota-test"] = previous
		} else {
			delete(registry, "quota-test")
		}
	}()

	svc := NewService(store)
	got, err := svc.Quotas()
	if err != nil {
		t.Fatalf("Quotas: %v", err)
	}

	provider := got["quota-test"]
	if provider.Data == nil {
		t.Fatal("expected legacy provider data")
	}
	if provider.Data.FiveHour.Used != float64(len("default-token")) {
		t.Fatalf("legacy used = %v, want %d", provider.Data.FiveHour.Used, len("default-token"))
	}
	if len(provider.Accounts) != 2 {
		t.Fatalf("got %d account results, want 2", len(provider.Accounts))
	}
	if provider.Accounts[0].Name != state.DefaultQuotaAccountName {
		t.Fatalf("first account = %q, want %q", provider.Accounts[0].Name, state.DefaultQuotaAccountName)
	}
	if provider.Accounts[0].Data == nil || provider.Accounts[0].Data.FiveHour.Used != float64(len("default-token")) {
		t.Fatalf("default account data = %#v", provider.Accounts[0].Data)
	}
	if provider.Accounts[1].Name != "work" {
		t.Fatalf("second account = %q, want work", provider.Accounts[1].Name)
	}
	if provider.Accounts[1].Data == nil || provider.Accounts[1].Data.FiveHour.Used != float64(len("work-token")) {
		t.Fatalf("work account data = %#v", provider.Accounts[1].Data)
	}
}

func TestServiceAccountSettingsReturnsStoredAccounts(t *testing.T) {
	dir, err := os.MkdirTemp(".", ".quota-service-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	defer os.RemoveAll(dir)

	store := state.NewStore(filepath.Join(dir, "state.db"))
	defer store.Close()

	if err := store.SaveQuotaAccounts(map[string][]state.QuotaAccount{
		"copilot": {
			{Name: state.DefaultQuotaAccountName, Config: map[string]string{"token": "default-token"}},
			{Name: "personal", Config: map[string]string{"token": "personal-token"}},
		},
	}); err != nil {
		t.Fatalf("SaveQuotaAccounts: %v", err)
	}

	svc := NewService(store)
	accounts, err := svc.AccountSettings()
	if err != nil {
		t.Fatalf("AccountSettings: %v", err)
	}

	got := accounts["copilot"]
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2", len(got))
	}
	if got[0].Name != state.DefaultQuotaAccountName || got[1].Name != "personal" {
		t.Fatalf("unexpected accounts order/content: %#v", got)
	}
}
