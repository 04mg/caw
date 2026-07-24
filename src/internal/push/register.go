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
	mux.HandleFunc("GET /push/prefs", h.GetPrefs)
	mux.HandleFunc("PUT /push/prefs", h.SavePrefs)
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
	Endpoint string           `json:"endpoint" validate:"required"`
	Keys     subscribeKeysReq `json:"keys" validate:"required"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	var req subscribeRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}
	if err := h.store.AddPushSubscription(req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
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

func (h *Handler) GetPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := h.store.GetPushPrefs()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, prefs)
}

type savePrefsRequest struct {
	Enabled    *bool `json:"enabled"`
	NeedsInput *bool `json:"needsInput"`
	Finished   *bool `json:"finished"`
}

func (h *Handler) SavePrefs(w http.ResponseWriter, r *http.Request) {
	var req savePrefsRequest
	if !httpx.BindRequest(w, r, &req) {
		return
	}

	prefs, err := h.store.GetPushPrefs()
	if err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	if req.Enabled != nil {
		prefs.Enabled = *req.Enabled
	}
	if req.NeedsInput != nil {
		prefs.NeedsInput = *req.NeedsInput
	}
	if req.Finished != nil {
		prefs.Finished = *req.Finished
	}

	if err := h.store.SavePushPrefs(prefs); err != nil {
		httpx.RespondInternalErr(w, err)
		return
	}
	httpx.RespondJSON(w, prefs)
}