package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Rejection reasons (docs/protocol.md "Errors").
const (
	ReasonMalformedJSON    = "malformed_json"
	ReasonUnsupportedVer   = "unsupported_version"
	ReasonUnknownField     = "unknown_field"
	ReasonMissingField     = "missing_field"
	ReasonInvalidField     = "invalid_field"
	ReasonUnknownEventType = "unknown_event_type"
	ReasonOversize         = "oversize"
)

// SupportedVersions are the envelope versions this build accepts: v1 (AP and
// management observation), v2 (+ tx.ack/tx.report, docs/tx.md), v3
// (+ wifi.client_observed, station sightings) and v4 (+ bluetooth.* events,
// docs/bluetooth/event-protocol.md). Older sensors keep working.
var SupportedVersions = map[int64]bool{1: true, 2: true, 3: true, 4: true}

// supportedVersionsList is the sorted supported versions as text, for error
// messages (derived from SupportedVersions so the two never drift).
var supportedVersionsList = func() string {
	vs := make([]int, 0, len(SupportedVersions))
	for v := range SupportedVersions {
		vs = append(vs, int(v))
	}
	sort.Ints(vs)
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ", ")
}()

// ValidationError explains why an event was rejected.
type ValidationError struct {
	Reason string
	Field  string
	Msg    string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Reason, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %s", e.Reason, e.Field, e.Msg)
}

func verr(reason, field, format string, a ...any) *ValidationError {
	return &ValidationError{Reason: reason, Field: field, Msg: fmt.Sprintf(format, a...)}
}

var (
	reIdent  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)
	reMAC    = regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`)
	reHex    = regexp.MustCompile(`^([0-9a-f]{2}){0,32}$`)
	reSemver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+([-+][A-Za-z0-9.-]+)?$`)
)

type kind int

const (
	kInt kind = iota
	kNum
	kStr
	kBool
	kIdent
	kMAC
	kHex
	kSemver
	kEnum
	kStrArr
	kIntArr
	kIdentArr
	kRSN
)

type field struct {
	name      string
	k         kind
	req       bool
	min, max  float64 // numeric bounds (inclusive); ignored when both zero
	maxLen    int     // max string length in bytes, or max array items
	enum      []string
	unique    bool
	itemLen   int // max length of string array items
	itemRange [2]float64
}

func intF(name string, req bool, min, max float64) field {
	return field{name: name, k: kInt, req: req, min: min, max: max}
}

const maxInt = 1<<53 - 1 // largest integer exactly representable for JSON consumers

var rsnFields = []field{
	intF("version", true, 0, 65535),
	{name: "group_cipher", k: kStr, req: true, maxLen: 32},
	{name: "pairwise", k: kStrArr, req: true, maxLen: 8, itemLen: 32},
	{name: "akm", k: kStrArr, req: true, maxLen: 8, itemLen: 32},
	{name: "mfpc", k: kBool, req: true},
	{name: "mfpr", k: kBool, req: true},
}

var ssidFields = []field{
	{name: "ssid", k: kStr, req: true, maxLen: 32},
	{name: "ssid_hex", k: kHex, req: true},
	intF("rssi", true, -127, 0),
}

type typeSpec struct {
	needBSSID, needChannel bool
	meta                   []field
}

