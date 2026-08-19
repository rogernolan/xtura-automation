package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureVAPIDKeysGeneratesAndPersistsStablePair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	base := trackingBaseConfig()
	cfg := &base

	generated, err := EnsureVAPIDKeys(path, cfg)
	if err != nil {
		t.Fatalf("EnsureVAPIDKeys() error = %v", err)
	}
	if !generated {
		t.Fatal("EnsureVAPIDKeys() generated = false, want true")
	}
	if strings.TrimSpace(cfg.Notifications.VAPIDPublicKey) == "" || strings.TrimSpace(cfg.Notifications.VAPIDPrivateKey) == "" {
		t.Fatal("EnsureVAPIDKeys() did not populate both keys")
	}
	publicKey := cfg.Notifications.VAPIDPublicKey
	privateKey := cfg.Notifications.VAPIDPrivateKey

	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	generated, err = EnsureVAPIDKeys(path, reloaded)
	if err != nil {
		t.Fatalf("EnsureVAPIDKeys() second call error = %v", err)
	}
	if generated {
		t.Fatal("EnsureVAPIDKeys() second call generated = true, want false")
	}
	if reloaded.Notifications.VAPIDPublicKey != publicKey || reloaded.Notifications.VAPIDPrivateKey != privateKey {
		t.Fatal("EnsureVAPIDKeys() rotated an existing pair")
	}
}

func TestEnsureVAPIDKeysRejectsPartialPair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{Notifications: NotificationsConfig{VAPIDPublicKey: "public"}}

	if _, err := EnsureVAPIDKeys(path, cfg); err == nil || !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("EnsureVAPIDKeys() error = %v, want incomplete-pair error", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("EnsureVAPIDKeys() created %s for incomplete pair", path)
	}
}
