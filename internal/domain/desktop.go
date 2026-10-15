package domain

type DesktopPreferences struct {
	AutoStart      bool  `json:"autostart"`
	MinimizeToTray bool  `json:"minimize_to_tray"`
	CloseToTray    bool  `json:"close_to_tray"`
	Version        int64 `json:"version"`
}
type TrayMenuItem struct {
	Action  string `json:"action"`
	Label   string `json:"label"`
	Checked bool   `json:"checked"`
}
type DesktopStatus struct {
	Preferences DesktopPreferences `json:"preferences"`
	TrayReady   bool               `json:"tray_ready"`
	WindowOpen  bool               `json:"window_open"`
	LastError   string             `json:"last_error"`
	Menu        []TrayMenuItem     `json:"menu"`
}
