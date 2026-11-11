// Package events defines the WRAITH event envelope and its strict
// validator. This is the reference implementation of docs/protocol.md and is
// tested against protocol/fixtures.
package events

import (
	"encoding/json"
	"time"
)

// ProtocolVersion is the newest envelope version this build emits and
// accepts; v1 sensors keep working (see validate.go).
const ProtocolVersion = 3

// MaxMessageSize is the maximum size of one serialized event, including the
// trailing newline when framed on a serial link.
const MaxMessageSize = 1024

// Event types (docs/protocol.md).
const (
	TypeHeartbeat      = "sensor.heartbeat"
	TypeSensorStatus   = "sensor.status"
	TypeAPObserved     = "wifi.ap_observed"
	TypeBeaconObserved = "wifi.beacon_observed"
	TypeDeauthObserved = "wifi.deauth_observed"
	TypeDisassocObs    = "wifi.disassoc_observed"
	TypePMFObserved    = "wifi.pmf_observed"
	// TypeWifiClientObserved: a station was seen exchanging data with an AP (v3).
	TypeWifiClientObserved = "wifi.client_observed"
	TypeTxAck              = "tx.ack"
	TypeTxReport           = "tx.report"
	TypeFinding            = "detection.finding"
	TypeProviderStatus     = "capture.provider_status"
)

// Sources.
const (
	SourceNano = "wraith-nano"
	SourceCore = "wraith-core"
)

// Event is a validated event (v1–v3). Metadata is kept raw; typed views are
// decoded on demand by consumers so the envelope stays cheap to pass around.
type Event struct {
	Version   int             `json:"version"`
	EventID   string          `json:"event_id"`
	Source    string          `json:"source"`
	SensorID  string          `json:"sensor_id"`
	Type      string          `json:"event_type"`
	Timestamp int64           `json:"timestamp"`
	Seq       uint64          `json:"seq"`
	Channel   *int            `json:"channel,omitempty"`
	BSSID     string          `json:"bssid,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`

	// ReceivedAt is set by Core on ingestion; it is not part of the wire format.
	ReceivedAt time.Time `json:"-"`
	// Raw is the exact validated JSON as received (stored as evidence).
	Raw json.RawMessage `json:"-"`
}

// EffectiveTime returns the sensor timestamp if the sensor had a wall clock,
// otherwise the time Core received the event.
func (e *Event) EffectiveTime() time.Time {
	if e.Timestamp > 0 {
		return time.Unix(e.Timestamp, 0)
	}
	return e.ReceivedAt
}

// RSN mirrors the protocol RSN object.
type RSN struct {
	Version     int      `json:"version"`
	GroupCipher string   `json:"group_cipher"`
	Pairwise    []string `json:"pairwise"`
	AKM         []string `json:"akm"`
	MFPC        bool     `json:"mfpc"`
	MFPR        bool     `json:"mfpr"`
}

// BeaconMeta is the metadata of wifi.beacon_observed.
type BeaconMeta struct {
	SSID           string `json:"ssid"`
	SSIDHex        string `json:"ssid_hex"`
	RSSI           int    `json:"rssi"`
	BeaconInterval *int   `json:"beacon_interval,omitempty"`
	Capability     *int   `json:"capability,omitempty"`
	RSN            *RSN   `json:"rsn,omitempty"`
	Count          *int   `json:"count,omitempty"`
}

// APMeta is the metadata of wifi.ap_observed.
type APMeta struct {
	SSID     string `json:"ssid"`
	SSIDHex  string `json:"ssid_hex"`
	RSSI     int    `json:"rssi"`
	AuthMode string `json:"auth_mode,omitempty"`
	Hidden   *bool  `json:"hidden,omitempty"`
}

// PMFMeta is the metadata of wifi.pmf_observed.
type PMFMeta struct {
	PMF        string `json:"pmf"`
	RSNPresent bool   `json:"rsn_present"`
}

// MgmtMeta is the metadata of wifi.deauth_observed / wifi.disassoc_observed.
type MgmtMeta struct {
	Count       int    `json:"count"`
	WindowMS    int    `json:"window_ms"`
	Src         string `json:"src,omitempty"`
	Dst         string `json:"dst,omitempty"`
	ReasonCodes []int  `json:"reason_codes,omitempty"`
	RSSI        *int   `json:"rssi,omitempty"`
	Protected   *bool  `json:"protected,omitempty"`
}

// HeartbeatMeta is the metadata of sensor.heartbeat.
type HeartbeatMeta struct {
	UptimeMS   int64 `json:"uptime_ms"`
	FreeHeap   int64 `json:"free_heap"`
	Channel    *int  `json:"channel,omitempty"`
	QueueDrops int64 `json:"queue_drops"`
}

// StatusMeta is the metadata of sensor.status.
type StatusMeta struct {
	State           string   `json:"state"`
	FirmwareVersion string   `json:"firmware_version"`
	Board           string   `json:"board,omitempty"`
	Capabilities    []string `json:"capabilities"`
	Message         string   `json:"message,omitempty"`
}

// TxAckMeta is the metadata of tx.ack (deauth test transmission, docs/tx.md).
type TxAckMeta struct {
	Command string `json:"command"` // deauth | disassoc | status
	Status  string `json:"status"`  // accepted | rejected
	Detail  string `json:"detail,omitempty"`
	Client  string `json:"client,omitempty"`
}

// TxReportMeta is the metadata of tx.report.
type TxReportMeta struct {
	Command   string `json:"command"`
	Client    string `json:"client"`
	Reason    int    `json:"reason"`
	Sent      int    `json:"sent"`
	Requested int    `json:"requested"`
	Outcome   string `json:"outcome"` // complete | aborted_host_lost | aborted_radio
}

// ClientMeta is the metadata of wifi.client_observed (v3): an aggregated
// station sighting, not guaranteed association state.
type ClientMeta struct {
	Client   string `json:"client"`
	Frames   int    `json:"frames"`
	WindowMS int    `json:"window_ms"`
	RSSI     *int   `json:"rssi,omitempty"`
}

// DecodeMeta decodes e.Metadata into v.
func (e *Event) DecodeMeta(v any) error { return json.Unmarshal(e.Metadata, v) }