var specs = map[string]typeSpec{
	TypeHeartbeat: {meta: []field{
		intF("uptime_ms", true, 0, maxInt),
		intF("free_heap", true, 0, maxInt),
		intF("channel", false, 1, 14),
		intF("queue_drops", true, 0, maxInt),
	}},
	TypeSensorStatus: {meta: []field{
		{name: "state", k: kEnum, req: true, enum: []string{"booting", "online", "degraded", "error"}},
		{name: "firmware_version", k: kSemver, req: true},
		{name: "board", k: kStr, maxLen: 64},
		{name: "capabilities", k: kStrArr, req: true, maxLen: 16, itemLen: 32},
		{name: "message", k: kStr, maxLen: 128},
	}},
	TypeAPObserved: {needBSSID: true, needChannel: true, meta: append(append([]field{}, ssidFields...),
		field{name: "auth_mode", k: kStr, maxLen: 32},
		field{name: "hidden", k: kBool},
	)},
	TypeBeaconObserved: {needBSSID: true, needChannel: true, meta: append(append([]field{}, ssidFields...),
		intF("beacon_interval", false, 0, 65535),
		intF("capability", false, 0, 65535),
		field{name: "rsn", k: kRSN},
		intF("count", false, 1, maxInt),
	)},
	TypeDeauthObserved: {needBSSID: true, meta: mgmtFields},
	TypeDisassocObs:    {needBSSID: true, meta: mgmtFields},
	TypePMFObserved: {needBSSID: true, meta: []field{
		{name: "pmf", k: kEnum, req: true, enum: []string{"required", "capable", "not_advertised", "unavailable"}},
		{name: "rsn_present", k: kBool, req: true},
	}},
	TypeFinding: {meta: []field{
		{name: "finding_id", k: kIdent, req: true},
		{name: "category", k: kStr, req: true, maxLen: 64},
		{name: "severity", k: kEnum, req: true, enum: []string{"info", "low", "medium", "high"}},
		{name: "confidence", k: kNum, req: true, min: 0, max: 1},
		{name: "classification", k: kEnum, req: true, enum: []string{"observed", "suspicious", "potential_attack", "confirmed"}},
		{name: "explanation", k: kStr, req: true, maxLen: 1024},
		{name: "evidence", k: kIdentArr, maxLen: 64},
	}},
	TypeProviderStatus: {meta: []field{
		{name: "provider", k: kStr, req: true, maxLen: 32},
		{name: "state", k: kEnum, req: true, enum: []string{"unavailable", "stopped", "starting", "running", "error"}},
		{name: "message", k: kStr, maxLen: 256},
	}},
}

var mgmtFields = []field{
	intF("count", true, 1, maxInt),
	intF("window_ms", true, 1, maxInt),
	{name: "src", k: kMAC},
	{name: "dst", k: kMAC},
	{name: "reason_codes", k: kIntArr, maxLen: 8, unique: true, itemRange: [2]float64{0, 65535}},
	intF("rssi", false, -127, 0),
	{name: "protected", k: kBool},
}

// txSpecs are the v2 additions: responses of the deauth test transmission
// (docs/tx.md). A v1 sensor can never produce them.
var txSpecs = map[string]typeSpec{
	TypeTxAck: {meta: []field{
		{name: "command", k: kEnum, req: true, enum: []string{"deauth", "disassoc", "status"}},
		{name: "status", k: kEnum, req: true, enum: []string{"accepted", "rejected"}},
		{name: "detail", k: kStr, maxLen: 128},
		{name: "client", k: kMAC},
	}},
	TypeTxReport: {needBSSID: true, needChannel: true, meta: []field{
		{name: "command", k: kEnum, req: true, enum: []string{"deauth", "disassoc"}},
		{name: "client", k: kMAC, req: true},
		intF("reason", true, 0, 65535),
		intF("sent", true, 0, 65535),
		intF("requested", true, 0, 65535),
		{name: "outcome", k: kEnum, req: true,
			enum: []string{"complete", "aborted_host_lost", "aborted_radio"}},
	}},
}

// clientSpecs are the v3 additions: data-frame station sightings.
var clientSpecs = map[string]typeSpec{
	TypeWifiClientObserved: {needBSSID: true, needChannel: true, meta: []field{
		{name: "client", k: kMAC, req: true},
		intF("frames", true, 1, maxInt),
		intF("window_ms", true, 1, maxInt),
		intF("rssi", false, -127, 0),
	}},
}

