/* Host tests for the Bluetooth build's pure modules: the radio-mode switch
 * state machine (radio_mode_core.c) and Bluetooth system events
 * (bt_protocol.c). Built at protocol v4 as a separate binary.
 * With --emit, prints sample lines for the Go validator. */
#include <stdio.h>
#include <string.h>

#include "bt_protocol.h"
#include "lcd_bt_screens.h"
#include "radio_mode.h"
#include "test_util.h"
#include "wraith_config.h"

int failures;

void run_ble_tests(void);
void emit_ble_samples(void);
void run_classic_tests(void);
void emit_classic_samples(void);

/* ---- capture sink ---- */
#define MAX_LINES 64
static char g_lines[MAX_LINES][WRAITH_MAX_MESSAGE];
static int g_n, g_overflow;
static uint32_t g_seq;
static proto_identity_t g_id;

static uint32_t cap_seq(void *ctx) { return g_seq++; }
static void cap_emit(const char *line, size_t len, void *ctx)
{
    if (len == 0) {
        g_overflow++;
        return;
    }
    if (g_n < MAX_LINES) {
        memcpy(g_lines[g_n], line, len);
        g_lines[g_n][len] = '\0';
        g_n++;
    }
}
static const event_sink_t g_sink = {.id = &g_id, .next_seq = cap_seq, .emit = cap_emit};
static const char *last(void) { return g_n ? g_lines[g_n - 1] : ""; }
static int count_with(const char *s)
{
    int c = 0;
    for (int i = 0; i < g_n; i++) {
        c += strstr(g_lines[i], s) != NULL;
    }
    return c;
}

/* ---- fake radio ---- */
static char g_calls[256];
static int g_fail_wifi_stop, g_fail_bt_start, g_fail_wifi_start, g_fail_bt_stop;
static int g_changes;
static radio_mode_t g_changed_to;
static void call(const char *c) { strncat(g_calls, c, sizeof g_calls - strlen(g_calls) - 1); }
static int f_wifi_stop(void) { call("wS "); return g_fail_wifi_stop ? -1 : 0; }
static int f_wifi_start(void) { call("wU "); return g_fail_wifi_start ? (g_fail_wifi_start--, -1) : 0; }
static int f_bt_start(void) { call("bU "); return g_fail_bt_start ? -1 : 0; }
static int f_bt_stop(void) { call("bS "); return g_fail_bt_stop ? -1 : 0; }
static void f_changed(radio_mode_t m, const char *why) { g_changes++; g_changed_to = m; }
static const radio_ops_t g_ops = {.wifi_stop = f_wifi_stop, .wifi_start = f_wifi_start,
                                  .bt_start = f_bt_start, .bt_stop = f_bt_stop, .mode_changed = f_changed};

static void reset(uint32_t max_bt_ms)
{
    g_n = g_overflow = 0;
    g_seq = 0;
    g_calls[0] = '\0';
    g_fail_wifi_stop = g_fail_bt_start = g_fail_wifi_start = g_fail_bt_stop = 0;
    g_changes = 0;
    radio_init(&g_sink, &g_ops, max_bt_ms);
}

static void test_parse(void)
{
    reset(0);
    CHECK(!radio_command("PING", 0));
    CHECK(!radio_command("TX_STATUS", 0));
    CHECK(!radio_command("MODEX WIFI", 0));
    CHECK(!radio_command("HELLO", 0));
    CHECK(g_n == 0); /* not ours: nothing emitted */
    CHECK(radio_command("MODE STATUS", 0));
    CHECK(radio_command("  MODE\tWIFI", 0));
    CHECK(radio_command("MODE BLUETOOTH", 0));
    CHECK(g_n == 0 && strcmp(g_calls, "") == 0); /* recorded only: nothing happens before a tick */
    const char *bad[] = {"MODE", "MODE bluetooth", "MODE WIFI extra", "MODE BT"};
    for (size_t i = 0; i < sizeof bad / sizeof bad[0]; i++) {
        int before = g_n;
        CHECK(radio_command(bad[i], 0));
        CHECK(g_n == before + 1 && strstr(last(), "\"code\":\"bad_command\"") != NULL);
    }
    CHECK(radio_failure_count() == 4);
}

