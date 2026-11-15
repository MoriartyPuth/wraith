package btschedule

// Action is what the driver should do this tick.
type Action int

const (
	ActNone         Action = iota
	ActStartBLE            // send MODE BLUETOOTH
	ActStartClassic        // send MODE BLUETOOTH CLASSIC
	ActStop                // send MODE WIFI (end a scheduled session)
)

func (a Action) String() string {
	switch a {
	case ActStartBLE:
		return "start_ble"
	case ActStartClassic:
		return "start_classic"
	case ActStop:
		return "stop"
	default:
		return "none"
	}
}

// Planner is the scheduler's state machine. It only manages sessions it
// started: a session the operator starts by hand (radio already in Bluetooth
// when a scheduled start comes due) is left alone, and never force-stopped.
// Not safe for concurrent use; the driver goroutine owns one.
type Planner struct {
	cfg        Schedule
	inSession  bool
	sessionEnd int64 // Unix ms
	nextDue    int64 // Unix ms of the next scheduled start
	haveDue    bool
}

// SetConfig applies a (validated) config. It ends a scheduled session in
// progress when the schedule is turned off or changed, and realigns the next
// start to `now`. Returns ActStop if a scheduled session must be ended now.
func (p *Planner) SetConfig(cfg Schedule, now int64) Action {
	wasInSession := p.inSession
	p.cfg = cfg
	p.inSession = false
	p.sessionEnd = 0
	if cfg.Enabled {
		p.nextDue = cfg.NextStart(now)
		p.haveDue = true
	} else {
		p.haveDue = false
	}
	if wasInSession {
		return ActStop // tear down the running scheduled session cleanly
	}
	return ActNone
}

// Config returns the current schedule.
func (p *Planner) Config() Schedule { return p.cfg }

// InSession reports whether a scheduled session is currently running, and
// when it ends (Unix ms).
func (p *Planner) InSession() (bool, int64) { return p.inSession, p.sessionEnd }

// NextDue is the next scheduled start (Unix ms, ok=false when disabled).
func (p *Planner) NextDue() (int64, bool) { return p.nextDue, p.haveDue && p.cfg.Enabled }

// Tick advances the plan. radioBluetooth is whether the sensor's radio is
// currently in Bluetooth mode (from its last reported status).
func (p *Planner) Tick(now int64, radioBluetooth bool) Action {
	if !p.cfg.Enabled {
		if p.inSession {
			p.inSession = false
			return ActStop
		}
		return ActNone
	}
	if p.inSession {
		if now >= p.sessionEnd {
			p.inSession = false
			return ActStop
		}
		return ActNone
	}
	if !p.haveDue {
		p.nextDue = p.cfg.NextStart(now)
		p.haveDue = true
	}
	if now < p.nextDue {
		return ActNone
	}
	// A scheduled start is due. Schedule the following one regardless.
	p.nextDue = p.cfg.NextStart(now)
	if radioBluetooth {
		return ActNone // operator (or a previous session) is already on Bluetooth; don't manage it
	}
	p.inSession = true
	p.sessionEnd = now + int64(p.cfg.DurationSeconds)*1000
	if p.cfg.Classic {
		return ActStartClassic
	}
	return ActStartBLE
}
