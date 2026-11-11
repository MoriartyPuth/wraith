package system

import (
	"os"
	"runtime"
	"strings"
	"time"
)

// Version of WRAITH Core.
const Version = "0.1.0"

// Info is returned by /api/v1/system/info.
type Info struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	GoVersion     string `json:"go_version"`
	OS            string `json:"os"`
	Kernel        string `json:"kernel"`
	Hostname      string `json:"hostname"`
	StartedAt     int64  `json:"started_at"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	SchemaVersion int    `json:"schema_version"`
	ProtocolVer   int    `json:"protocol_version"`
	// TXEnabled mirrors the Core configuration: whether the deauth test
	// transmission API is exposed (set by the api handler, not Collect).
	TXEnabled bool `json:"tx_enabled"`
}

// Collect gathers static host information.
func Collect(started time.Time) Info {
	host, _ := os.Hostname()
	kernel, _ := os.ReadFile("/proc/sys/kernel/osrelease")
	return Info{
		Name:          "WRAITH Core",
		Version:       Version,
		GoVersion:     runtime.Version(),
		OS:            osPrettyName(),
		Kernel:        strings.TrimSpace(string(kernel)),
		Hostname:      host,
		StartedAt:     started.UnixMilli(),
		UptimeSeconds: int64(time.Since(started).Seconds()),
	}
}

func osPrettyName() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
			return strings.Trim(v, `"`)
		}
	}
	return runtime.GOOS
}
