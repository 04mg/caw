package quota

import (
	"github.com/gin-gonic/gin"

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

func (h *Handler) Quotas(c *gin.Context) {
	res, err := h.svc.Quotas()
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, res)
}

func (h *Handler) Settings(c *gin.Context) {
	settings, err := h.svc.Settings()
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, settings)
}

func (h *Handler) SaveSettings(c *gin.Context) {
	var req map[string]map[string]string
	if !httpx.Bind(c, &req) {
		return
	}
	if err := h.svc.SaveSettings(req); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, map[string]bool{"ok": true})
}

func (h *Handler) DeviceCode(c *gin.Context) {
	dc, err := h.svc.InitiateDeviceLogin()
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, dc)
}

func (h *Handler) PollToken(c *gin.Context) {
	deviceCode := c.Param("device_code")
	if deviceCode == "" {
		httpx.BadRequest(c, "device_code required")
		return
	}
	tr, err := h.svc.PollDeviceToken(deviceCode)
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, tr)
}

func Register(rg *gin.RouterGroup, store *state.Store) {
	h := NewHandler(NewService(store))
	rg.GET("/quotas", h.Quotas)
	rg.GET("/quotas/settings", h.Settings)
	rg.PUT("/quotas/settings", h.SaveSettings)
	rg.POST("/quotas/copilot/device-codes", h.DeviceCode)
	rg.GET("/quotas/copilot/device-codes/:device_code", h.PollToken)
}