package quota

import (
	"net/http"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
)

type Service struct {
	store *state.Store
}

func NewService(store *state.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Quotas() (map[string]ProviderResponse, error) {
	settings, err := s.store.GetQuotaSettings()
	if err != nil {
		return nil, err
	}

	res := make(map[string]ProviderResponse)
	for name, provider := range registry {
		if checker, ok := provider.(interface{ IsInstalled() bool }); ok {
			if !checker.IsInstalled() {
				continue
			}
		}

		config, ok := settings[name]
		if !ok {
			if name == "antigravity" || name == "claude" || name == "codex" || name == "copilot" {
				config = make(map[string]string)
			} else {
				continue
			}
		}

		data, err := provider.GetQuotas(config)
		if err != nil {
			res[name] = ProviderResponse{Error: err.Error()}
		} else {
			res[name] = ProviderResponse{Data: data}
		}
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

func (s *Service) SaveSettings(req map[string]map[string]string) error {
	return s.store.SaveQuotaSettings(req)
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
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if err := h.svc.SaveSettings(req); err != nil {
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
	mux.HandleFunc("POST /quotas/copilot/device-codes", h.DeviceCode)
	mux.HandleFunc("GET /quotas/copilot/device-codes/{device_code}", h.PollToken)
}