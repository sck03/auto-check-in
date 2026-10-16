package domain

import "testing"

func TestStorageOnlySessionIsRecognized(t *testing.T) {
	c := Credentials{Method: "cookie", Storage: map[string]map[string]string{"https://site.test": {"session": "saved"}}}
	if !c.Summary().HasSession {
		t.Fatal("a saved page session was shown as missing")
	}
	c.Storage["https://site.test"] = map[string]string{}
	if c.Summary().HasSession {
		t.Fatal("empty browser storage was counted as a login session")
	}
}

func TestChangingIdentityClearsOldSession(t *testing.T) {
	c := Credentials{Method: "password", Username: "old", Password: "old-password", APIUser: "old-id", Token: "old-token", CookieHeader: "session=old", Cookies: []Cookie{{Name: "session", Value: "old"}}, Storage: map[string]map[string]string{"https://site.test": {"user": "old"}}}
	name := "new"
	updated := c.Apply(&CredentialPatch{Username: &name})
	if updated.Summary().HasSession || updated.Summary().HasToken || updated.APIUser != "" || len(updated.Storage) != 0 {
		t.Fatal("changed identity retained old authentication")
	}
	unchanged := c.Apply(&CredentialPatch{Username: &c.Username})
	if !unchanged.Summary().HasSession || unchanged.APIUser != c.APIUser {
		t.Fatal("ordinary edit discarded authentication")
	}
}

func TestReplacingTokenClearsConflictingBrowserSession(t *testing.T) {
	c := Credentials{Method: "cookie", Token: "old-token", APIUser: "old-id", CookieHeader: "session=old", Cookies: []Cookie{{Name: "session", Value: "old"}}, Storage: map[string]map[string]string{"https://up.x666.me": {"userToken": "old-token"}}}
	token := "new-token"
	updated := c.Apply(&CredentialPatch{Token: &token})
	if updated.Token != token || updated.Summary().HasSession || updated.APIUser != "" || len(updated.Storage) != 0 {
		t.Fatal("new token retained conflicting authentication from the old session")
	}
	unchanged := c.Apply(&CredentialPatch{Token: &c.Token})
	if !unchanged.Summary().HasSession || unchanged.APIUser != c.APIUser || len(unchanged.Storage) == 0 {
		t.Fatal("unchanged token discarded its browser session")
	}
}
