package quota

import (
	"net/http"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/prefs"
	"github.com/04mg/caw/internal/state"
)

// perProviderTimeout bounds how long a single quota provider may block the
// /quotas response. A slow or hung provider (e.g. antigravity spawning an
// agy instance that never binds a port) returns an error instead of
// stalling the entire API call.
const perProviderTimeout = 30 * time.Second

type Service struct {
	store *state.Store
}

func NewService(store *state.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Quotas() (map[string]ProviderResponse, error) {
	accountsByProvider, err := s.store.GetQuotaAccounts()
	if err != nil {
		return nil, err
	}

	disabled := prefs.GetPrefs(s.store).DisabledProviders
	res := make(map[string]ProviderResponse)
	type result struct {
		provider string
		account  string
		resp     ProviderResponse
	}

	var wg sync.WaitGroup
	accountCount := 0
	providerAccounts := make(map[string][]state.QuotaAccount, len(registry))
	for name := range registry {
		accountCount += len(resolveQuotaAccounts(name, accountsByProvider[name]))
	}
	results := make(chan result, max(1, accountCount))

	for name, provider := range registry {
		if slices.Contains(disabled, name) {
			continue
		}
		if checker, ok := provider.(interface{ IsInstalled() bool }); ok {
			if !checker.IsInstalled() {
				continue
			}
		}

		accounts := resolveQuotaAccounts(name, accountsByProvider[name])
		if len(accounts) == 0 {
			continue
		}
		providerAccounts[name] = accounts
		for _, account := range accounts {
			wg.Add(1)
			go func(name string, provider QuotaProvider, account state.QuotaAccount) {
				defer wg.Done()
				results <- result{
					provider: name,
					account:  account.Name,
					resp:     fetchQuotaWithTimeout(provider, account.Config),
				}
			}(name, provider, account)
		}
	}

	wg.Wait()
	close(results)
	byProvider := make(map[string]map[string]ProviderResponse, len(providerAccounts))
	for r := range results {
		if byProvider[r.provider] == nil {
			byProvider[r.provider] = make(map[string]ProviderResponse)
		}
		byProvider[r.provider][r.account] = r.resp
	}
	for provider, accounts := range providerAccounts {
		accountResponses := make([]NamedProviderResponse, 0, len(accounts))
		for _, account := range accounts {
			resp := byProvider[provider][account.Name]
			accountResponses = append(accountResponses, NamedProviderResponse{
				ID:    account.ID,
				Name:  account.Name,
				Data:  resp.Data,
				Error: resp.Error,
			})
		}
		entry := ProviderResponse{Accounts: accountResponses}
		if legacyAccount, ok := selectLegacyProviderAccount(accounts); ok {
			legacyResp := byProvider[provider][legacyAccount.Name]
			entry.Data = legacyResp.Data
			entry.Error = legacyResp.Error
		}
		res[provider] = entry
	}
	return res, nil
}

func (s *Service) Settings() (map[string]map[string]string, error) {
	settings, err := s.store.GetQuotaSettings()
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = make(map[string]map[string]string)
	}
	for name, provider := range registry {
		if checker, ok := provider.(interface{ IsInstalled() bool }); ok {
			if settings[name] == nil {
				settings[name] = make(map[string]string)
			}
			if checker.IsInstalled() {
				settings[name]["installed"] = "true"
			} else {
				settings[name]["installed"] = "false"
			}
		}
	}
	return settings, nil
}

func (s *Service) AccountSettings() (map[string][]state.QuotaAccount, error) {
	accounts, err := s.store.GetQuotaAccounts()
	if err != nil {
		return nil, err
	}
	if accounts == nil {
		accounts = make(map[string][]state.QuotaAccount)
	}
	return accounts, nil
}

func (s *Service) SaveSettings(req map[string]map[string]string) error {
	return s.store.SaveQuotaSettings(req)
}

func (s *Service) SaveAccountSettings(req map[string][]state.QuotaAccount) error {
	return s.store.SaveQuotaAccounts(req)
}

func (s *Service) InitiateDeviceLogin() (any, error) {
	return initiateDeviceLogin()
}

func (s *Service) PollDeviceToken(deviceCode string) (any, error) {
	return pollDeviceToken(deviceCode)
}

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Quotas(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Quotas()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, res)
}

func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.Settings()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, settings)
}

func (h *Handler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]map[string]string
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SaveSettings(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, map[string]bool{"ok": true})
}

func (h *Handler) AccountSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.svc.AccountSettings()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, settings)
}

func (h *Handler) SaveAccountSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string][]state.QuotaAccount
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SaveAccountSettings(req); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, map[string]bool{"ok": true})
}

func (h *Handler) DeviceCode(w http.ResponseWriter, r *http.Request) {
	dc, err := h.svc.InitiateDeviceLogin()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, dc)
}

func (h *Handler) PollToken(w http.ResponseWriter, r *http.Request) {
	deviceCode := r.PathValue("device_code")
	if deviceCode == "" {
		httpx.RespondBadRequest(w, "device_code required")
		return
	}
	tr, err := h.svc.PollDeviceToken(deviceCode)
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, tr)
}

func Register(mux *http.ServeMux, store *state.Store) {
	h := NewHandler(NewService(store))
	mux.HandleFunc("GET /quotas", h.Quotas)
	mux.HandleFunc("GET /quotas/settings", h.Settings)
	mux.HandleFunc("PUT /quotas/settings", h.SaveSettings)
	mux.HandleFunc("GET /quotas/settings/accounts", h.AccountSettings)
	mux.HandleFunc("PUT /quotas/settings/accounts", h.SaveAccountSettings)
	mux.HandleFunc("POST /quotas/copilot/device-codes", h.DeviceCode)
	mux.HandleFunc("GET /quotas/copilot/device-codes/{device_code}", h.PollToken)
}

func resolveQuotaAccounts(provider string, accounts []state.QuotaAccount) []state.QuotaAccount {
	if len(accounts) == 0 {
		if allowsEmptyQuotaConfig(provider) {
			return []state.QuotaAccount{{Name: state.DefaultQuotaAccountName, Config: map[string]string{}}}
		}
		return nil
	}
	resolved := make([]state.QuotaAccount, len(accounts))
	copy(resolved, accounts)
	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Name < resolved[j].Name })
	return resolved
}

func selectLegacyProviderAccount(accounts []state.QuotaAccount) (state.QuotaAccount, bool) {
	if len(accounts) == 0 {
		return state.QuotaAccount{}, false
	}
	for _, account := range accounts {
		if account.Name == state.DefaultQuotaAccountName {
			return account, true
		}
	}
	return accounts[0], true
}

func allowsEmptyQuotaConfig(provider string) bool {
	switch provider {
	case "antigravity", "claude", "codex", "copilot":
		return true
	default:
		return false
	}
}

func fetchQuotaWithTimeout(provider QuotaProvider, config map[string]string) ProviderResponse {
	type outcome struct {
		data *QuotaResponse
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		data, err := provider.GetQuotas(config)
		done <- outcome{data: data, err: err}
	}()

	select {
	case out := <-done:
		if out.err != nil {
			return ProviderResponse{Error: out.err.Error()}
		}
		return ProviderResponse{Data: out.data}
	case <-time.After(perProviderTimeout):
		return ProviderResponse{Error: "provider timed out"}
	}
}