static void test_switch_and_back(void)
{
    reset(0);
    radio_command("MODE BLUETOOTH", 1000);
    radio_tick(1000, true);
    CHECK(strcmp(g_calls, "wS bU ") == 0); /* Wi-Fi fully stopped before Bluetooth starts */
    CHECK(radio_current() == RADIO_BLUETOOTH && g_changes == 1 && g_changed_to == RADIO_BLUETOOTH);
    CHECK(strstr(last(), "\"event_type\":\"bluetooth.system.status\"") != NULL);
    CHECK(strstr(last(), "\"mode\":\"bluetooth\"") && strstr(last(), "\"state\":\"idle\""));
    CHECK(strstr(last(), "\"version\":4") != NULL);

    radio_command("MODE BLUETOOTH", 1100); /* already there: report, do nothing */
    radio_tick(1100, true);
    CHECK(strcmp(g_calls, "wS bU ") == 0 && strstr(last(), "already in Bluetooth mode"));

    radio_command("MODE STATUS", 1200);
    radio_tick(1200, true);
    CHECK(strstr(last(), "\"mode\":\"bluetooth\""));

    radio_command("MODE WIFI", 1300);
    radio_tick(1300, true);
    CHECK(strcmp(g_calls, "wS bU bS wU ") == 0); /* Bluetooth down before Wi-Fi comes back */
    CHECK(radio_current() == RADIO_WIFI && g_changes == 2 && radio_switch_count() == 2);
    CHECK(strstr(last(), "\"mode\":\"wifi\"") && strstr(last(), "Wi-Fi mode (operator)"));
    CHECK(radio_failure_count() == 0);

    radio_command("MODE WIFI", 1400);
    radio_tick(1400, true);
    CHECK(strstr(last(), "already in Wi-Fi mode") && g_changes == 2);
}

static void test_auto_return(void)
{
    /* Host link lost while in Bluetooth mode: back to the Wi-Fi baseline. */
    reset(0);
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    radio_tick(500, true);
    CHECK(radio_current() == RADIO_BLUETOOTH);
    radio_tick(600, false);
    CHECK(radio_current() == RADIO_WIFI && strstr(last(), "host link lost"));
    CHECK(strcmp(g_calls, "wS bU bS wU ") == 0);

    /* Session limit. */
    reset(10000);
    radio_command("MODE BLUETOOTH", 5000);
    radio_tick(5000, true);
    radio_tick(14999, true);
    CHECK(radio_current() == RADIO_BLUETOOTH);
    radio_tick(15000, true);
    CHECK(radio_current() == RADIO_WIFI && strstr(last(), "session limit"));

    /* Limit across the 32-bit millisecond wrap. */
    reset(10000);
    radio_command("MODE BLUETOOTH", 0xFFFFF000u);
    radio_tick(0xFFFFF000u, true);
    radio_tick(0x00000100u, true); /* 4.35 s later */
    CHECK(radio_current() == RADIO_BLUETOOTH);
    radio_tick(0x00001800u, true); /* 10.2 s later */
    CHECK(radio_current() == RADIO_WIFI);

    /* Wi-Fi mode ignores host loss (it is the baseline). */
    reset(0);
    radio_tick(0, false);
    CHECK(radio_current() == RADIO_WIFI && strcmp(g_calls, "") == 0 && g_n == 0);
}

/* The mode must be re-announced whenever the host link comes (back) up: a
 * status emitted while Core was disconnected (e.g. the automatic return to
 * Wi-Fi after host loss) is lost, and Core would keep showing a stale mode. */
/* LCD in Bluetooth mode: 16 printable ASCII columns, the Classic TX state
 * always visible, countdown to the automatic return to Wi-Fi. */
