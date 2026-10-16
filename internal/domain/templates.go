package domain

import (
	"net/url"
	"slices"
	"strings"
)

type Template struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Site        Site     `json:"site"`
	Schedule    Schedule `json:"schedule"`
}

var siteTemplates = buildTemplates()

// The catalog owns template names and defaults; callers receive value copies.
func Templates() []Template { return slices.Clone(siteTemplates) }

func TemplateByID(id string) (Template, bool) {
	for _, template := range siteTemplates {
		if template.ID == id {
			return template, true
		}
	}
	return Template{}, false
}

func buildTemplates() []Template {
	base := Site{
		Name: "New API", Adapter: "newapi", Transport: "http", LoginPath: "/login", LoginAPIPath: "/api/user/login",
		CheckInPath: "/api/user/checkin", UserInfoPath: "/api/user/self",
		UserHeader: "New-Api-User", QuotaPerUnit: 500000, Unit: "USD",
		Selectors: Selectors{Username: `input[name="username"],input[name="email"],input[type="email"]`,
			Password: `input[type="password"]`, Submit: `button[type="submit"]`,
			GitHub:  `a[href*="/oauth/github"],button[data-provider="github"]`,
			LinuxDO: `a[href*="/oauth/linuxdo"],button[data-provider="linuxdo"]`},
	}
	any := base
	any.ID, any.Name, any.BaseURL, any.Adapter = "anyrouter", "AnyRouter", "https://anyrouter.top", "anyrouter"
	any.Transport, any.CheckInPath = "browser", ""
	agent := base
	agent.ID, agent.Name, agent.BaseURL, agent.Adapter = "agentrouter", "AgentRouter", "https://agentrouter.org", "agentrouter"
	agent.Transport, agent.CheckInPath = "browser", ""
	page := base
	page.Name, page.Adapter, page.Transport, page.CheckInPath = "自定义网页签到", "browser", "browser", "/"
	wong := base
	wong.Name, wong.BaseURL, wong.Transport = "WONG", "https://wzw.pp.ua", "browser"
	mint := base
	mint.Name, mint.BaseURL, mint.Adapter = "薄荷 API", "https://up.x666.me", "x666"
	mint.Transport, mint.LoginPath, mint.LoginAPIPath = "browser", "/", "/api/auth/login"
	mint.CheckInPath, mint.UserInfoPath = "/api/checkin/spin", "/api/checkin/status"
	mint.QuotaPerUnit, mint.Unit = 500, "次"
	templates := []Template{
		{ID: "anyrouter", Description: "北京时间 08:00 后访问已登录页面并确认身份，无需调用签到接口。", Site: any},
		{ID: "agentrouter", Description: "北京时间 08:00 后重新登录触发签到，使用密码或已授权的 OAuth 会话。", Site: agent},
		{ID: "newapi", Description: "通用接口模板，适用于已启用签到功能的 New API 兼容站点；填写站点地址，必要时调整接口路径。", Site: base},
		{ID: "browser", Description: "通用页面模板，填写站点地址、签到页面，以及签到按钮和成功结果的 CSS 选择器。", Site: page},
		{ID: "wong", Description: "WONG 专用配置，使用 New API 签到接口，并复核今日签到状态。", Site: wong},
		{ID: "x666", Description: "在 up.x666.me 使用 LinuxDO 登录并保存会话；每天执行一次免费抽奖，奖励直接到账。", Site: mint},
	}
	for i := range templates {
		templates[i].Site.TemplateID = templates[i].ID
		templates[i].Schedule = DefaultSchedule()
	}
	return templates
}

// NormalizeTemplate upgrades legacy sites without replacing customized names,
// addresses or endpoints. Explicit template IDs survive address changes.
func (s *Site) NormalizeTemplate() {
	if s.TemplateID != "" {
		return
	}
	s.TemplateID = s.Adapter
	for _, template := range siteTemplates {
		if template.Site.Adapter == s.Adapter && template.Site.BaseURL != "" && templateOrigin(s.BaseURL) == templateOrigin(template.Site.BaseURL) {
			s.TemplateID = template.ID
			break
		}
	}
	if (s.Adapter == "anyrouter" && s.Name == "Any Router") ||
		(s.Adapter == "agentrouter" && s.Name == "Agent Router") ||
		(s.Adapter == "x666" && s.Name == "薄荷 API 每日签到") {
		if template, ok := TemplateByID(s.TemplateID); ok {
			s.Name = template.Site.Name
		}
	}
}

func templateOrigin(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	host := strings.ToLower(u.Host)
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host = strings.TrimSuffix(host, ":"+u.Port())
	}
	return u.Scheme + "://" + host
}
