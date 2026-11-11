// Package system reports host information. Everything here is read-only:
// nothing changes interface state or NetworkManager configuration. Wireless
// interface details are informational; WRAITH captures only via the ESP32.
package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WirelessInterface describes one wireless netdev.
type WirelessInterface struct {
	Name           string   `json:"name"`
	Phy            string   `json:"phy"`
	Driver         string   `json:"driver"`
	Mode           string   `json:"mode"`
	SupportedModes []string `json:"supported_modes"`
	MonitorCapable bool     `json:"monitor_capable"`
	// MonitorConcurrent is true if the phy advertises an interface combination
	// that allows a monitor vif alongside a managed one. Most drivers do not;
	// then monitoring requires taking the managed link down.
	MonitorConcurrent bool `json:"monitor_concurrent"`
}

var sysClassNet = "/sys/class/net"

// ListWirelessInterfaces enumerates wireless interfaces via sysfs and `iw`.
func ListWirelessInterfaces() ([]WirelessInterface, error) {
	entries, err := os.ReadDir(sysClassNet)
	if err != nil {
		return nil, err
	}
	var out []WirelessInterface
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(sysClassNet, e.Name(), "phy80211")); err != nil {
			continue
		}
		info, err := WirelessInterfaceInfo(e.Name())
		if err != nil {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// WirelessInterfaceInfo inspects one interface.
func WirelessInterfaceInfo(name string) (WirelessInterface, error) {
	if strings.ContainsAny(name, "/.") || name == "" {
		return WirelessInterface{}, errors.New("invalid interface name")
	}
	base := filepath.Join(sysClassNet, name)
	phyLink, err := os.Readlink(filepath.Join(base, "phy80211"))
	if err != nil {
		return WirelessInterface{}, fmt.Errorf("not a wireless interface")
	}
	w := WirelessInterface{Name: name, Phy: filepath.Base(phyLink)}
	if d, err := os.Readlink(filepath.Join(base, "device", "driver")); err == nil {
		w.Driver = filepath.Base(d)
	}
	if out, err := runIW("dev", name, "info"); err == nil {
		w.Mode = parseIWDevMode(out)
	}
	if out, err := runIW("phy", w.Phy, "info"); err == nil {
		w.SupportedModes, w.MonitorConcurrent = parseIWPhy(out)
		for _, m := range w.SupportedModes {
			if m == "monitor" {
				w.MonitorCapable = true
			}
		}
	}
	return w, nil
}

func runIW(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "iw", args...).Output()
	return string(out), err
}

func parseIWDevMode(out string) string {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) >= 2 && f[0] == "type" {
			return strings.Join(f[1:], " ")
		}
	}
	return ""
}

// parseIWPhy extracts supported interface modes and whether any valid
// interface combination lists "monitor".
func parseIWPhy(out string) (modes []string, monitorCombo bool) {
	sc := bufio.NewScanner(strings.NewReader(out))
	section := ""
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, "\t "))
		if indent <= 1 && strings.HasSuffix(t, ":") {
			section = strings.TrimSuffix(t, ":")
			continue
		}
		switch section {
		case "Supported interface modes":
			if strings.HasPrefix(t, "* ") {
				modes = append(modes, strings.TrimPrefix(t, "* "))
			} else if indent <= 1 {
				section = ""
			}
		case "valid interface combinations":
			if strings.Contains(t, "monitor") {
				monitorCombo = true
			}
			if indent <= 1 && !strings.HasPrefix(t, "*") {
				section = ""
			}
		}
	}
	return modes, monitorCombo
}
