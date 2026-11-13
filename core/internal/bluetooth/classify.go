package bluetooth

// ClassFromCoD decodes the Major Device Class of a Bluetooth Classic Class of
// Device (CoD) field into WRAITH's device_class vocabulary. The CoD is what
// the device itself advertises in its inquiry response, so this is a decoding
// of an observed field, not a guess — but a device can advertise any CoD it
// likes. Nothing here looks at the address.
//
// CoD layout (Bluetooth Assigned Numbers, "Class of Device"):
//
//	bits 23-13 major service classes
//	bits 12-8  major device class
//	bits 7-2   minor device class
//	bits 1-0   format type
func ClassFromCoD(cod int) string {
	major := (cod >> 8) & 0x1F
	switch major {
	case 0x01:
		return "computer"
	case 0x02:
		return "phone"
	case 0x03:
		return "network" // LAN / network access point
	case 0x04:
		return "audio" // audio/video: headsets, speakers, car kits, TVs
	case 0x05:
		return peripheralClass(cod)
	case 0x06:
		return "imaging"
	case 0x07:
		return "wearable"
	case 0x08:
		return "toy"
	case 0x09:
		return "health"
	case 0x1F:
		return "uncategorized"
	default: // 0x00 miscellaneous and reserved values
		return "unknown"
	}
}

// peripheralClass splits the Peripheral major class into input devices and
// other peripherals using the minor class: bits 7-6 flag keyboard/pointing
// devices, bits 5-2 enumerate the device type.
func peripheralClass(cod int) string {
	if (cod>>6)&0x3 != 0 { // keyboard, pointing device, or combo
		return "input"
	}
	switch (cod >> 2) & 0xF {
	case 0x1, 0x2, 0x3, 0x5, 0x7, 0x9: // joystick, gamepad, remote, digitizer, pen, gestural
		return "input"
	}
	return "peripheral" // card reader, sensing device, scanner, uncategorized
}
