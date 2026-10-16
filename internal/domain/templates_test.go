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
		if site.Adapter == "browser" {
			site.Selectors.CheckIn, site.Selectors.Success = "#checkin", "#success"
		}
		if err := site.Validate(false); err != nil {
			t.Fatalf("%s defaults cannot be saved: %v", template.ID, err)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("expected six distinct templates, got %d", len(seen))
	}
	copy := Templates()
	copy[0].Site.Name = "custom"
	copy[0].Site.Selectors.Username = "#custom"
	original, _ := TemplateByID("anyrouter")
	if original.Site.Name != "AnyRouter" || original.Site.Selectors.Username == "#custom" {
		t.Fatal("editing a template copy changed the catalog")
	}
}

func TestLegacyTemplateIdentity(t *testing.T) {
	for _, test := range []struct {
		name, adapter, base, template, wantName string
	}{
		{"Any Router", "anyrouter", "https://mirror.test", "anyrouter", "AnyRouter"},
		{"Agent Router", "agentrouter", "https://agentrouter.org", "agentrouter", "AgentRouter"},
		{"薄荷 API 每日签到", "x666", "https://up.x666.me", "x666", "薄荷 API"},
		{"我的 WONG", "newapi", "https://WZW.PP.UA:443/", "wong", "我的 WONG"},
		{"自定义站点", "newapi", "https://custom.test", "newapi", "自定义站点"},
		{"自定义页面", "browser", "https://custom.test", "browser", "自定义页面"},
	} {
		t.Run(test.name, func(t *testing.T) {
			site := Site{Name: test.name, Adapter: test.adapter, BaseURL: test.base, CheckInPath: "/custom"}
			site.NormalizeTemplate()
			if site.TemplateID != test.template || site.Name != test.wantName || site.BaseURL != test.base || site.CheckInPath != "/custom" {
				t.Fatalf("legacy configuration was not preserved: %+v", site)
			}
			site.BaseURL, site.Name = "https://another.test", "我的新名称"
			site.NormalizeTemplate()
			if site.TemplateID != test.template || site.Name != "我的新名称" {
				t.Fatal("normalization replaced explicit identity or a custom name")
			}
		})
	}
}

func TestTemplateMustMatchAdapter(t *testing.T) {
	template, _ := TemplateByID("wong")
	for _, id := range []string{"missing", "agentrouter", "x666"} {
		site := template.Site
		site.TemplateID = id
		if site.Validate(false) == nil {
			t.Fatalf("accepted incompatible template %q", id)
		}
	}
}
