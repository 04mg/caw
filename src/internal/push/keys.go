package push

import (
	"log"

	"github.com/04mg/caw/internal/state"
	webpush "github.com/SherClockHolmes/webpush-go"
)

// vapidKeys holds the in-memory VAPID key pair, loaded once at startup.
var vapidKeys struct {
	public  string
	private string
}

// EnsureVAPIDKeys loads or generates the VAPID key pair, persisting it to the
// settings KV table so the same keys survive restarts (browsers would
// otherwise lose their subscription association).
func EnsureVAPIDKeys(store *state.Store) {
	pub, err := store.GetSetting("vapid_public_key")
	if err == nil && pub != "" {
		priv, err := store.GetSetting("vapid_private_key")
		if err == nil && priv != "" {
			vapidKeys.public = pub
			vapidKeys.private = priv
			return
		}
	}

	// Generate new keys
	priv, pubk, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		log.Printf("push: failed to generate VAPID keys: %v", err)
		return
	}

	if err := store.SetSetting("vapid_public_key", pubk); err != nil {
		log.Printf("push: failed to persist VAPID public key: %v", err)
		return
	}
	if err := store.SetSetting("vapid_private_key", priv); err != nil {
		log.Printf("push: failed to persist VAPID private key: %v", err)
		return
	}

	vapidKeys.public = pubk
	vapidKeys.private = priv
}

// PublicKey returns the VAPID public key (base64url, no padding) for the browser.
func PublicKey() string {
	return vapidKeys.public
}

// PrivateKey returns the VAPID private key.
func PrivateKey() string {
	return vapidKeys.private
}

// HasKeys reports whether VAPID keys have been initialized.
func HasKeys() bool {
	return vapidKeys.public != "" && vapidKeys.private != ""
}