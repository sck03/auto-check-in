package domain

import "testing"

func TestTemplateCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, template := range Templates() {
		if seen[template.ID] || template.Site.Name == "" || template.Site.TemplateID != template.ID {
			t.Fatalf("invalid template identity: %+v", template)
		}
		seen[template.ID] = true
		site := template.Site
		if site.BaseURL == "" {
			site.BaseURL = "https://example.test"
		}
		if err := site.Validate(false); err != nil {
			t.Fatalf("%s defaults cannot be saved: %v", template.ID, err)
		}
	}
	if len(seen) != 4 || seen["wong"] || seen["browser"] {
		t.Fatalf("expected four unified templates, got %v", seen)
	}
	copy := Templates()
	copy[0].Site.Name = "custom"
	copy[0].Site.Selectors.Username = "#custom"
	original, _ := TemplateByID("anyrouter")
	if original.Site.Name != "AnyRouter" || original.Site.Selectors.Username == "#custom" {
		t.Fatal("editing a template copy changed the catalog")
	}
}

func TestTemplateMustMatchCheckInMode(t *testing.T) {
	for _, template := range Templates() {
		for _, adapter := range []string{"newapi", "browser", "anyrouter", "agentrouter", "x666", "unknown", ""} {
			site := template.Site
			site.BaseURL, site.CheckInPath = "https://example.test", "/checkin"
			site.Adapter, site.Transport = adapter, "browser"
			site.Selectors.CheckIn, site.Selectors.Success = "#checkin", "#success"
			want := adapter == template.Site.Adapter || (template.ID == "newapi" && adapter == "browser")
			if err := site.Validate(false); (err == nil) != want {
				t.Errorf("template=%s adapter=%s: %v", template.ID, adapter, err)
			}
		}
	}
	template, _ := TemplateByID("newapi")
	for _, id := range []string{"", "missing", "wong", "browser"} {
		site := template.Site
		site.BaseURL, site.TemplateID = "https://example.test", id
		if site.Validate(false) == nil {
			t.Fatalf("accepted unknown or removed template %q", id)
		}
	}
}

func TestPageCheckInAllowsAutomaticRecognitionWithBrowser(t *testing.T) {
	template, _ := TemplateByID("newapi")
	site := template.Site
	site.BaseURL, site.Adapter, site.CheckInPath = "https://example.test", "browser", "/daily"
	site.Selectors.CheckIn, site.Selectors.Success = "#checkin", "#success"
	if site.Validate(false) == nil {
		t.Fatal("page mode accepted HTTP transport")
	}
	site.Transport = "browser"
	if err := site.Validate(false); err != nil {
		t.Fatal(err)
	}
	site.Selectors.Success = ""
	site.Selectors.CheckIn = ""
	if err := site.Validate(false); err != nil {
		t.Fatal("automatic page recognition was rejected", err)
	}
}
