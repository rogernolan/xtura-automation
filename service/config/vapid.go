package config

import (
	"fmt"
	"strings"

	"github.com/SherClockHolmes/webpush-go"
)

// EnsureVAPIDKeys creates and persists a VAPID pair when notifications have
// not been configured yet. Existing pairs are never rotated automatically.
func EnsureVAPIDKeys(path string, cfg *Config) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config must not be nil")
	}
	publicKey := strings.TrimSpace(cfg.Notifications.VAPIDPublicKey)
	privateKey := strings.TrimSpace(cfg.Notifications.VAPIDPrivateKey)
	if publicKey != "" && privateKey != "" {
		return false, nil
	}
	if publicKey != "" || privateKey != "" {
		return false, fmt.Errorf("notifications.vapid_public_key and notifications.vapid_private_key must be configured together")
	}
	if strings.TrimSpace(path) == "" {
		return false, fmt.Errorf("config path is required to persist generated VAPID keys")
	}

	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return false, fmt.Errorf("generate VAPID keys: %w", err)
	}
	cfg.Notifications.VAPIDPublicKey = publicKey
	cfg.Notifications.VAPIDPrivateKey = privateKey
	if err := SaveFile(path, *cfg); err != nil {
		return false, fmt.Errorf("persist generated VAPID keys: %w", err)
	}
	return true, nil
}
