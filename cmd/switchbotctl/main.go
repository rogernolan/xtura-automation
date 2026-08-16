package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "discover":
		runDiscover(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "settings":
		runSettings(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: switchbotctl <discover|status|settings> [flags]")
}

func flags(args []string) (*flag.FlagSet, string, context.Context) {
	fs := flag.NewFlagSet("switchbotctl", flag.ExitOnError)
	base := fs.String("base", defaultBaseURL(), "local API base URL")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	fs.Parse(args)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	_ = cancel
	return fs, *base, ctx
}

func defaultBaseURL() string {
	if base := os.Getenv("XTURA_API_URL"); base != "" {
		return base
	}
	return "http://127.0.0.1"
}

func runDiscover(args []string) {
	_, base, ctx := flags(args)
	var devices []struct {
		MAC      string    `json:"mac"`
		DevType  int       `json:"dev_type"`
		LastSeen time.Time `json:"last_seen"`
		RSSI     int       `json:"rssi"`
	}
	if err := doJSON(ctx, http.MethodGet, base+"/v1/sensors/discover", nil, &devices); err != nil {
		fail(err)
	}
	if len(devices) == 0 {
		fmt.Println("no SwitchBot devices observed yet")
		return
	}
	fmt.Printf("%-18s %-8s %8s %s\n", "MAC", "DevType", "RSSI", "LastSeen")
	for _, device := range devices {
		fmt.Printf("%-18s 0x%-6X %8d %s\n", device.MAC, device.DevType, device.RSSI, device.LastSeen.Local().Format(time.RFC3339))
	}
}

func runStatus(args []string) {
	_, base, ctx := flags(args)
	var settings struct {
		Enabled   bool   `json:"enabled"`
		HCIDevice string `json:"hci_device"`
		Sensors   []struct {
			Name    string `json:"name"`
			MAC     string `json:"mac"`
			Primary bool   `json:"primary"`
		} `json:"sensors"`
	}
	if err := doJSON(ctx, http.MethodGet, base+"/v1/sensors/settings", nil, &settings); err != nil {
		fail(err)
	}
	fmt.Printf("enabled: %t\n", settings.Enabled)
	fmt.Printf("hci_device: %s\n", settings.HCIDevice)
	if len(settings.Sensors) == 0 {
		fmt.Println("sensors: none configured")
		return
	}
	fmt.Println("sensors:")
	for _, sensor := range settings.Sensors {
		marker := ""
		if sensor.Primary {
			marker = " (primary)"
		}
		fmt.Printf("  %s  %s%s\n", sensor.MAC, sensor.Name, marker)
	}
}

func runSettings(args []string) {
	_, base, ctx := flags(args)
	var settings struct {
		Enabled   bool   `json:"enabled"`
		HCIDevice string `json:"hci_device"`
		Sensors   []struct {
			Name    string `json:"name"`
			MAC     string `json:"mac"`
			Primary bool   `json:"primary"`
		} `json:"sensors"`
	}
	if err := doJSON(ctx, http.MethodGet, base+"/v1/sensors/settings", nil, &settings); err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(encoded))
}

func doJSON(ctx context.Context, method, url string, body interface{}, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		if payload.Error != "" {
			return fmt.Errorf("%s %s: %s", method, url, payload.Error)
		}
		return fmt.Errorf("%s %s: status %d", method, url, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
