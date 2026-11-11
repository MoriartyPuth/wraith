// IEEE 802.11-2020 Table 9-49 reason codes (subset most seen in
// deauthentication/disassociation frames).
const REASONS: Record<number, string> = {
  1: "unspecified",
  2: "previous authentication no longer valid",
  3: "station leaving (deauthenticated)",
  4: "inactivity",
  5: "AP unable to handle all associated stations",
  6: "class 2 frame from non-authenticated station",
  7: "class 3 frame from non-associated station",
  8: "station leaving (disassociated)",
  9: "not authenticated before association request",
  14: "message integrity failure",
  15: "4-way handshake timeout",
  16: "group key handshake timeout",
  23: "802.1X authentication failed",
  34: "too many unacknowledged frames",
};

export function reasonName(code: number): string {
  return REASONS[code] ?? "other";
}
