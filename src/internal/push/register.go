package push

import (
	"net/http"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
)

type Handler struct {
	store *state.Store
}

func Register(mux *http.ServeMux, store *state.Store) {
	h := &Handler{store: store}
	mux.HandleFunc("GET /push/vapid-public-key", h.GetVAPIDPublicKey)
	mux.HandleFunc("POST /push/subscribe", h.Subscribe)
	mux.HandleFunc("DELETE /push/subscribe", h.Unsubscribe)
	mux.HandleFunc("GET /push/devices", h.ListDevices)
	mux.HandleFunc("PUT /push/devices/{deviceId}", h.UpdateDevice)
	mux.HandleFunc("DELETE /push/devices/{deviceId}", h.RemoveDevice)
}

type vapidPublicKeyResponse struct {
	PublicKey string `json:"publicKey"`
}

func (h *Handler) GetVAPIDPublicKey(w http.ResponseWriter, r *http.Request) {
	httpx.RespondJSON(w, vapidPublicKeyResponse{PublicKey: PublicKey()})
}

type subscribeKeysReq struct {
	P256dh string `json:"p256dh" validate:"required"`
	Auth   string `json:"auth" validate:"required"`
}

type subscribeRequest struct {
	Endpoint   string           `json:"endpoint" validate:"required"`
	Keys       subscribeKeysReq `json:"keys" validate:"required"`
	DeviceID   string           `json:"deviceId"`
	DeviceName string           `json:"deviceName"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if err := h.store.AddPushSubscription(req.Endpoint, req.Keys.P256dh, req.Keys.Auth, req.DeviceID, req.DeviceName); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondCreated(w, okResponse{OK: true})
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint" validate:"required"`
}

func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	var req unsubscribeRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if err := h.store.RemovePushSubscription(req.Endpoint); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondNoContent(w)
}

type deviceInfo struct {
	DeviceID    string `json:"deviceId"`
	DeviceName  string `json:"deviceName"`
	Endpoint    string `json:"endpoint"`
	CreatedAt   string `json:"createdAt"`
	Enabled     bool   `json:"enabled"`
	NeedsInput  bool   `json:"needsInput"`
	Finished    bool   `json:"finished"`
}

func (h *Handler) ListDevices(w http.ResponseWriter, r *http.Request) {
	subs, err := h.store.GetPushSubscriptions()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	devices := make([]deviceInfo, 0, len(subs))
	for _, sub := range subs {
		devices = append(devices, deviceInfo{
			DeviceID:   sub.DeviceID,
			DeviceName: sub.DeviceName,
			Endpoint:   sub.Endpoint,
			CreatedAt:  sub.CreatedAt,
			Enabled:    sub.Enabled,
			NeedsInput: sub.NeedsInput,
			Finished:   sub.Finished,
		})
	}
	httpx.RespondJSON(w, devices)
}

type updateDeviceRequest struct {
	DeviceName  *string `json:"deviceName"`
	Enabled     *bool   `json:"enabled"`
	NeedsInput  *bool   `json:"needsInput"`
	Finished    *bool   `json:"finished"`
}

func (h *Handler) UpdateDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceId")
	var req updateDeviceRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if err := h.store.UpdatePushSubscriptionPrefs(deviceID, req.Enabled, req.NeedsInput, req.Finished, req.DeviceName); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, okResponse{OK: true})
}

func (h *Handler) RemoveDevice(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("deviceId")
	if err := h.store.RemovePushSubscriptionByDeviceID(deviceID); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondNoContent(w)
}
