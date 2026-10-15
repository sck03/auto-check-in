package security

import (
	"auto-check-in/internal/domain"
	"net/http"
	"regexp"
	"strings"
	"unicode"
)

var querySecret = regexp.MustCompile(`(?i)(code|state|access_token|token|session|password)=([^&\s]+)`)
var authSecret = regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9+/_.=-]+`)

func Redact(message string, c domain.Credentials) string {
	secrets := []string{c.Password, c.Token}
	req := http.Request{Header: http.Header{"Cookie": []string{c.CookieHeader}}}
	for _, cookie := range req.Cookies() {
		secrets = append(secrets, cookie.Value)
	}
	for _, cookie := range c.Cookies {
		secrets = append(secrets, cookie.Value)
	}
	for _, origin := range c.Storage {
		for _, value := range origin {
			if len(value) > 8 {
				secrets = append(secrets, value)
			}
		}
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[已隐藏]")
		}
	}
	message = querySecret.ReplaceAllString(message, "$1=[已隐藏]")
	message = authSecret.ReplaceAllString(message, "$1 [已隐藏]")
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, message)
	runes := []rune(message)
	if len(runes) > 400 {
		message = string(runes[:400]) + "…"
	}
	return message
}
