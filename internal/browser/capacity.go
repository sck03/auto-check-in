package browser

import (
	"context"

	"auto-check-in/internal/port"
)

// Capacity reports admission pressure without starting a browser. The broadcast
// is captured with the snapshot so workers cannot miss a released slot.
func (f *Factory) Capacity() port.Capacity {
	f.slotMu.Lock()
	defer f.slotMu.Unlock()
	result := port.Capacity{Changed: f.slotWake}
	if len(f.slots) == cap(f.slots) {
		result.BlockedTransports = []string{"browser"}
	}
	return result
}

func (f *Factory) reserveSlot(ctx context.Context, interactive bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f.lifetime.Err(); err != nil {
			return err
		}
		f.slotMu.Lock()
		select {
		case f.slots <- struct{}{}:
			f.notifySlots()
			f.slotMu.Unlock()
			return nil
		default:
		}
		wake := f.slotWake
		f.slotMu.Unlock()
		if interactive {
			return port.ErrBrowserBusy
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.lifetime.Done():
			return f.lifetime.Err()
		case <-wake:
		}
	}
}

func (f *Factory) releaseSlot() {
	f.slotMu.Lock()
	<-f.slots
	f.notifySlots()
	f.slotMu.Unlock()
}

func (f *Factory) notifySlots() {
	close(f.slotWake)
	f.slotWake = make(chan struct{})
}
