package garmin

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	discoverDialTimeout        = 400 * time.Millisecond
	discoverFingerprintTimeout = 3 * time.Second
	discoverReadLimit          = 1 << 20
	defaultConnectTimeout      = 10 * time.Second
	defaultDiscoverTimeout     = 5 * time.Second
	defaultDiscoverInterval    = time.Minute
)

var servFingerprintMarkers = []string{
	"mfd-channel-subscription",
	"application-settings",
}

func discoverServOnSubnet(ctx context.Context, host string, port int) (string, error) {
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil {
		return "", fmt.Errorf("garmin discovery: %q is not an IPv4 address", host)
	}
	v4 := ip.To4()
	prefix := fmt.Sprintf("%d.%d.%d", v4[0], v4[1], v4[2])

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	client := &http.Client{
		Timeout: discoverFingerprintTimeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext:       (&net.Dialer{Timeout: discoverDialTimeout}).DialContext,
		},
	}

	var (
		mu    sync.Mutex
		found string
		wg    sync.WaitGroup
	)
	for last := 1; last < 255; last++ {
		wg.Add(1)
		go func(last int) {
			defer wg.Done()
			if ctx.Err() != nil {
				return
			}
			addr := net.JoinHostPort(fmt.Sprintf("%s.%d", prefix, last), strconv.Itoa(port))
			conn, err := net.DialTimeout("tcp", addr, discoverDialTimeout)
			if err != nil {
				return
			}
			_ = conn.Close()
			matches, err := servFingerprint(ctx, client, addr)
			if err != nil || !matches {
				return
			}
			mu.Lock()
			if found == "" {
				found = addr
			}
			mu.Unlock()
			cancel()
		}(last)
	}
	wg.Wait()

	if found == "" {
		return "", fmt.Errorf("garmin discovery: no empirbus serv on %s.0/24 port %d", prefix, port)
	}
	return found, nil
}

func servFingerprint(ctx context.Context, client *http.Client, addr string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, discoverFingerprintTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/", nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, discoverReadLimit))
	if err != nil {
		return false, err
	}
	page := string(body)
	for _, marker := range servFingerprintMarkers {
		if !strings.Contains(page, marker) {
			return false, nil
		}
	}
	return true, nil
}

func servWSURL(configured, addr string) (string, error) {
	parsed, err := url.Parse(configured)
	if err != nil {
		return "", err
	}
	parsed.Host = addr
	return parsed.String(), nil
}

func hostPortOf(wsURL string) (string, int, error) {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return "", 0, err
	}
	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}