// btSpecs are the v4 additions: Bluetooth Classic + BLE observation events,
// emitted only by the Bluetooth-enabled firmware build (switch-mode; the
// radio runs Wi-Fi OR Bluetooth, never both). All Bluetooth specifics live in
// metadata — the envelope's bssid/channel stay Wi-Fi-only. The device address
// is metadata "address" (BD_ADDR, same AA:BB:CC:DD:EE:FF form as a MAC).
// See docs/bluetooth/event-protocol.md.
var btSpecs = map[string]typeSpec{
	TypeBtClassicDiscovered: {meta: btClassicFields},
	TypeBtClassicUpdated:    {meta: btClassicFields},
	TypeBtBleAdvObserved: {meta: []field{
		{name: "address", k: kMAC, req: true},
		{name: "address_type", k: kEnum, req: true, enum: btAddrTypes},
		intF("rssi", true, -127, 0),
		{name: "adv_type", k: kEnum, enum: []string{"ind", "direct_ind", "scan_ind", "nonconn_ind", "scan_rsp"}},
		intF("payload_len", false, 0, 255),
		intF("flags", false, 0, 255),
		{name: "name", k: kStr, maxLen: 248},
		intF("tx_power", false, -127, 127),
		{name: "services", k: kStrArr, maxLen: 16, itemLen: 36},
		intF("manufacturer_id", false, 0, 65535),
		{name: "manufacturer_data", k: kHex},
		intF("window_ms", false, 1, maxInt),
		intF("count", false, 1, maxInt),
	}},
	TypeBtBleDiscovered: {meta: btBleDeviceFields},
	TypeBtBleUpdated:    {meta: btBleDeviceFields},
	TypeBtBleConnObserved: {meta: []field{
		{name: "address", k: kMAC, req: true},
		{name: "address_type", k: kEnum, enum: btAddrTypes},
	}},
	TypeBtBleConnLost: {meta: []field{
		{name: "address", k: kMAC, req: true},
		{name: "address_type", k: kEnum, enum: btAddrTypes},
		{name: "reason", k: kStr, maxLen: 128},
	}},
	TypeBtGattService: {meta: []field{
		{name: "address", k: kMAC, req: true},
		{name: "uuid", k: kStr, req: true, maxLen: 36},
		intF("start_handle", false, 0, 65535),
		intF("end_handle", false, 0, 65535),
		{name: "primary", k: kBool},
	}},
	TypeBtGattChar: {meta: []field{
		{name: "address", k: kMAC, req: true},
		{name: "service_uuid", k: kStr, maxLen: 36},
		{name: "uuid", k: kStr, req: true, maxLen: 36},
		intF("handle", false, 0, 65535),
		{name: "properties", k: kStrArr, maxLen: 8, itemLen: 24},
	}},
	TypeBtSystemStatus: {meta: []field{
		{name: "state", k: kEnum, req: true,
			enum: []string{"idle", "scanning_ble", "scanning_classic", "scanning_dual", "error"}},
		{name: "mode", k: kEnum, req: true, enum: []string{"wifi", "bluetooth"}},
		intF("classic_devices", false, 0, maxInt),
		intF("ble_devices", false, 0, maxInt),
		{name: "message", k: kStr, maxLen: 128},
	}},
	TypeBtSystemError: {meta: []field{
		{name: "code", k: kStr, req: true, maxLen: 64},
		{name: "message", k: kStr, req: true, maxLen: 256},
	}},
}

// btAddrTypes: BLE advertiser address types. "random" covers static and
// private (resolvable/non-resolvable) addresses — WRAITH never assumes a
// random address maps to one physical device.
var btAddrTypes = []string{"public", "random"}

var btClassicFields = []field{
	{name: "address", k: kMAC, req: true},
	{name: "name", k: kStr, maxLen: 248},
	intF("cod", false, 0, 16777215), // 24-bit Class of Device
	{name: "device_class", k: kEnum, enum: []string{
		"audio", "phone", "computer", "peripheral", "input", "wearable",
		"imaging", "network", "health", "toy", "uncategorized", "unknown"}},
	intF("rssi", false, -127, 0),
	intF("count", false, 1, maxInt),
	intF("window_ms", false, 1, maxInt),
}

