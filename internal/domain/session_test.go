package domain

import (
	"strings"
	"testing"
)

func TestSessionBudgetIncludesCookiesAndUTF8Storage(t *testing.T) {
	c := Credentials{Cookies: []Cookie{{Name: "session", Value: strings.Repeat("x", MaxSessionBytes/2)}}, Storage: map[string]map[string]string{"https://fixture.invalid": {"cache": strings.Repeat("界", MaxSessionBytes/6)}}}
	if c.ValidateSession() == nil {
		t.Fatal("combined session exceeded byte budget")
	}
	c.Storage = nil
	if err := c.ValidateSession(); err != nil {
		t.Fatal("bounded session rejected", err)
	}
	c.Storage = map[string]map[string]string{"https://fixture.invalid": {}}
	for i := 0; i <= MaxStorageEntries; i++ {
		c.Storage["https://fixture.invalid"][string(rune(i+32))] = ""
	}
	if c.ValidateSession() == nil {
		t.Fatal("unbounded entry overhead accepted")
	}
}
