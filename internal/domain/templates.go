package domain

import "slices"

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

// A single generic template supports API requests and explicit page actions.
func (t Template) Supports(adapter string) bool {
	return t.Site.Adapter == adapter || (t.ID == "newapi" && adapter == "browser")
}

func buildTemplates() []Template {
	base := Site{
		Name: "New API（通用）", Adapter: "newapi", Transport: "http", LoginPath: "/login", LoginAPIPath: "/api/user/login",
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
	mint := base
	mint.Name, mint.BaseURL, mint.Adapter = "薄荷 API", "https://up.x666.me", "x666"
	mint.Transport, mint.LoginPath, mint.LoginAPIPath = "browser", "/", "/api/auth/login"
	mint.CheckInPath, mint.UserInfoPath = "/api/checkin/spin", "/api/checkin/status"
	mint.QuotaPerUnit, mint.Unit = 500, "次"
	templates := []Template{
		{ID: "anyrouter", Description: "北京时间 08:00 后访问已登录页面并确认身份，无需调用签到接口。", Site: any},
		{ID: "agentrouter", Description: "北京时间 08:00 后重新登录触发签到，使用密码或已授权的 OAuth 会话。", Site: agent},
		{ID: "newapi", Description: "适用于 WONG 等 New API 兼容站点；默认调用签到接口，也可选择网页按钮签到并配置页面选择器。", Site: base},
		{ID: "x666", Description: "在 up.x666.me 使用 LinuxDO 登录并保存会话；每天执行一次免费抽奖，奖励直接到账。", Site: mint},
	}
	for i := range templates {
		templates[i].Site.TemplateID = templates[i].ID
		templates[i].Schedule = DefaultSchedule()
	}
	return templates
}
