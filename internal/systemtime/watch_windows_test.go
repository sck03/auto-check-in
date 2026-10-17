//go:build windows

package systemtime

import (
	"testing"
	"time"
)

func TestClockAndResumeMessagesWakeListener(t *testing.T) {
	events := make(chan struct{}, 4)
	w, err := start(func() { events <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		select {
		case <-w.done:
		default:
			postMessage.Call(w.window, wmClose, 0, 0)
			<-w.done
		}
	}()
	for _, message := range []struct{ kind, value uintptr }{
		{wmTimeChange, 0}, {wmPowerBroadcast, pbtResumeAutomatic}, {wmPowerBroadcast, pbtResumeSuspend},
	} {
		postMessage.Call(w.window, message.kind, message.value, 0)
		select {
		case <-events:
		case <-time.After(time.Second):
			t.Fatal("system event did not wake the scheduler")
		}
	}
	// Query/suspend broadcasts must not start work before the system resumes.
	postMessage.Call(w.window, wmPowerBroadcast, 4, 0)
	postMessage.Call(w.window, wmTimeChange, 0, 0)
	// Drain the native queue before checking the count, so a delayed extra
	// callback cannot escape the negative assertion below.
	postMessage.Call(w.window, wmClose, 0, 0)
	select {
	case <-w.done:
	case <-time.After(time.Second):
		t.Fatal("message queue stalled")
	}
	select {
	case <-events:
	default:
		t.Fatal("time change did not wake the scheduler")
	}
	select {
	case <-events:
		t.Fatal("suspend event incorrectly woke work")
	default:
	}
}
