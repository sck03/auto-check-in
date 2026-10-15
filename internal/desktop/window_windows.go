//go:build windows

package desktop

import (
	"sync"
	"syscall"
	"unsafe"
)

var user32 = syscall.NewLazyDLL("user32.dll")
var enumWindows = user32.NewProc("EnumWindows")
var getWindowPID = user32.NewProc("GetWindowThreadProcessId")
var getWindowOwner = user32.NewProc("GetWindow")
var windowVisible = user32.NewProc("IsWindowVisible")
var isIconic = user32.NewProc("IsIconic")
var showWindow = user32.NewProc("ShowWindow")
var foregroundWindow = user32.NewProc("SetForegroundWindow")

var windowSearchMu sync.Mutex
var windowSearchPID int
var windowSearchResult uintptr

// Windows callbacks cannot be freed by Go. Allocate one callback, not one per
// polling tick, so a long-running tray instance never exhausts callback slots.
var windowSearchCallback = syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
	var process uint32
	_, _, _ = getWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&process)))
	owner, _, _ := getWindowOwner.Call(hwnd, 4)
	visible, _, _ := windowVisible.Call(hwnd)
	if int(process) == windowSearchPID && owner == 0 && visible != 0 {
		windowSearchResult = hwnd
		return 0
	}
	return 1
})

func findWindow(pid int) uintptr {
	if pid <= 0 {
		return 0
	}
	windowSearchMu.Lock()
	defer windowSearchMu.Unlock()
	windowSearchPID = pid
	windowSearchResult = 0
	_, _, _ = enumWindows.Call(windowSearchCallback, 0)
	return windowSearchResult
}
func minimized(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	value, _, _ := isIconic.Call(hwnd)
	return value != 0
}
func restoreWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	_, _, _ = showWindow.Call(hwnd, 9)
	_, _, _ = foregroundWindow.Call(hwnd)
}
func minimizeWindow(hwnd uintptr) {
	if hwnd != 0 {
		_, _, _ = showWindow.Call(hwnd, 6)
	}
}
