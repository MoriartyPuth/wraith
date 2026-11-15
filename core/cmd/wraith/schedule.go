package main

import (
	"context"

	"github.com/MoriartyPuth/wraith/core/internal/capture"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

// scheduleEnv adapts the capture manager and store to btschedule.Env: it
// sends MODE commands and reports whether a scheduled switch can be delivered
// and whether the radio is already in Bluetooth mode.
type scheduleEnv struct {
	mgr   *capture.Manager
	store *storage.Store
}

func (e scheduleEnv) SendCommand(line string) error { return e.mgr.SendCommand(line) }

func (e scheduleEnv) RadioBluetooth(ctx context.Context) bool {
	bt, _ := e.store.AnyRadioMode(ctx, "bluetooth")
	return bt
}

func (e scheduleEnv) Ready(ctx context.Context) (canSwitch, classicOK bool) {
	running := false
	for _, st := range e.mgr.Statuses() {
		if st.State == capture.StateRunning || st.State == capture.StateDegraded {
			running = true
			break
		}
	}
	if !running {
		return false, false
	}
	canSwitch, _ = e.store.AnySensorHasCapability(ctx, "radio-switch")
	classicOK, _ = e.store.AnySensorHasCapability(ctx, "classic-inquiry")
	return canSwitch, classicOK
}
