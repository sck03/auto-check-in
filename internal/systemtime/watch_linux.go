//go:build linux

package systemtime

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Watch blocks on the kernel's clock-change notification, never a polling timer.
// CLOCK_REALTIME's offset changes on clock correction and across system suspend.
func Watch(notify func()) (func(), error) {
	fd, err := unix.TimerfdCreate(unix.CLOCK_REALTIME, unix.TFD_CLOEXEC|unix.TFD_NONBLOCK)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "system-time-events")
	var mu sync.Mutex
	closed := false
	arm := func() error {
		mu.Lock()
		defer mu.Unlock()
		if closed {
			return os.ErrClosed
		}
		deadline := unix.ItimerSpec{Value: unix.Timespec{Sec: time.Now().AddDate(100, 0, 0).Unix()}}
		return unix.TimerfdSettime(fd, unix.TFD_TIMER_ABSTIME|unix.TFD_TIMER_CANCEL_ON_SET, &deadline, nil)
	}
	if err := arm(); err != nil {
		file.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var event [8]byte
		for {
			_, err := file.Read(event[:])
			if err != nil && !errors.Is(err, unix.ECANCELED) {
				return
			}
			notify()
			if err := arm(); err != nil {
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			closed = true
			file.Close()
			mu.Unlock()
			<-done
		})
	}, nil
}