static void lcd_ok(const char *l) { CHECK(strlen(l) == LCD_COLS); for (int i = 0; i < LCD_COLS; i++) CHECK(l[i] >= 0x20 && l[i] < 0x7F); }
static void test_lcd_bt(void)
{
    char a[LCD_COLS + 1], b[LCD_COLS + 1];
    lcd_snapshot_t s = {.host_connected = true, .sensor_id = "nano-70b2a8"};
    lcd_bt_info_t bt = {.classic_on = true, .scanning = true, .ble = 16, .classic = 1, .best_ble_rssi = -45,
                        .best_classic_rssi = -90, .remaining_ms = 754000};
    lcd_bt_screens_render(LCD_BT_MODE, &bt, &s, a, b);
    CHECK(strcmp(a, "BT MODE BLE+CL  ") == 0 && strcmp(b, "WIFI BACK 12:34 ") == 0);
    lcd_bt_screens_render(LCD_BT_COUNTS, &bt, &s, a, b);
    CHECK(strcmp(a, "BLE:16 CL:1     ") == 0 && strcmp(b, "CLASSIC TX ON   ") == 0);
    lcd_bt_screens_render(LCD_BT_SIGNAL, &bt, &s, a, b);
    CHECK(strcmp(a, "BEST BLE:-45    ") == 0 && strcmp(b, "BEST CL:-90     ") == 0);
    lcd_bt_screens_render(LCD_BT_HOST, &bt, &s, a, b);
    CHECK(strcmp(a, "HOST:CONNECTED  ") == 0 && strncmp(b, "nano-70b2a8", 11) == 0);

    /* BLE only, nothing heard, no limit, partial second rounds up. */
    bt = (lcd_bt_info_t){.scanning = true, .ble = 0, .classic = 0, .remaining_ms = UINT32_MAX};
    lcd_bt_screens_render(LCD_BT_MODE, &bt, &s, a, b);
    CHECK(strcmp(a, "BT MODE BLE     ") == 0 && strcmp(b, "WIFI PAUSED     ") == 0);
    lcd_bt_screens_render(LCD_BT_COUNTS, &bt, &s, a, b);
    CHECK(strcmp(b, "BLE PASSIVE     ") == 0);
    lcd_bt_screens_render(LCD_BT_SIGNAL, &bt, &s, a, b);
    CHECK(strcmp(a, "NO BLE HEARD    ") == 0 && strcmp(b, "CLASSIC OFF     ") == 0);
    bt.remaining_ms = 1;
    lcd_bt_screens_render(LCD_BT_MODE, &bt, &s, a, b);
    CHECK(strcmp(b, "WIFI BACK 00:01 ") == 0);

    /* Extremes stay 16 columns: huge counts, unknown counts, RSSI floor,
     * absurd countdown, every screen index including out of range. */
    bt = (lcd_bt_info_t){.classic_on = true, .ble = 123456, .classic = -1, .best_ble_rssi = -300,
                         .best_classic_rssi = -128, .remaining_ms = UINT32_MAX - 1};
    for (int scr = 0; scr <= LCD_BT_SCREEN_COUNT; scr++) {
        lcd_bt_screens_render((lcd_bt_screen_t)scr, &bt, &s, a, b);
        lcd_ok(a);
        lcd_ok(b);
    }
    lcd_bt_screens_render(LCD_BT_COUNTS, &bt, &s, a, b);
    CHECK(strcmp(a, "BLE:999 CL:-    ") == 0);
    lcd_bt_screens_render(LCD_BT_SIGNAL, &bt, &s, a, b);
    CHECK(strcmp(a, "BEST BLE:-127   ") == 0 && strcmp(b, "BEST CL:-127    ") == 0);
    lcd_bt_screens_render(LCD_BT_MODE, &bt, &s, a, b);
    CHECK(strcmp(b, "WIFI BACK 99:59 ") == 0);
}

