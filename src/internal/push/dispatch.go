package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/04mg/caw/internal/state"
	webpush "github.com/SherClockHolmes/webpush-go"
)

// PushPayload is the JSON sent to the service worker via the push message.
type PushPayload struct {
	Type      string `json:"type"`       // "needs_input" | "finished"
	Title     string `json:"title"`      // agent session title
	Body      string `json:"body"`      // human-readable description
	SessionID string `json:"sessionId"` // agent session ID
	Workspace string `json:"workspace"`  // workspace name (best-effort)
	AgentID   string `json:"agentId"`   // agent type id
}

// pendingFinished tracks debounce timers for "finished" push notifications,
// mirroring the 3-second debounce the frontend uses for toasts.
var (
	pendingFinished   = make(map[string]*time.Timer)
	pendingFinishedMu sync.Mutex
)

const finishedDebounce = 3 * time.Second

// agentDisplayName maps internal agent IDs to human-readable names for push notifications.
var agentDisplayName = map[string]string{
	"claude":   "Claude",
	"codex":    "Codex",
	"copilot":  "Copilot",
	"agy":      "Antigravity",
	"opencode": "OpenCode",
	"pi":       "Pi",
}

func agentName(agentID string) string {
	if name, ok := agentDisplayName[agentID]; ok {
		return name
	}
	return "Agent"
}

// Dispatch sends a push notification for the given event type to all stored
// subscriptions, respecting the user's push preferences. It runs non-blocking
// — each push send happens in its own goroutine.
func Dispatch(store *state.Store, eventType, sessionID, agentID, title, workspace string) {
	if !HasKeys() {
		return
	}

	prefs, err := store.GetPushPrefs()
	if err != nil {
		log.Printf("push: failed to read prefs: %v", err)
		return
	}
	if !prefs.Enabled {
		return
	}

	switch eventType {
	case "needs_input":
		if !prefs.NeedsInput {
			return
		}
	case "finished":
		if !prefs.Finished {
			return
		}
	default:
		return
	}

	// Build the payload — title is the agent action, body is the task title (secondary)
	name := agentName(agentID)
	notifTitle := fmt.Sprintf("%s is waiting for your input", name)
	if eventType == "finished" {
		notifTitle = fmt.Sprintf("%s has finished its task", name)
	}
	payload := PushPayload{
		Type:      eventType,
		Title:     notifTitle,
		Body:      title,
		SessionID: sessionID,
		Workspace: workspace,
		AgentID:   agentID,
	}

	subs, err := store.GetPushSubscriptions()
	if err != nil {
		log.Printf("push: failed to read subscriptions: %v", err)
		return
	}
	if len(subs) == 0 {
		return
	}

	payloadBytes, _ := json.Marshal(payload)

	var urgency webpush.Urgency
	var ttl int
	if eventType == "needs_input" {
		urgency = webpush.UrgencyHigh
		ttl = 2419200 // 28 days max — Apple rejects TTL=0
	} else {
		urgency = webpush.UrgencyNormal
		ttl = 3600
	}

	for _, sub := range subs {
		go sendToSubscription(store, sub, payloadBytes, urgency, ttl)
	}
}

// DispatchFinishedDebounced fires a "finished" push after a debounce delay.
// If the agent resumes working before the timer fires, the push is cancelled.
func DispatchFinishedDebounced(store *state.Store, sessionID, agentID, title, workspace string) {
	pendingFinishedMu.Lock()
	defer pendingFinishedMu.Unlock()

	if timer, ok := pendingFinished[sessionID]; ok {
		timer.Stop()
	}

	pendingFinished[sessionID] = time.AfterFunc(finishedDebounce, func() {
		pendingFinishedMu.Lock()
		delete(pendingFinished, sessionID)
		pendingFinishedMu.Unlock()
		Dispatch(store, "finished", sessionID, agentID, title, workspace)
	})
}

// CancelFinishedDebounced cancels a pending "finished" push for the given session.
// Called when the agent goes back to a working state.
func CancelFinishedDebounced(sessionID string) {
	pendingFinishedMu.Lock()
	defer pendingFinishedMu.Unlock()
	if timer, ok := pendingFinished[sessionID]; ok {
		timer.Stop()
		delete(pendingFinished, sessionID)
	}
}

func sendToSubscription(store *state.Store, sub state.PushSubscription, payload []byte, urgency webpush.Urgency, ttl int) {
	subscription := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			Auth:   sub.Auth,
			P256dh: sub.P256dh,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := webpush.SendNotificationWithContext(ctx, payload, subscription, &webpush.Options{
		Subscriber:      "noreply@caw.app",
		VAPIDPublicKey:  vapidKeys.public,
		VAPIDPrivateKey: vapidKeys.private,
		TTL:             ttl,
		Urgency:         urgency,
	})
	if err != nil {
		log.Printf("push: send to %s failed: %v", sub.Endpoint, err)
		return
	}
	defer resp.Body.Close()

	// 410 Gone or 404 Not Found means the subscription is permanently expired.
	if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
		_ = store.RemovePushSubscription(sub.Endpoint)
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("push: endpoint %s returned status %d", sub.Endpoint, resp.StatusCode)
	}
}