var btBleDeviceFields = []field{
	{name: "address", k: kMAC, req: true},
	{name: "address_type", k: kEnum, req: true, enum: btAddrTypes},
	{name: "name", k: kStr, maxLen: 248},
	intF("rssi", false, -127, 0),
	{name: "services", k: kStrArr, maxLen: 16, itemLen: 36},
	intF("manufacturer_id", false, 0, 65535),
	{name: "classification", k: kEnum, enum: []string{
		"beacon", "wearable", "phone", "computer", "peripheral",
		"sensor", "tag", "medical", "unknown"}},
	{name: "confidence", k: kNum, min: 0, max: 1},
	intF("count", false, 1, maxInt),
}

// specsByVersion: v2 is v1 plus the tx event types; v3 adds client sightings;
// v4 adds the bluetooth.* events. Each is a superset of the one before.
var specsByVersion = func() map[int64]map[string]typeSpec {
	merge := func(base map[string]typeSpec, add ...map[string]typeSpec) map[string]typeSpec {
		n := len(base)
		for _, m := range add {
			n += len(m)
		}
		out := make(map[string]typeSpec, n)
		for k, v := range base {
			out[k] = v
		}
		for _, m := range add {
			for k, v := range m {
				out[k] = v
			}
		}
		return out
	}
	v2 := merge(specs, txSpecs)
	v3 := merge(v2, clientSpecs)
	v4 := merge(v3, btSpecs)
	return map[int64]map[string]typeSpec{1: specs, 2: v2, 3: v3, 4: v4}
}()

var envelopeFields = []field{
	intF("version", true, 0, maxInt), // exact version checked separately
	{name: "event_id", k: kIdent, req: true},
	{name: "source", k: kEnum, req: true, enum: []string{SourceNano, SourceCore}},
	{name: "sensor_id", k: kIdent, req: true},
	{name: "event_type", k: kStr, req: true, maxLen: 64},
	intF("timestamp", true, 0, maxInt),
	intF("seq", true, 0, maxInt),
	intF("channel", false, 1, 233),
	{name: "bssid", k: kMAC},
	{name: "metadata", k: -1, req: true}, // object, validated per type
}

