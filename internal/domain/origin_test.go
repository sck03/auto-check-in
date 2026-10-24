package domain

import "testing"

func TestCanonicalOriginMatchesBrowserDefaults(t *testing.T) {
	for input, want := range map[string]string{
		"https://EXAMPLE.com:443":  "https://example.com",
		"http://EXAMPLE.com:80":    "http://example.com",
		"https://example.com:8443": "https://example.com:8443",
		"https://[::1]:443":        "https://[::1]",
		"http://[::1]:80":          "http://[::1]",
	} {
		t.Run(input, func(t *testing.T) {
			site := Templates()[2].Site
			site.BaseURL = input
			if err := site.Validate(true); err != nil || site.BaseURL != want {
				t.Fatalf("origin=%s want=%s err=%v", site.BaseURL, want, err)
			}
		})
	}
}
