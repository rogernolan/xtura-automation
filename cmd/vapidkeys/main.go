package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"empirebus-tests/service/config"
)

func main() {
	var configPath string
	var subject string
	flag.StringVar(&configPath, "config", "", "path to the Xtura config file")
	flag.StringVar(&subject, "subject", "", "VAPID subject URI, for example mailto:you@example.com")
	flag.Parse()
	if strings.TrimSpace(configPath) == "" {
		log.Fatal("-config is required")
	}

	cfg, err := config.LoadFile(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	oldSubject := cfg.Notifications.Subject
	if strings.TrimSpace(subject) != "" {
		cfg.Notifications.Subject = strings.TrimSpace(subject)
	}
	generated, err := config.EnsureVAPIDKeys(configPath, cfg)
	if err != nil {
		log.Fatalf("generate VAPID keys: %v", err)
	}
	if !generated && cfg.Notifications.Subject != oldSubject {
		if err := config.SaveFile(configPath, *cfg); err != nil {
			log.Fatalf("save VAPID subject: %v", err)
		}
	}

	if generated {
		fmt.Printf("generated VAPID keys in %s\n", configPath)
	} else {
		fmt.Printf("VAPID keys already exist in %s; they were not rotated\n", configPath)
	}
	fmt.Printf("public key: %s\n", cfg.Notifications.VAPIDPublicKey)
}
