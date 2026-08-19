package notifications

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type SubscriptionStore struct {
	mu            sync.Mutex
	path          string
	subscriptions []Subscription
}

func LoadSubscriptionStore(path string) (*SubscriptionStore, error) {
	s := &SubscriptionStore{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.subscriptions); err != nil {
		return nil, fmt.Errorf("decode subscriptions: %w", err)
	}
	return s, nil
}

func (s *SubscriptionStore) List() []Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Subscription(nil), s.subscriptions...)
}
func (s *SubscriptionStore) Upsert(sub Subscription) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.subscriptions {
		if s.subscriptions[i].Endpoint == sub.Endpoint {
			s.subscriptions[i] = sub
			return s.save()
		}
	}
	s.subscriptions = append(s.subscriptions, sub)
	return s.save()
}
func (s *SubscriptionStore) Remove(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.subscriptions[:0]
	for _, sub := range s.subscriptions {
		if sub.Endpoint != endpoint {
			out = append(out, sub)
		}
	}
	s.subscriptions = out
	return s.save()
}
func (s *SubscriptionStore) save() error {
	data, err := json.MarshalIndent(s.subscriptions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "subscriptions-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
