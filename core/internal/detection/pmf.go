package detection

import (
	"fmt"

	"github.com/MoriartyPuth/wraith/core/internal/events"
)

// pmfInvalidDetector reports RSN elements advertising MFPR without MFPC,
// which 802.11 defines as invalid. Informational.
type pmfInvalidDetector struct{ seen map[string]bool }

func newPMFInvalidDetector() *pmfInvalidDetector { return &pmfInvalidDetector{seen: map[string]bool{}} }

func (d *pmfInvalidDetector) Name() string { return "pmf_invalid_advertisement" }

func (d *pmfInvalidDetector) Observe(c *Context) []Finding {
	ev := c.Event
	if ev.Type != events.TypeBeaconObserved {
		return nil
	}
	var m events.BeaconMeta
	if ev.DecodeMeta(&m) != nil || m.RSN == nil || !m.RSN.MFPR || m.RSN.MFPC || d.seen[ev.BSSID] {
		return nil
	}
	d.seen[ev.BSSID] = true
	return []Finding{{
		Category: "pmf_invalid_advertisement", BSSID: ev.BSSID, Severity: "info", Confidence: 0.9,
		Classification: ClassObserved, Evidence: []string{ev.EventID},
		Explanation: fmt.Sprintf("BSSID %s advertises Management Frame Protection Required without Capable (MFPR=1, MFPC=0), "+
			"an invalid RSN configuration; clients may refuse to connect or ignore PMF.", ev.BSSID),
		Limitations: "A single corrupted capture can flip bits; the value is reported once per BSSID per Core run.",
	}}
}
