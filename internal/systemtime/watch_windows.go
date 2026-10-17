//go:build windows

// Package systemtime watches clock changes and resume events without polling.
package systemtime

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmTimeChange       = 0x001e
	wmPowerBroadcast   = 0x0218
	wmClose            = 0x0010
	pbtResumeAutomatic = 0x0012
	pbtResumeSuspend   = 0x0007
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	registerClass    = user32.NewProc("RegisterClassW")
	createWindow     = user32.NewProc("CreateWindowExW")
	destroyWindow    = user32.NewProc("DestroyWindow")
	defWindowProc    = user32.NewProc("DefWindowProcW")
	getMessage       = user32.NewProc("GetMessageW")
	dispatchMessage  = user32.NewProc("DispatchMessageW")
	postMessage      = user32.NewProc("PostMessageW")
	postQuitMessage  = user32.NewProc("PostQuitMessage")
	registerResume   = user32.NewProc("RegisterSuspendResumeNotification")
	unregisterResume = user32.NewProc("UnregisterSuspendResumeNotification")
	className, _     = syscall.UTF16PtrFromString("AutoCheckInSystemTime")
	classOnce        sync.Once
	classError       error
	listeners        sync.Map
	windowProc       = syscall.NewCallback(handleMessage)
)

type windowClass struct {
	Style                              uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
}
type windowMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}
type watcher struct {
	window uintptr
	done   chan struct{}
}

func handleMessage(window uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmClose {
		_, _, _ = postQuitMessage.Call(0)
		return 0
	}
	if message == wmTimeChange || (message == wmPowerBroadcast && (wParam == pbtResumeAutomatic || wParam == pbtResumeSuspend)) {
		if notify, ok := listeners.Load(window); ok {
			notify.(func())()
		}
		return 1
	}
	result, _, _ := defWindowProc.Call(window, uintptr(message), wParam, lParam)
	return result
}

func start(notify func()) (*watcher, error) {
	ready := make(chan error, 1)
	w := &watcher{done: make(chan struct{})}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(w.done)
		classOnce.Do(func() {
			class := windowClass{Proc: windowProc, ClassName: className}
			if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
				classError = fmt.Errorf("register time event window: %w", err)
			}
		})
		if classError != nil {
			ready <- classError
			return
		}
		// A hidden top-level window receives broadcasts; message-only windows do not.
		hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
		if hwnd == 0 {
			ready <- fmt.Errorf("create time event window: %w", err)
			return
		}
		w.window = hwnd
		listeners.Store(hwnd, notify)
		defer listeners.Delete(hwnd)
		defer destroyWindow.Call(hwnd)
		power, _, err := registerResume.Call(hwnd, 0)
		if power == 0 {
			ready <- fmt.Errorf("register resume notification: %w", err)
			return
		}
		defer unregisterResume.Call(power)
		ready <- nil
		var message windowMessage
		for {
			result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(result) <= 0 {
				return
			}
			_, _, _ = dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
		}
	}()
	if err := <-ready; err != nil {
		<-w.done
		return nil, err
	}
	return w, nil
}

// Watch returns an idempotent stop function that waits for native resources to close.
func Watch(notify func()) (func(), error) {
	w, err := start(notify)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			select {
			case <-w.done:
				return
			default:
			}
			_, _, _ = postMessage.Call(w.window, wmClose, 0, 0)
			<-w.done
		})
	}, nil
}
