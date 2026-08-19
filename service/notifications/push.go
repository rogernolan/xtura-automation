package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/SherClockHolmes/webpush-go"
)

type Subscription struct {
	Endpoint string           `json:"endpoint"`
	Keys     SubscriptionKeys `json:"keys"`
}
type SubscriptionKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}
type PushConfig struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}
type Sender struct {
	config PushConfig
	client *http.Client
}

func NewSender(config PushConfig, client *http.Client) *Sender {
	if client == nil {
		client = http.DefaultClient
	}
	return &Sender{config: config, client: client}
}

func (s *Sender) Send(ctx context.Context, sub Subscription, notification Notification) error {
	if s.config.PublicKey == "" || s.config.PrivateKey == "" || s.config.Subject == "" {
		return fmt.Errorf("web push is not configured")
	}
	body, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	resp, err := webpush.SendNotificationWithContext(ctx, body, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{P256dh: sub.Keys.P256DH, Auth: sub.Keys.Auth}}, &webpush.Options{VAPIDPublicKey: s.config.PublicKey, VAPIDPrivateKey: s.config.PrivateKey, Subscriber: s.config.Subject, TTL: 300, HTTPClient: s.client})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("push endpoint returned %s", resp.Status)
	}
	return nil
}
