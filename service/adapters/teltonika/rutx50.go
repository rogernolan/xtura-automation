package teltonika

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	domainlocation "empirebus-tests/service/domains/location"
)

type RUTX50Config struct {
	Endpoint           string
	LoginEndpoint      string
	Username           string
	Password           string
	PasswordFile       string
	AuthToken          string
	InsecureSkipVerify bool
	Timeout            time.Duration
}

type RUTX50 struct {
	cfg         RUTX50Config
	client      *http.Client
	mu          sync.Mutex
	cachedToken string
}

func NewRUTX50(cfg RUTX50Config) *RUTX50 {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // Local RUTX50 devices commonly use self-signed HTTPS certs.
	return &RUTX50{
		cfg: cfg,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (r *RUTX50) Poll(ctx context.Context) (domainlocation.Fix, error) {
	data, err := r.getGPSStatus(ctx, false)
	if err != nil {
		return domainlocation.Fix{}, err
	}
	return r.fixFromStatus(data)
}

func (r *RUTX50) getGPSStatus(ctx context.Context, forceLogin bool) ([]byte, error) {
	endpoint := strings.TrimSpace(r.cfg.Endpoint)
	if endpoint == "" {
		endpoint = "http://192.168.51.1/api/gps/position/status"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	token, err := r.token(ctx, forceLogin)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && !forceLogin && r.hasCredentials() {
		r.clearCachedToken()
		return r.getGPSStatus(ctx, true)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rutx50 gps status http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (r *RUTX50) fixFromStatus(data []byte) (domainlocation.Fix, error) {
	var payload interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return domainlocation.Fix{}, fmt.Errorf("decode rutx50 gps status: %w", err)
	}
	lat, lon, err := coordinatesFromPayload(payload)
	if err != nil {
		return domainlocation.Fix{}, err
	}
	fix := domainlocation.Fix{
		Latitude:  lat,
		Longitude: lon,
		Source:    "rutx50:" + endpointPath(r.cfg.Endpoint),
		UpdatedAt: time.Now().UTC(),
	}
	if alt, ok := altitudeFromPayload(payload); ok {
		fix.Altitude = &alt
	}
	return fix, nil
}

func altitudeFromPayload(payload interface{}) (float64, bool) {
	values := map[string]float64{}
	walkPayload(payload, values)
	return firstCoordinate(values, "altitude", "alt", "elevation")
}

func (r *RUTX50) token(ctx context.Context, forceLogin bool) (string, error) {
	if strings.TrimSpace(r.cfg.AuthToken) != "" {
		return strings.TrimSpace(r.cfg.AuthToken), nil
	}
	if !r.hasCredentials() {
		return "", nil
	}
	r.mu.Lock()
	cached := r.cachedToken
	r.mu.Unlock()
	if cached != "" && !forceLogin {
		return cached, nil
	}
	token, err := r.login(ctx)
	if err != nil {
		return "", err
	}
	r.mu.Lock()
	r.cachedToken = token
	r.mu.Unlock()
	return token, nil
}

func (r *RUTX50) login(ctx context.Context) (string, error) {
	endpoint := strings.TrimSpace(r.cfg.LoginEndpoint)
	if endpoint == "" {
		endpoint = "https://192.168.51.1/api/login"
	}
	password, err := r.password()
	if err != nil {
		return "", err
	}
	bodies, err := loginRequestBodies(r.username(), password)
	if err != nil {
		return "", err
	}
	var lastErr error
	for _, body := range bodies {
		token, err := r.loginWithBody(ctx, endpoint, body)
		if err == nil {
			return token, nil
		}
		lastErr = err
		if !shouldTryAlternateLoginBody(err) {
			break
		}
	}
	return "", lastErr
}

func (r *RUTX50) loginWithBody(ctx context.Context, endpoint string, body []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("rutx50 login http %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("decode rutx50 login: %w", err)
	}
	token, ok := tokenFromPayload(payload)
	if !ok {
		return "", fmt.Errorf("rutx50 login response missing token")
	}
	return token, nil
}

func loginRequestBodies(username, password string) ([][]byte, error) {
	enveloped, err := json.Marshal(map[string]map[string]string{
		"data": {
			"username": username,
			"password": password,
		},
	})
	if err != nil {
		return nil, err
	}
	plain, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return nil, err
	}
	return [][]byte{enveloped, plain}, nil
}

func shouldTryAlternateLoginBody(err error) bool {
	text := err.Error()
	return strings.Contains(text, "rutx50 login http 400") ||
		strings.Contains(text, "rutx50 login http 401") ||
		strings.Contains(text, "rutx50 login response missing token")
}

func endpointPath(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "/api/gps/position/status"
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil || req.URL.Path == "" {
		return endpoint
	}
	return req.URL.Path
}

func (r *RUTX50) username() string {
	if strings.TrimSpace(r.cfg.Username) == "" {
		return "admin"
	}
	return strings.TrimSpace(r.cfg.Username)
}

func (r *RUTX50) password() (string, error) {
	if r.cfg.Password != "" {
		return r.cfg.Password, nil
	}
	if strings.TrimSpace(r.cfg.PasswordFile) == "" {
		return "", nil
	}
	data, err := os.ReadFile(strings.TrimSpace(r.cfg.PasswordFile))
	if err != nil {
		return "", fmt.Errorf("read rutx50 password file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *RUTX50) hasCredentials() bool {
	return r.cfg.Password != "" || strings.TrimSpace(r.cfg.PasswordFile) != ""
}

func (r *RUTX50) clearCachedToken() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedToken = ""
}

func coordinatesFromPayload(payload interface{}) (float64, float64, error) {
	values := map[string]float64{}
	walkPayload(payload, values)
	lat, latOK := firstCoordinate(values, "latitude", "lat")
	lon, lonOK := firstCoordinate(values, "longitude", "longitude_deg", "lon", "lng", "long")
	if !latOK || !lonOK {
		return 0, 0, fmt.Errorf("rutx50 gps status missing latitude/longitude")
	}
	if lat < -90 || lat > 90 {
		return 0, 0, fmt.Errorf("latitude out of range: %f", lat)
	}
	if lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("longitude out of range: %f", lon)
	}
	return lat, lon, nil
}

func tokenFromPayload(payload interface{}) (string, bool) {
	values := map[string]string{}
	walkStringPayload(payload, values)
	for _, key := range []string{"token", "access_token", "auth_token", "jwt"} {
		if value := strings.TrimSpace(values[normalizeKey(key)]); value != "" {
			return value, true
		}
	}
	return "", false
}

func walkPayload(value interface{}, values map[string]float64) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, nested := range v {
			if number, ok := numberValue(nested); ok {
				values[normalizeKey(key)] = number
			}
			walkPayload(nested, values)
		}
	case []interface{}:
		for _, nested := range v {
			walkPayload(nested, values)
		}
	}
}

func walkStringPayload(value interface{}, values map[string]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, nested := range v {
			if text, ok := nested.(string); ok {
				values[normalizeKey(key)] = text
			}
			walkStringPayload(nested, values)
		}
	case []interface{}:
		for _, nested := range v {
			walkStringPayload(nested, values)
		}
	}
}

func firstCoordinate(values map[string]float64, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, ok := values[normalizeKey(key)]
		if ok {
			return value, true
		}
	}
	return 0, false
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "-", "_")
	return key
}

func numberValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
