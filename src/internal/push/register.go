package push

import (
	"github.com/gin-gonic/gin"

	"github.com/04mg/caw/internal/httpx"
	"github.com/04mg/caw/internal/state"
)

type Handler struct {
	store *state.Store
}

func Register(rg *gin.RouterGroup, store *state.Store) {
	h := &Handler{store: store}
	rg.GET("/push/vapid-public-key", h.GetVAPIDPublicKey)
	rg.POST("/push/subscribe", h.Subscribe)
	rg.DELETE("/push/subscribe", h.Unsubscribe)
	rg.GET("/push/prefs", h.GetPrefs)
	rg.PUT("/push/prefs", h.SavePrefs)
}

func (h *Handler) GetVAPIDPublicKey(c *gin.Context) {
	httpx.OK(c, gin.H{"publicKey": PublicKey()})
}

type subscribeRequest struct {
	Endpoint string            `json:"endpoint" binding:"required"`
	Keys     subscribeKeysReq  `json:"keys" binding:"required"`
}

type subscribeKeysReq struct {
	P256dh string `json:"p256dh" binding:"required"`
	Auth   string `json:"auth" binding:"required"`
}

func (h *Handler) Subscribe(c *gin.Context) {
	var req subscribeRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if err := h.store.AddPushSubscription(req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.Created(c, gin.H{"ok": true})
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
}

func (h *Handler) Unsubscribe(c *gin.Context) {
	var req unsubscribeRequest
	if !httpx.Bind(c, &req) {
		return
	}
	if err := h.store.RemovePushSubscription(req.Endpoint); err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.NoContent(c)
}

func (h *Handler) GetPrefs(c *gin.Context) {
	prefs, err := h.store.GetPushPrefs()
	if err != nil {
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, prefs)
}

type savePrefsRequest struct {
	Enabled    *bool `json:"enabled"`
	NeedsInput *bool `json:"needsInput"`
	Finished   *bool `json:"finished"`
}

func (h *Handler) SavePrefs(c *gin.Context) {
	var req savePrefsRequest
	if !httpx.Bind(c, &req) {
		return
	}

	prefs, err := h.store.GetPushPrefs()
	if err != nil {
		httpx.InternalErr(c, err)
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
		httpx.InternalErr(c, err)
		return
	}
	httpx.OK(c, prefs)
}