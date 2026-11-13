// Package bluetooth turns validated protocol v4 bluetooth.* events into the
// Bluetooth device inventory (docs/bluetooth/event-protocol.md). It is
// deliberately separate from the Wi-Fi ingest logic: ingest hands it every
// bluetooth.* event inside the same transaction and it never reads or writes
// Wi-Fi state.
package bluetooth

// ClassicMeta is the metadata of bluetooth.classic.device_discovered/updated.
type ClassicMeta struct {
	Address     string  `json:"address"`
	Name        *string `json:"name,omitempty"`
	CoD         *int    `json:"cod,omitempty"`
	DeviceClass *string `json:"device_class,omitempty"`
	RSSI        *int    `json:"rssi,omitempty"`
	Count       *int64  `json:"count,omitempty"`
	WindowMS    *int64  `json:"window_ms,omitempty"`
}

// BLEAdvMeta is the metadata of bluetooth.ble.advertisement_observed.
type BLEAdvMeta struct {
	Address          string   `json:"address"`
	AddressType      string   `json:"address_type"`
	RSSI             int      `json:"rssi"`
	AdvType          *string  `json:"adv_type,omitempty"`
	PayloadLen       *int     `json:"payload_len,omitempty"`
	Flags            *int     `json:"flags,omitempty"`
	Name             *string  `json:"name,omitempty"`
	TxPower          *int     `json:"tx_power,omitempty"`
	Services         []string `json:"services,omitempty"`
	ManufacturerID   *int     `json:"manufacturer_id,omitempty"`
	ManufacturerData *string  `json:"manufacturer_data,omitempty"`
	WindowMS         *int64   `json:"window_ms,omitempty"`
	Count            *int64   `json:"count,omitempty"`
}

// BLEDeviceMeta is the metadata of bluetooth.ble.device_discovered/updated.
type BLEDeviceMeta struct {
	Address        string   `json:"address"`
	AddressType    string   `json:"address_type"`
	Name           *string  `json:"name,omitempty"`
	RSSI           *int     `json:"rssi,omitempty"`
	Services       []string `json:"services,omitempty"`
	ManufacturerID *int     `json:"manufacturer_id,omitempty"`
	Classification *string  `json:"classification,omitempty"`
	Confidence     *float64 `json:"confidence,omitempty"`
	Count          *int64   `json:"count,omitempty"`
}

// AddressMeta is the part of connection and GATT events that names a device.
type AddressMeta struct {
	Address     string  `json:"address"`
	AddressType *string `json:"address_type,omitempty"`
}

// StatusMeta is the metadata of bluetooth.system.status.
type StatusMeta struct {
	State          string  `json:"state"`
	Mode           string  `json:"mode"`
	ClassicDevices *int64  `json:"classic_devices,omitempty"`
	BLEDevices     *int64  `json:"ble_devices,omitempty"`
	Message        *string `json:"message,omitempty"`
}

// ErrorMeta is the metadata of bluetooth.system.error.
type ErrorMeta struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
