package detection

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
)

// Deauth/disassoc burst thresholds (frames per BSSID within the window).
const (
	DeauthWindow        = 60 * time.Second
	DeauthSuspicious    = 30
	DeauthHigh          = 100
	DeauthFindingPeriod = 5 * time.Minute
)

const deauthLimitations = "A single ESP32 sensor hops channels and captures only a fraction of frames, so counts are lower bounds. " +
	"Transmitter addresses in management frames are trivially spoofed: frames \"from\" an AP are only addressed from it. " +
	"APs and clients legitimately send these frames on reboot, roaming, band steering, idle timeouts and when a device leaves."

type mgmtRec struct {
	at        time.Time
	count     int
	eventID   string
	broadcast bool
	protected bool
	reasons   []int
	kind      string
}

type deauthDetector struct {
	byBSSID map[string][]mgmtRec
	cd      *cooldown
}

func newDeauthDetector() *deauthDetector {
	return &deauthDetector{byBSSID: map[string][]mgmtRec{}, cd: newCooldown(DeauthFindingPeriod)}
}

func (d *deauthDetector) Name() string { return "deauth_activity" }

func (d *deauthDetector) Observe(c *Context) []Finding {
	ev := c.Event
	if ev.Type != events.TypeDeauthObserved && ev.Type != events.TypeDisassocObs {
		return nil
	}
	var m events.MgmtMeta
	if ev.DecodeMeta(&m) != nil || ev.BSSID == "" {
		return nil
	}
	kind := "deauthentication"
	if ev.Type == events.TypeDisassocObs {
		kind = "disassociation"
	}
	recs := append(d.byBSSID[ev.BSSID], mgmtRec{
		at: c.Now, count: m.Count, eventID: ev.EventID, broadcast: m.Dst == "FF:FF:FF:FF:FF:FF",
		protected: m.Protected != nil && *m.Protected, reasons: m.ReasonCodes, kind: kind,
	})
	cut := 0
	for cut < len(recs) && c.Now.Sub(recs[cut].at) > DeauthWindow {
		cut++
	}
	recs = recs[cut:]
	d.byBSSID[ev.BSSID] = recs

	total, bcast, unprot := 0, 0, 0
	reasons := map[int]bool{}
	kinds := map[string]bool{}
	var evidence []string
	for _, r := range recs {
		total += r.count
		if r.broadcast {
			bcast += r.count
		}
		if !r.protected {
			unprot += r.count
		}
		for _, rc := range r.reasons {
			reasons[rc] = true
		}
		kinds[r.kind] = true
		evidence = append(evidence, r.eventID)
	}
	if total < DeauthSuspicious || !d.cd.allow(ev.BSSID, c.Now) {
		return nil
	}

	pmfRequired := c.CurAP != nil && c.CurAP.PMF == "required"
	f := Finding{
		Category: "deauth_activity", BSSID: ev.BSSID, Evidence: evidence,
		Classification: ClassSuspicious, Severity: "medium", Confidence: 0.5,
		Limitations: deauthLimitations,
	}
	var why []string
	switch {
	case total >= DeauthHigh && pmfRequired && unprot > 0:
		f.Classification, f.Severity, f.Confidence = ClassPotential, "high", 0.7
		why = append(why, fmt.Sprintf("the AP advertises PMF as required, yet %d of these frames were unprotected; "+
			"a PMF-required AP should protect its deauthentication/disassociation frames, so unprotected ones are likely forged "+
			"(or sent to/from non-PMF clients if the network is misconfigured)", unprot))
	case total >= DeauthHigh:
		f.Severity, f.Confidence = "high", 0.6
	}
	if bcast > 0 {
		why = append(why, fmt.Sprintf("%d were addressed to broadcast (all clients)", bcast))
	}
	f.Explanation = fmt.Sprintf("%d %s frame(s) with BSSID %s in the last %s (threshold %d)",
		total, strings.Join(sortedKeys(kinds), "/"), ev.BSSID, DeauthWindow, DeauthSuspicious)
	if len(reasons) > 0 {
		f.Explanation += fmt.Sprintf("; reason codes %v", sortedInts(reasons))
	}
	if len(why) > 0 {
		f.Explanation += "; " + strings.Join(why, "; ")
	}
	f.Explanation += "."
	return []Finding{f}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedInts(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}