/* Countdown to the automatic return to Wi-Fi. */
static void test_remaining(void)
{
    reset(15 * 60 * 1000);
    CHECK(radio_bt_remaining_ms(0) == UINT32_MAX); /* Wi-Fi mode */
    radio_command("MODE BLUETOOTH", 0xFFFFF000u);
    radio_tick(0xFFFFF000u, true);
    CHECK(radio_bt_remaining_ms(0xFFFFF000u) == 15 * 60 * 1000);
    CHECK(radio_bt_remaining_ms(0xFFFFF000u + 60000) == 14 * 60 * 1000); /* across the timer wrap */
    CHECK(radio_bt_remaining_ms(0xFFFFF000u + 15 * 60 * 1000 + 5) == 0);
    reset(0);
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    CHECK(radio_bt_remaining_ms(123456) == UINT32_MAX); /* no limit */
}

static void test_announce_on_host_connect(void)
{
    reset(0);
    radio_tick(0, false);
    CHECK(g_n == 0); /* nobody listening: nothing announced */
    radio_tick(100, true);
    CHECK(g_n == 1 && strstr(last(), "\"mode\":\"wifi\"") && strstr(last(), "host connected"));
    radio_tick(200, true);
    CHECK(g_n == 1); /* only on the rising edge */

    radio_command("MODE BLUETOOTH", 300);
    radio_tick(300, true);
    CHECK(radio_current() == RADIO_BLUETOOTH);
    radio_tick(400, false); /* host lost: back to Wi-Fi (this status is lost on the wire) */
    CHECK(radio_current() == RADIO_WIFI);
    int before = g_n;
    radio_tick(500, false);
    CHECK(g_n == before);
    radio_tick(600, true); /* Core is back: it learns the real mode */
    CHECK(g_n == before + 1 && strstr(last(), "\"mode\":\"wifi\"") && strstr(last(), "host connected"));
}

static int g_polls;
static long g_ble = 7;
static void f_poll(uint32_t now) { g_polls++; }
static const char *f_state(void) { return "scanning_ble"; }
static long f_count(void) { return g_ble; }

/* Scanning hooks: polled every tick in Bluetooth mode only; status reports
 * the scan state and device count, and repeats every 60 s. */
static void test_scan_hooks(void)
{
    radio_ops_t ops = g_ops;
    ops.bt_poll = f_poll, ops.bt_state = f_state, ops.ble_count = f_count;
    reset(0);
    radio_init(&g_sink, &ops, 0);
    g_polls = 0;
    radio_tick(0, true);
    radio_tick(100, true);
    CHECK(g_polls == 0); /* Wi-Fi mode: never polled */
    CHECK(strstr(last(), "\"state\":\"idle\"") && !strstr(last(), "ble_devices"));
    radio_command("MODE BLUETOOTH", 200);
    radio_tick(200, true);
    CHECK(strstr(last(), "\"state\":\"scanning_ble\"") && strstr(last(), "\"ble_devices\":7"));
    int before = g_n;
    radio_tick(300, true);
    radio_tick(200 + RADIO_BT_STATUS_INTERVAL_MS - 1, true);
    CHECK(g_n == before && g_polls >= 2);
    g_ble = 9;
    radio_tick(200 + RADIO_BT_STATUS_INTERVAL_MS, true);
    CHECK(g_n == before + 1 && strstr(last(), "\"ble_devices\":9"));
    radio_command("MODE WIFI", 70000);
    radio_tick(70000, true);
    int polls = g_polls;
    radio_tick(70100, true);
    CHECK(g_polls == polls && !strstr(last(), "ble_devices"));
}

/* MODE BLUETOOTH CLASSIC: Classic inquiry (transmits) only on request;
 * on/off without leaving Bluetooth mode; always off after a switch. */
