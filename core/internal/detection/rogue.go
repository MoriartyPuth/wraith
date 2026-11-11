package detection

import (
	"fmt"
	"strings"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
	"github.com/MoriartyPuth/wraith/core/internal/storage"
)

const rogueLimitations = "Many legitimate setups advertise one SSID from several BSSIDs (mesh systems, multi-AP networks, " +
	"repeaters, dual-band routers) and some mix security modes deliberately (guest, legacy-compatibility or transition " +
	"networks). This is an indicator to investigate, not proof of an evil twin."

// rogueAPDetector flags a BSSID that advertises the exact SSID bytes of other
// known BSSIDs but with weaker security than they do.
//
// It is evaluated when an AP's auth mode becomes known (first ap_observed
// for that BSSID, even if a beacon report created the row earlier), and in
// both directions: the newly identified AP may be the weaker one, or it may
// be a stronger sibling that reveals an already-known weaker one.
type rogueAPDetector struct{ cd *cooldown }

func newRogueAPDetector() *rogueAPDetector {
	return &rogueAPDetector{cd: newCooldown(30 * time.Minute)}
}

func (d *rogueAPDetector) Name() string { return "rogue_ap_indicator" }

func (d *rogueAPDetector) Observe(c *Context) []Finding {
	ev := c.Event
	if ev.Type != events.TypeAPObserved || c.CurAP == nil || c.Lookup == nil {
		return nil
	}
	if c.PrevAP != nil && c.PrevAP.AuthMode != nil {
		return nil // auth mode already known; changes are security_change's job
	}
	cur := c.CurAP
	if cur.SSIDHex == "" || strings.Trim(cur.SSIDHex, "0") == "" { // hidden
		return nil
	}
	if _, ok := authRank[deref(cur.AuthMode)]; !ok {
		return nil
	}
	peers, err := c.Lookup.APsBySSIDHex(cur.SSIDHex)
	if err != nil {
		return nil
	}
	group := []storage.AccessPoint{*cur}
	for _, p := range peers {
		if p.BSSID != cur.BSSID {
			if _, ok := authRank[deref(p.AuthMode)]; ok {
				group = append(group, p)
			}
		}
	}

	var out []Finding
	// Flag every member weaker than another member, where the comparison
	// involves the newly identified AP (as the weak one or the strong one).
	for _, target := range group {
		var stronger []storage.AccessPoint
		for _, other := range group {
			if other.BSSID == target.BSSID || (target.BSSID != cur.BSSID && other.BSSID != cur.BSSID) {
				continue
			}
			if authRank[deref(other.AuthMode)] > authRank[deref(target.AuthMode)] {
				stronger = append(stronger, other)
			}
		}
		if len(stronger) == 0 || !d.cd.allow(cur.SSIDHex+"/"+target.BSSID, c.Now) {
			continue
		}
		out = append(out, rogueFinding(target, stronger, ev.EventID))
	}
	return out
}

func rogueFinding(target storage.AccessPoint, stronger []storage.AccessPoint, evidence string) Finding {
	tRank := authRank[deref(target.AuthMode)]
	best, bestMode := -1, ""
	var list []string
	for _, s := range stronger {
		r := authRank[deref(s.AuthMode)]
		list = append(list, fmt.Sprintf("%s (%s)", s.BSSID, deref(s.AuthMode)))
		if r > best {
			best, bestMode = r, deref(s.AuthMode)
		}
	}
	if len(list) > 5 {
		list = append(list[:5], fmt.Sprintf("and %d more", len(list)-5))
	}
	sev, conf := "low", 0.3 // one step weaker, e.g. WPA/WPA2 mixed vs WPA2-only
	switch {
	case tRank <= 1:
		sev, conf = "high", 0.5 // open or WEP twin of a protected network
	case best-tRank >= 2:
		sev, conf = "medium", 0.4
	}
	return Finding{
		Category: "rogue_ap_indicator", BSSID: target.BSSID, Severity: sev, Confidence: conf,
		Classification: ClassSuspicious, Evidence: []string{evidence},
		Explanation: fmt.Sprintf("BSSID %s advertises SSID %q with %s, while other BSSIDs for the same SSID advertise stronger security (best: %s): %s.",
			target.BSSID, target.SSID, deref(target.AuthMode), bestMode, strings.Join(list, ", ")),
		Limitations: rogueLimitations,
	}
}
