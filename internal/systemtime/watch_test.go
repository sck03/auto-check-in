package systemtime

import (
	"testing"
	"time"
)

func TestWatcherCanStartAndStopRepeatedly(t *testing.T) {
	for range 3 {
		stop, err := Watch(func() {})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { stop(); stop(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("closing watcher did not release its blocked wait")
		}
	}
}