static int g_classic_calls, g_classic_on, g_fail_classic;
static int f_classic(bool on)
{
    g_classic_calls++;
    if (g_fail_classic) return -1;
    g_classic_on = on;
    return 0;
}
static long f_ccount(void) { return 3; }
static void test_classic_mode(void)
{
    radio_ops_t ops = g_ops;
    ops.bt_classic = f_classic, ops.classic_count = f_ccount;
    reset(0);
    radio_init(&g_sink, &ops, 0);
    g_classic_calls = g_classic_on = g_fail_classic = 0;

    /* Plain MODE BLUETOOTH never touches Classic. */
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    CHECK(radio_current() == RADIO_BLUETOOTH && g_classic_calls == 0 && !radio_classic_on());
    CHECK(strstr(last(), "BLE passive") && strstr(last(), "\"classic_devices\":3"));

    /* Turn it on in place: no switch. */
    int changes = g_changes;
    radio_command("MODE BLUETOOTH CLASSIC", 100);
    radio_tick(100, true);
    CHECK(g_classic_calls == 1 && g_classic_on && radio_classic_on() && g_changes == changes);
    CHECK(strstr(last(), "Classic inquiry (transmits)"));
    radio_command("  MODE  BLUETOOTH   CLASSIC  ", 150); /* already on */
    radio_tick(150, true);
    CHECK(g_classic_calls == 1 && strstr(last(), "already in Bluetooth mode"));
    /* ...and off again. */
    radio_command("MODE BLUETOOTH", 200);
    radio_tick(200, true);
    CHECK(g_classic_calls == 2 && !g_classic_on && !radio_classic_on() && strstr(last(), "BLE passive"));

    /* From Wi-Fi in one command; back to Wi-Fi clears it (bt_stop ends it). */
    radio_command("MODE WIFI", 300);
    radio_tick(300, true);
    g_calls[0] = '\0';
    radio_command("MODE BLUETOOTH CLASSIC", 400);
    radio_tick(400, true);
    CHECK(strcmp(g_calls, "wS bU ") == 0 && radio_classic_on() && count_with("Classic inquiry (transmits)") >= 2);
    radio_command("MODE WIFI", 500);
    radio_tick(500, true);
    CHECK(!radio_classic_on() && !strstr(last(), "classic_devices"));
    radio_command("MODE BLUETOOTH", 600);
    radio_tick(600, true);
    CHECK(!radio_classic_on());

    /* Enabling fails: error, Bluetooth mode stays (BLE only). */
    g_fail_classic = 1;
    radio_command("MODE BLUETOOTH CLASSIC", 700);
    radio_tick(700, true);
    CHECK(radio_current() == RADIO_BLUETOOTH && !radio_classic_on() && count_with("\"code\":\"classic_failed\"") == 1);
    g_fail_classic = 0;

    /* Malformed. */
    int errs = count_with("bad_command");
    radio_command("MODE BLUETOOTH CLASSIC NOW", 800);
    radio_command("MODE BLUETOOTH CLASSICX", 800);
    radio_command("MODE WIFI CLASSIC", 800);
    CHECK(count_with("bad_command") == errs + 3);

    /* Firmware without Classic: rejected, nothing happens. */
    reset(0);
    radio_command("MODE BLUETOOTH CLASSIC", 0);
    radio_tick(0, true);
    CHECK(radio_current() == RADIO_WIFI && count_with("\"code\":\"classic_unsupported\"") == 1);
}

