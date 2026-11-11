/* Host unit tests for hardware-independent firmware modules.
 * Build: scripts/firmware-host-tests.sh (plain gcc, no PlatformIO needed).
 * With `--emit`, prints sample protocol lines to stdout instead, for
 * cross-validation by `wraith validate` (scripts/check-protocol.sh). */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "frame_parser.h"
#include "lcd_screens.h"
#include "json_writer.h"
#include "wraith_protocol.h"

#include "test_util.h"

int failures;

static const uint8_t MAC[6] = {0x24, 0x0A, 0xC4, 0xA1, 0xB2, 0xC3};

static void test_identity(void)
{
    proto_identity_t id;
    proto_identity_init(&id, MAC, 0xdeadbeef);
    CHECK(strcmp(id.sensor_id, "nano-a1b2c3") == 0);
    CHECK(strcmp(id.boot_id, "deadbeef") == 0);
}

static void test_json_escaping(void)
{
    char buf[128];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    jw_object_begin(&jw);
    jw_kv_string(&jw, "a", "q\"b\\n\n\x01");
    jw_key(&jw, "arr");
    jw_array_begin(&jw);
    jw_int(&jw, -1);
    jw_bool(&jw, true);
    jw_array_end(&jw);
    jw_object_end(&jw);
    CHECK(!jw.overflow);
    CHECK(strcmp(buf, "{\"a\":\"q\\\"b\\\\n\\n\\u0001\",\"arr\":[-1,true]}") == 0);
}

static void test_utf8(void)
{
    const uint8_t khmer[] = {0xE1, 0x9E, 0x80}; /* U+1780 */
    const uint8_t bad[] = {'A', 0xC3};           /* truncated */
    const uint8_t overlong[] = {0xC0, 0xAF};
    const uint8_t surrogate[] = {0xED, 0xA0, 0x80};
    CHECK(jw_utf8_valid(khmer, sizeof khmer));
    CHECK(!jw_utf8_valid(bad, sizeof bad));
    CHECK(!jw_utf8_valid(overlong, sizeof overlong));
    CHECK(!jw_utf8_valid(surrogate, sizeof surrogate));

    char buf[64];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    jw_string_bytes(&jw, bad, sizeof bad);
    CHECK(strcmp(buf, "\"A?\"") == 0);
    jw_init(&jw, buf, sizeof buf);
    jw_hex(&jw, khmer, sizeof khmer);
    CHECK(strcmp(buf, "\"e19e80\"") == 0);
}

static void test_overflow(void)
{
    char buf[16];
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    jw_object_begin(&jw);
    jw_kv_string(&jw, "key", "a value that does not fit");
    CHECK(jw.overflow);
    CHECK(strlen(buf) < sizeof buf);

    proto_identity_t id;
    proto_identity_init(&id, MAC, 1);
    char small[40];
    CHECK(proto_heartbeat(small, sizeof small, &id, 0, 1, 1, 1, 0) == 0);
}

static void test_crc32(void)
{
    CHECK(proto_crc32("123456789", 9) == 0xCBF43926u); /* standard check value */
    CHECK(proto_crc32("", 0) == 0);

    proto_identity_t id;
    proto_identity_init(&id, MAC, 1);
    char buf[WRAITH_MAX_MESSAGE];
    size_t n = proto_heartbeat(buf, sizeof buf, &id, 0, 1, 1, 1, 0);
    CHECK(n > WRAITH_CRC_SUFFIX_LEN && buf[n - 1] == '\n' && buf[n - WRAITH_CRC_SUFFIX_LEN] == '\t');
    char want[16];
    snprintf(want, sizeof want, "%08lx", (unsigned long)proto_crc32(buf, n - WRAITH_CRC_SUFFIX_LEN));
    CHECK(strncmp(buf + n - 9, want, 8) == 0);
    CHECK(memchr(buf, '\t', n - WRAITH_CRC_SUFFIX_LEN) == NULL); /* no raw tab inside JSON */
}

static void test_lcd_screens(void)
{
    char a[LCD_COLS + 1], b[LCD_COLS + 1];
    lcd_snapshot_t s = {.wifi_ok = true, .aps = 12, .channel = 6, .best_rssi = -48, .deauth = 2,
                        .disassoc = 1, .host_connected = true, .sensor_id = "nano-70b2a8"};
    lcd_render(LCD_SCREEN_WIFI, &s, a, b);
    CHECK(strcmp(a, "AP:12  CH:06    ") == 0);
    CHECK(strcmp(b, "BEST RSSI:-48   ") == 0);
    lcd_render(LCD_SCREEN_HOST, &s, a, b);
    CHECK(strcmp(a, "HOST:CONNECTED  ") == 0 && strcmp(b, "nano-70b2a8     ") == 0);
    lcd_render(LCD_SCREEN_MGMT, &s, a, b);
    CHECK(strcmp(a, "DEAUTH:2        ") == 0 && strcmp(b, "DISASSOC:1      ") == 0);
    /* Overflow and edge values stay within 16 printable columns. */
    lcd_snapshot_t big = {.aps = 123456, .channel = 0, .best_rssi = 0, .deauth = 4000000000u,
                          .disassoc = 4000000000u, .sensor_id = "an-extremely-long-sensor-id\x01"};
    for (int sc = 0; sc < LCD_SCREEN_COUNT; sc++) {
        lcd_render((lcd_screen_t)sc, &big, a, b);
        CHECK(strlen(a) == LCD_COLS && strlen(b) == LCD_COLS);
        for (int i = 0; i < LCD_COLS; i++) {
            CHECK(a[i] >= 0x20 && a[i] < 0x7F && b[i] >= 0x20 && b[i] < 0x7F);
        }
    }
    lcd_render(LCD_SCREEN_WIFI, &big, a, b);
    CHECK(strncmp(b, "NO AP HEARD", 11) == 0);
    lcd_render(LCD_SCREEN_STATUS, &big, a, b);
    CHECK(strncmp(b, "WIFI ERROR", 10) == 0);
}

