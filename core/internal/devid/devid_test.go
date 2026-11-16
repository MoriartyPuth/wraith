package devid

import "testing"

func TestLookup(t *testing.T) {
	if n := Prefixes(); n < 30000 {
		t.Fatalf("embedded OUI table too small: %d", n)
	}
	cases := []struct {
		mac, vendor, hint string
		private           bool
	}{
		{"8C:98:6B:26:DD:59", "Apple", "iPhone, iPad, Mac or other Apple device", false},
		{"8c:98:6b:26:dd:59", "Apple", "iPhone, iPad, Mac or other Apple device", false}, // lower case
		{"64:24:4D:11:27:AA", "Ezviz", "camera", false},
		{"94:24:B8:6E:8B:0C", "Gree", "air conditioner", false},
		{"10:A5:1D:90:66:C7", "Intel", "laptop or PC", false},
		{"D4:8D:26:86:80:D3", "LG", "TV or appliance", false},
		{"F0:0F:EC:71:C6:A8", "Huawei", "phone or router", false},
		{"F0:09:0D:36:B8:56", "TP-Link", "router, extender or smart-home device", false},
		{"FC:B0:DE:86:8F:E3", "Foxconn", "laptop, PC or console", false},
		{"1C:4E:A2:59:0E:83", "V-Link", "", false}, // generic shortening, no guess
		{"32:B4:EC:04:82:C5", "", "private address: usually a phone, tablet or laptop", true},
		{"FF:FF:FF:FF:FF:FF", "", "", false}, // broadcast
		{"33:33:00:00:00:01", "", "", false}, // multicast
		{"nope", "", "", false},
		{"ZZ:00:00:00:00:00", "", "", false},
	}
	for _, c := range cases {
		got := Lookup(c.mac)
		if got.Vendor != c.vendor || got.Hint != c.hint || got.Private != c.private {
			t.Errorf("Lookup(%s) = %+v, want vendor=%q hint=%q private=%v", c.mac, got, c.vendor, c.hint, c.private)
		}
		if got.Vendor != "" && got.VendorFull == "" {
			t.Errorf("Lookup(%s): vendor without full name", c.mac)
		}
	}
}

func TestShorten(t *testing.T) {
	for in, want := range map[string]string{
		"Shenzhen V-Link Technology CO., LTD.":     "V-Link",
		"Hangzhou Ezviz Software Co.,Ltd.":         "Ezviz Software",
		"GREE ELECTRIC APPLIANCES, INC. OF ZHUHAI": "GREE ELECTRIC APPLIANCES",
		"Acme Widgets GmbH":                        "Acme Widgets",
		"Foo (Thailand) Co., Ltd.":                 "Foo",
		"Inc.":                                     "Inc", // nothing to strip but the dot
	} {
		if got := shorten(in); got != want {
			t.Errorf("shorten(%q) = %q, want %q", in, got, want)
		}
	}
}