// Parse validates raw (one JSON object, without framing newline) against the
// v1 schema and returns the decoded event.
func Parse(raw []byte) (*Event, error) {
	if len(raw) >= MaxMessageSize {
		return nil, verr(ReasonOversize, "", "%d bytes (max %d incl. newline)", len(raw), MaxMessageSize-1)
	}
	if !utf8.Valid(raw) {
		return nil, verr(ReasonMalformedJSON, "", "not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var top map[string]any
	if err := dec.Decode(&top); err != nil {
		return nil, verr(ReasonMalformedJSON, "", "%v", err)
	}
	if dec.More() {
		return nil, verr(ReasonMalformedJSON, "", "trailing data after object")
	}
	if top == nil {
		return nil, verr(ReasonMalformedJSON, "", "not a JSON object")
	}
	if err := checkObject(top, envelopeFields, ""); err != nil {
		return nil, err
	}
	ver, _ := top["version"].(json.Number).Int64()
	if !SupportedVersions[ver] {
		return nil, verr(ReasonUnsupportedVer, "version", "got %d, want one of %s", ver, supportedVersionsList)
	}
	et := top["event_type"].(string)
	spec, ok := specsByVersion[ver][et]
	if !ok {
		return nil, verr(ReasonUnknownEventType, "event_type", "%q", et)
	}
	if spec.needBSSID {
		if _, ok := top["bssid"]; !ok {
			return nil, verr(ReasonMissingField, "bssid", "required for %s", et)
		}
	}
	if spec.needChannel {
		if _, ok := top["channel"]; !ok {
			return nil, verr(ReasonMissingField, "channel", "required for %s", et)
		}
	}
	meta, ok := top["metadata"].(map[string]any)
	if !ok {
		return nil, verr(ReasonInvalidField, "metadata", "must be an object")
	}
	if err := checkObject(meta, spec.meta, "metadata."); err != nil {
		return nil, err
	}

	var ev Event
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, verr(ReasonMalformedJSON, "", "%v", err)
	}
	ev.Raw = append(json.RawMessage(nil), raw...)
	return &ev, nil
}

func checkObject(obj map[string]any, fields []field, prefix string) error {
	known := make(map[string]*field, len(fields))
	for i := range fields {
		known[fields[i].name] = &fields[i]
	}
	for k := range obj {
		if known[k] == nil {
			return verr(ReasonUnknownField, prefix+k, "not allowed")
		}
	}
	for i := range fields {
		f := &fields[i]
		v, present := obj[f.name]
		if !present {
			if f.req {
				return verr(ReasonMissingField, prefix+f.name, "required")
			}
			continue
		}
		if err := checkValue(v, f, prefix+f.name); err != nil {
			return err
		}
	}
	return nil
}

func checkValue(v any, f *field, path string) error {
	bad := func(format string, a ...any) error { return verr(ReasonInvalidField, path, format, a...) }
	switch f.k {
	case -1:
		if _, ok := v.(map[string]any); !ok {
			return bad("must be an object")
		}
	case kInt:
		n, ok := v.(json.Number)
		if !ok {
			return bad("must be an integer")
		}
		i, err := n.Int64()
		if err != nil {
			return bad("must be an integer")
		}
		if (f.min != 0 || f.max != 0) && (float64(i) < f.min || float64(i) > f.max) {
			return bad("%d out of range [%v, %v]", i, f.min, f.max)
		}
	case kNum:
		n, ok := v.(json.Number)
		if !ok {
			return bad("must be a number")
		}
		x, err := n.Float64()
		if err != nil || x < f.min || x > f.max {
			return bad("%s out of range [%v, %v]", n, f.min, f.max)
		}
	case kBool:
		if _, ok := v.(bool); !ok {
			return bad("must be a boolean")
		}
	case kStr, kIdent, kMAC, kHex, kSemver, kEnum:
		s, ok := v.(string)
		if !ok {
			return bad("must be a string")
		}
		return checkString(s, f, bad)
	case kStrArr, kIdentArr, kIntArr:
		arr, ok := v.([]any)
		if !ok {
			return bad("must be an array")
		}
		if f.maxLen > 0 && len(arr) > f.maxLen {
			return bad("%d items (max %d)", len(arr), f.maxLen)
		}
		seen := map[string]bool{}
		for i, it := range arr {
			item := field{name: f.name}
			switch f.k {
			case kStrArr:
				item.k, item.maxLen = kStr, f.itemLen
			case kIdentArr:
				item.k = kIdent
			case kIntArr:
				item.k, item.min, item.max = kInt, f.itemRange[0], f.itemRange[1]
			}
			if err := checkValue(it, &item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
			if f.unique {
				key := fmt.Sprint(it)
				if seen[key] {
					return bad("duplicate item %s", key)
				}
				seen[key] = true
			}
		}
	case kRSN:
		obj, ok := v.(map[string]any)
		if !ok {
			return bad("must be an object")
		}
		return checkObject(obj, rsnFields, path+".")
	}
	return nil
}

func checkString(s string, f *field, bad func(string, ...any) error) error {
	switch f.k {
	case kStr:
		if f.maxLen > 0 && len(s) > f.maxLen {
			return bad("%d bytes (max %d)", len(s), f.maxLen)
		}
	case kIdent:
		if !reIdent.MatchString(s) {
			return bad("must match [A-Za-z0-9._:-]{1,64}")
		}
	case kMAC:
		if !reMAC.MatchString(s) {
			return bad("must be uppercase AA:BB:CC:DD:EE:FF")
		}
	case kHex:
		if !reHex.MatchString(s) {
			return bad("must be lowercase hex, at most 32 bytes")
		}
	case kSemver:
		if !reSemver.MatchString(s) {
			return bad("must be a semantic version")
		}
	case kEnum:
		for _, e := range f.enum {
			if s == e {
				return nil
			}
		}
		return bad("%q not one of %v", s, f.enum)
	}
	return nil
}
