//go:build windows

package main

import (
	winassets "auto-check-in/build/windows"
	"auto-check-in/internal/bootstrap"
	"auto-check-in/internal/desktop"
	"auto-check-in/internal/port"
	"flag"
	"syscall"
	"unsafe"
)

func main() {
	config := bootstrap.EnvConfig()
	config.Listen = "127.0.0.1:0"
	config.Desktop = true
	background := flag.Bool("background", false, "Start in the system tray")
	flag.StringVar(&config.DataDir, "data", config.DataDir, "Persistent data directory (default: data beside executable)")
	flag.Parse()
	var manager *desktop.Manager
	config.NewDesktop = func(dir string) (port.DesktopIntegration, error) {
		var err error
		manager, err = desktop.NewManager(dir, *background)
		if err != nil {
			return nil, err
		}
		return manager, err
	}
	core, err := bootstrap.Start(config)
	if err != nil {
		dir, _ := config.DataDirectory()
		if dir != "" && desktop.ActivateExisting(dir, config.AccessToken) {
			return
		}
		showError(err.Error())
		return
	}
	defer core.Close()
	manager.Bind(core.URL, core.Token, config.BrowserPath, core.Service)
	manager.Run(winassets.Icon)
}
func showError(message string) {
	title, _ := syscall.UTF16PtrFromString("Auto Check-In")
	text, _ := syscall.UTF16PtrFromString(message)
	_, _, _ = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