static void test_heartbeat(void)
{
    proto_identity_t id;
    proto_identity_init(&id, MAC, 0x0000abcd);
    char buf[WRAITH_MAX_MESSAGE];
    size_t n = proto_heartbeat(buf, sizeof buf, &id, 7, 5000, 180000, 6, 2);
    CHECK(n > 0 && buf[n - 1] == '\n');
    CHECK(strstr(buf, "\"event_id\":\"0000abcd-7\"") != NULL);
    CHECK(strstr(buf, "\"metadata\":{\"uptime_ms\":5000,\"free_heap\":180000,\"channel\":6,\"queue_drops\":2}}") != NULL);
    /* Channel outside 1..14 is omitted rather than sent invalid. */
    n = proto_heartbeat(buf, sizeof buf, &id, 8, 1, 1, 0, 0);
    CHECK(n > 0 && strstr(buf, "\"channel\"") == NULL);
}

static void test_frame_parser(void)
{
    /* Broadcast deauth from AA:BB:CC:DD:EE:FF, reason 7. */
    uint8_t deauth[26] = {0xC0, 0x00, 0x3A, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
                          0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0xAA, 0xBB, 0xCC, 0xDD,
                          0xEE, 0xFF, 0x10, 0x00, 0x07, 0x00};
    wlan_hdr_t h;
    CHECK(frame_parse_mgmt(deauth, sizeof deauth, &h) == FP_OK);
    CHECK(h.subtype == WLAN_MGMT_DEAUTH);
    CHECK(!h.protected_frame);
    CHECK(h.addr1[0] == 0xFF && h.addr2[5] == 0xFF);
    CHECK(h.seq_num == 1);
    CHECK(h.body_len == 2 && h.body[0] == 7);

    deauth[1] |= 0x40; /* Protected Frame bit (PMF-protected deauth) */
    CHECK(frame_parse_mgmt(deauth, sizeof deauth, &h) == FP_OK && h.protected_frame);

    for (size_t n = 0; n < IEEE80211_HDR_LEN; n++) {
        CHECK(frame_parse_mgmt(deauth, n, &h) == FP_TRUNCATED);
    }
    uint8_t data[24] = {0x08}; /* data frame */
    CHECK(frame_parse_mgmt(data, sizeof data, &h) == FP_UNSUPPORTED);
    uint8_t ver[24] = {0x01};
    CHECK(frame_parse_mgmt(ver, sizeof ver, &h) == FP_BAD_VERSION);
    uint8_t order[24] = {0x80, 0x80}; /* beacon with +HTC but no room for it */
    CHECK(frame_parse_mgmt(order, sizeof order, &h) == FP_TRUNCATED);
}

/* Emit lines for cross-validation with the Go reference validator. */
static int emit(void)
{
    proto_identity_t id;
    proto_identity_init(&id, MAC, 0x1234abcd);
    char buf[WRAITH_MAX_MESSAGE];
    const char *caps[] = {"wifi-2.4ghz", "promiscuous"};
    uint32_t seq = 0;
    size_t n;
    puts("I (312) boot: interleaved ESP-IDF log line");
    n = proto_status(buf, sizeof buf, &id, seq++, SENSOR_BOOTING, caps, 2, "starting \"wraith\"");
    fputs(n ? buf : "OVERFLOW\n", stdout);
    n = proto_heartbeat(buf, sizeof buf, &id, seq++, 5000, 181234, 6, 0);
    fputs(n ? buf : "OVERFLOW\n", stdout);
    n = proto_status(buf, sizeof buf, &id, seq++, SENSOR_ONLINE, caps, 2, NULL);
    fputs(n ? buf : "OVERFLOW\n", stdout);

    /* An SSID event built from raw bytes, as the beacon path will. */
    const uint8_t bssid[6] = {0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0x01};
    const uint8_t ssid[] = {'L', 'a', 'b', '"', 0xE1, 0x9E, 0x80};
    json_writer_t jw;
    jw_init(&jw, buf, sizeof buf);
    proto_begin(&jw, &id, EVT_AP_OBSERVED, seq++, 6, bssid);
    jw_key(&jw, "ssid");
    jw_string_bytes(&jw, ssid, sizeof ssid);
    jw_key(&jw, "ssid_hex");
    jw_hex(&jw, ssid, sizeof ssid);
    jw_kv_int(&jw, "rssi", -48);
    n = proto_end(&jw);
    fputs(n ? buf : "OVERFLOW\n", stdout);
    emit_wifi_samples();
    return 0;
}

int main(int argc, char **argv)
{
    if (argc > 1 && strcmp(argv[1], "--emit") == 0) {
        return emit();
    }
    test_identity();
    test_json_escaping();
    test_utf8();
    test_overflow();
    test_heartbeat();
    test_crc32();
    test_lcd_screens();
    test_frame_parser();
    run_wifi_tests();
    if (failures) {
        fprintf(stderr, "%d check(s) failed\n", failures);
        return 1;
    }
    puts("firmware host tests: all checks passed");
    return 0;
}
