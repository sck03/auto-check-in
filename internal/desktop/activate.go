package desktop

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ActivateExisting(dir, token string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "endpoint.json"))
	if err != nil {
		return false
	}
	var endpoint struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(raw, &endpoint) != nil {
		return false
	}
	target, err := url.Parse(endpoint.URL)
	if err != nil || target.Scheme != "http" || target.User != nil {
		return false
	}
	ip := net.ParseIP(target.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	if token == "" {
		raw, err = os.ReadFile(filepath.Join(dir, "access.token"))
		if err != nil {
			return false
		}
		token = strings.TrimSpace(string(raw))
	}
	request, err := http.NewRequest("POST", endpoint.URL+"/api/v1/desktop/action", bytes.NewBufferString(`{"action":"show"}`))
	if err != nil {
		return false
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode == 202
}