static void test_failures_fall_back_to_wifi(void)
{
    /* Bluetooth fails to start: release it and restart Wi-Fi. */
    reset(0);
    g_fail_bt_start = 1;
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    CHECK(strcmp(g_calls, "wS bU bS wU ") == 0);
    CHECK(radio_current() == RADIO_WIFI && g_changes == 0);
    CHECK(count_with("\"code\":\"bt_start_failed\"") == 1 && strstr(last(), "\"mode\":\"wifi\""));

    /* Wi-Fi refuses to stop: stay (and make sure it is running). */
    reset(0);
    g_fail_wifi_stop = 1;
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    CHECK(strcmp(g_calls, "wS wU ") == 0 && radio_current() == RADIO_WIFI);
    CHECK(count_with("\"code\":\"wifi_stop_failed\"") == 1);

    /* Wi-Fi does not come back after Bluetooth: report error, retry every
     * WRAITH_WIFI_RESTART_RETRY_MS until it does. */
    reset(0);
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    g_fail_wifi_start = 2; /* the restart and the first retry fail */
    radio_command("MODE WIFI", 100);
    radio_tick(100, true);
    CHECK(radio_current() == RADIO_WIFI && count_with("\"code\":\"wifi_start_failed\"") == 1);
    CHECK(strstr(last(), "\"state\":\"error\""));
    g_calls[0] = '\0';
    radio_tick(100 + WRAITH_WIFI_RESTART_RETRY_MS - 1, true);
    CHECK(strcmp(g_calls, "") == 0); /* not yet */
    radio_tick(100 + WRAITH_WIFI_RESTART_RETRY_MS, true);
    CHECK(strcmp(g_calls, "wU ") == 0 && strstr(last(), "\"state\":\"error\"") != NULL); /* still failing, no new status */
    radio_tick(100 + 2 * WRAITH_WIFI_RESTART_RETRY_MS, true);
    CHECK(strcmp(g_calls, "wU wU ") == 0 && strstr(last(), "restarted") && strstr(last(), "\"state\":\"idle\""));

    /* Bluetooth refuses to stop: Wi-Fi still comes back. */
    reset(0);
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    g_fail_bt_stop = 1;
    radio_command("MODE WIFI", 1);
    radio_tick(1, true);
    CHECK(radio_current() == RADIO_WIFI && count_with("\"code\":\"bt_stop_failed\"") == 1);
    CHECK(strcmp(g_calls, "wS bU bS wU ") == 0);
}

static void test_emitters(void)
{
    reset(0);
    char longmsg[400];
    memset(longmsg, 'x', sizeof longmsg - 1);
    longmsg[sizeof longmsg - 1] = '\0';
    bt_emit_status(&g_sink, "idle", "wifi", 3, -1, longmsg);
    CHECK(g_overflow == 0 && strstr(last(), "\"classic_devices\":3") && !strstr(last(), "ble_devices"));
    CHECK(strstr(last(), "\"message\":\"") && strlen(strstr(last(), "\"message\":\"")) < 128 + 40);
    bt_emit_error(&g_sink, longmsg, longmsg);
    CHECK(g_overflow == 0 && strstr(last(), "\"code\":\""));
    /* Uninitialised sink: no crash, nothing emitted. */
    int before = g_n;
    bt_emit_status(&(event_sink_t){0}, "idle", "wifi", -1, -1, NULL);
    CHECK(g_n == before);
}

static void emit_samples(void)
{
    reset(5000);
    radio_command("MODE BLUETOOTH", 0);
    radio_tick(0, true);
    radio_command("MODE STATUS", 1);
    radio_tick(1, true);
    radio_tick(2, false); /* host lost -> Wi-Fi */
    radio_command("MODE nonsense", 3);
    g_fail_bt_start = 1;
    radio_command("MODE BLUETOOTH", 4);
    radio_tick(4, true);
    char longmsg[400];
    memset(longmsg, 'y', sizeof longmsg - 1);
    longmsg[sizeof longmsg - 1] = '\0';
    bt_emit_status(&g_sink, "scanning_dual", "bluetooth", 12, 40, longmsg);
    bt_emit_error(&g_sink, longmsg, longmsg);
    for (int i = 0; i < g_n; i++) {
        fputs(g_lines[i], stdout);
    }
}

int main(int argc, char **argv)
{
    static const uint8_t mac[6] = {0x38, 0x3E, 0x51, 0x70, 0xB2, 0xA8};
    proto_identity_init(&g_id, mac, 0xb7b70005u);
    if (argc > 1 && strcmp(argv[1], "--emit") == 0) {
        emit_samples();
        emit_ble_samples();
        emit_classic_samples();
        return 0;
    }
    test_parse();
    test_switch_and_back();
    test_auto_return();
    test_announce_on_host_connect();
    test_scan_hooks();
    test_failures_fall_back_to_wifi();
    test_emitters();
    test_classic_mode();
    test_lcd_bt();
    test_remaining();
    run_ble_tests();
    run_classic_tests();
    if (failures) {
        fprintf(stderr, "bluetooth host tests: %d FAILED\n", failures);
        return 1;
    }
    printf("bluetooth host tests: all passed\n");
    return 0;
}
