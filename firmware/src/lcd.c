#include "lcd.h"

#include <string.h>

#include "driver/i2c_master.h"
#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "wraith_config.h"

static const char *TAG = "lcd";

/* PCF8574 -> HD44780 wiring used by nearly all backpacks:
 * P0=RS P1=RW P2=E P3=backlight P4..P7=D4..D7. */
#define PIN_RS 0x01
#define PIN_EN 0x04
#define PIN_BL 0x08

#define SCREEN_SECONDS 4

/* Return the error from an I2C step without logging (the caller logs once). */
#define TRY(expr)                    \
    do {                             \
        esp_err_t err_ = (expr);     \
        if (err_ != ESP_OK) {        \
            return err_;             \
        }                            \
    } while (0)

static i2c_master_bus_handle_t s_bus;
static i2c_master_dev_handle_t s_dev;
static int s_addr;
static lcd_snapshot_fn s_snapshot;

/* Append one 4-bit transfer (with enable pulse) to buf. */
static size_t nibble(uint8_t *buf, size_t n, uint8_t hi4, uint8_t rs)
{
    uint8_t b = (hi4 & 0xF0) | PIN_BL | rs;
    buf[n++] = b | PIN_EN;
    buf[n++] = b;
    return n;
}

static size_t byte_out(uint8_t *buf, size_t n, uint8_t v, uint8_t rs)
{
    n = nibble(buf, n, v & 0xF0, rs);
    return nibble(buf, n, (uint8_t)(v << 4), rs);
}

static esp_err_t send(const uint8_t *buf, size_t n)
{
    return i2c_master_transmit(s_dev, buf, n, 50);
}

static esp_err_t command(uint8_t c)
{
    uint8_t buf[4];
    esp_err_t err = send(buf, byte_out(buf, 0, c, 0));
    if (c == 0x01 || c == 0x02) {
        vTaskDelay(pdMS_TO_TICKS(3)); /* clear/home take ~1.5 ms */
    }
    return err;
}

static esp_err_t write_line(int row, const char *text)
{
    uint8_t buf[4 + LCD_COLS * 4];
    size_t n = byte_out(buf, 0, (uint8_t)(0x80 | (row ? 0x40 : 0x00)), 0);
    for (int i = 0; i < LCD_COLS; i++) {
        n = byte_out(buf, n, (uint8_t)text[i], PIN_RS);
    }
    return send(buf, n);
}

static esp_err_t hd44780_init(void)
{
    uint8_t buf[2];
    vTaskDelay(pdMS_TO_TICKS(50));
    /* Reset into 4-bit mode (HD44780 datasheet, "initializing by instruction"). */
    for (int i = 0; i < 3; i++) {
        TRY(send(buf, nibble(buf, 0, 0x30, 0)));
        vTaskDelay(pdMS_TO_TICKS(i == 0 ? 5 : 1));
    }
    TRY(send(buf, nibble(buf, 0, 0x20, 0)));
    vTaskDelay(pdMS_TO_TICKS(1));
    const uint8_t seq[] = {0x28 /* 4-bit, 2 lines, 5x8 */, 0x0C /* display on, no cursor */,
                           0x01 /* clear */, 0x06 /* increment, no shift */};
    for (size_t i = 0; i < sizeof seq; i++) {
        TRY(command(seq[i]));
    }
    return ESP_OK;
}

esp_err_t lcd_init(void)
{
    i2c_master_bus_config_t bus = {
        .i2c_port = WRAITH_LCD_I2C_PORT,
        .sda_io_num = WRAITH_LCD_SDA_GPIO,
        .scl_io_num = WRAITH_LCD_SCL_GPIO,
        .clk_source = I2C_CLK_SRC_DEFAULT,
        .glitch_ignore_cnt = 7,
        .flags.enable_internal_pullup = 1, /* weak 3.3 V pull-ups; backpacks usually add their own */
    };
    esp_err_t err = i2c_new_master_bus(&bus, &s_bus);
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "i2c bus: %s", esp_err_to_name(err));
        return err;
    }
    /* PCF8574 answers at 0x20-0x27, PCF8574A at 0x38-0x3F; 0x27 and 0x3F are
     * the usual factory settings, so try them first. */
    static const uint8_t candidates[] = {0x27, 0x3F, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26,
                                         0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E};
    for (size_t i = 0; i < sizeof candidates; i++) {
        if (i2c_master_probe(s_bus, candidates[i], 20) == ESP_OK) {
            s_addr = candidates[i];
            break;
        }
    }
    if (s_addr == 0) {
        ESP_LOGW(TAG, "no PCF8574 backpack found on SDA=%d SCL=%d", WRAITH_LCD_SDA_GPIO, WRAITH_LCD_SCL_GPIO);
        i2c_del_master_bus(s_bus);
        s_bus = NULL;
        return ESP_ERR_NOT_FOUND;
    }
    i2c_device_config_t dev = {
        .dev_addr_length = I2C_ADDR_BIT_LEN_7,
        .device_address = (uint16_t)s_addr,
        .scl_speed_hz = WRAITH_LCD_I2C_HZ,
    };
    err = i2c_master_bus_add_device(s_bus, &dev, &s_dev);
    if (err == ESP_OK) {
        err = hd44780_init();
    }
    if (err != ESP_OK) {
        ESP_LOGE(TAG, "init at 0x%02x: %s", s_addr, esp_err_to_name(err));
        return err;
    }
    ESP_LOGW(TAG, "LCD at I2C 0x%02x", s_addr);
    write_line(0, "WRAITH          ");
    write_line(1, "Initializing... ");
    return ESP_OK;
}

static void lcd_task(void *arg)
{
    lcd_snapshot_t snap;
    char l1[LCD_COLS + 1], l2[LCD_COLS + 1];
    int tick = 0, failures = 0;
    for (;;) {
        lcd_screen_t screen = (lcd_screen_t)((tick / SCREEN_SECONDS) % LCD_SCREEN_COUNT);
        memset(&snap, 0, sizeof snap);
        s_snapshot(&snap);
        lcd_render(screen, &snap, l1, l2);
        if (write_line(0, l1) != ESP_OK || write_line(1, l2) != ESP_OK) {
            /* Display unplugged or bus glitch: back off, then re-initialise. */
            if (++failures % 10 == 1) {
                ESP_LOGW(TAG, "write failed (%d); retrying", failures);
            }
            vTaskDelay(pdMS_TO_TICKS(5000));
            hd44780_init();
            continue;
        }
        failures = 0;
        tick++;
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}

esp_err_t lcd_start(lcd_snapshot_fn snapshot)
{
    s_snapshot = snapshot;
    if (xTaskCreate(lcd_task, "wraith_lcd", 3072, NULL, 2, NULL) != pdPASS) {
        return ESP_ERR_NO_MEM;
    }
    return ESP_OK;
}

int lcd_address(void) { return s_addr; }
