package detection

import (
	"fmt"
	"time"

	"github.com/MoriartyPuth/wraith/core/internal/events"
)

// authRank orders advertised auth modes by protection. Unknown modes are
// absent and never produce downgrade findings.
var authRank = map[string]int{
	"open": 0, "wep": 1, "owe": 2,
	"wpa-psk": 2, "wpa-eap": 3,
	"wpa-wpa2-psk": 3, "wpa-wpa2-eap": 4,
	"wpa2-psk": 4, "wpa2-eap": 5,
	"wpa2-wpa3-psk": 5,
	"wpa3-sae":      6, "wpa3-eap-192": 7,
}

var pmfRank = map[string]int{"not_advertised": 0, "capable": 1, "required": 2}

const securityLimitations = "Configuration changes are often legitimate (administrator changes, firmware updates, " +
	"WPA3 transition settings). A forged beacon with the same BSSID would produce the same observation; " +
	"compare with the AP's configuration before acting."

type securityChangeDetector struct{ cd *cooldown }

func newSecurityChangeDetector() *securityChangeDetector {
	return &securityChangeDetector{cd: newCooldown(10 * time.Minute)}
}

func (d *securityChangeDetector) Name() string { return "security_change" }

func (d *securityChangeDetector) Observe(c *Context) []Finding {
	ev := c.Event
	if c.PrevAP == nil || c.CurAP == nil {
		return nil
	}
	prev, cur := c.PrevAP, c.CurAP
	var out []Finding

	switch ev.Type {
	case events.TypeAPObserved:
		pa, pok := authRank[deref(prev.AuthMode)]
		ca, cok := authRank[deref(cur.AuthMode)]
		if pok && cok && ca < pa && d.cd.allow(ev.BSSID+"/auth", c.Now) {
			sev := "medium"
			if ca <= 1 {
				sev = "high" // to open or WEP
			}
			out = append(out, Finding{
				Category: "security_downgrade", BSSID: ev.BSSID, Severity: sev, Confidence: 0.6,
				Classification: ClassSuspicious, Evidence: []string{ev.EventID},
				Explanation: fmt.Sprintf("BSSID %s (SSID %q) changed its advertised security from %s to %s.",
					ev.BSSID, cur.SSID, deref(prev.AuthMode), deref(cur.AuthMode)),
				Limitations: securityLimitations,
			})
		}
		if prev.SSIDHex != "" && cur.SSIDHex != "" && prev.SSIDHex != cur.SSIDHex &&
			d.cd.allow(ev.BSSID+"/ssid", c.Now) {
			out = append(out, Finding{
				Category: "ap_identity_change", BSSID: ev.BSSID, Severity: "low", Confidence: 0.9,
				Classification: ClassObserved, Evidence: []string{ev.EventID},
				Explanation: fmt.Sprintf("BSSID %s changed its SSID from %q to %q.", ev.BSSID, prev.SSID, cur.SSID),
				Limitations: "Renaming a network is normally an administrator action. Multi-SSID APs use distinct BSSIDs per SSID, so a change on one BSSID is unusual but not by itself malicious.",
			})
		}
	case events.TypePMFObserved:
		pp, pok := pmfRank[prev.PMF]
		cp, cok := pmfRank[cur.PMF]
		if pok && cok && cp < pp && d.cd.allow(ev.BSSID+"/pmf", c.Now) {
			out = append(out, Finding{
				Category: "pmf_downgrade", BSSID: ev.BSSID, Severity: "medium", Confidence: 0.6,
				Classification: ClassSuspicious, Evidence: []string{ev.EventID},
				Explanation: fmt.Sprintf("BSSID %s (SSID %q) previously advertised PMF %s and now advertises %s. "+
					"Without PMF, clients accept unprotected deauthentication frames.",
					ev.BSSID, cur.SSID, prev.PMF, cur.PMF),
				Limitations: securityLimitations + " This reflects AP advertisements only, not what clients negotiated.",
			})
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
