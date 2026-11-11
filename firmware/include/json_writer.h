/* Minimal allocation-free JSON writer into a caller-provided buffer.
 * Hardware-independent. On overflow the writer latches `overflow` and stops
 * writing; callers check it once at the end. */
#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#define JW_MAX_DEPTH 6

typedef struct {
    char *buf;
    size_t cap; /* bytes available including room for the terminating NUL */
    size_t len;
    bool overflow;
    uint8_t depth;
    bool after_key; /* next value follows a key */
    bool first[JW_MAX_DEPTH]; /* no comma needed before next member/element */
} json_writer_t;

void jw_init(json_writer_t *jw, char *buf, size_t cap);
void jw_object_begin(json_writer_t *jw); /* value position: { */
void jw_object_end(json_writer_t *jw);
void jw_array_begin(json_writer_t *jw);
void jw_array_end(json_writer_t *jw);
void jw_key(json_writer_t *jw, const char *key); /* key must be plain ASCII */

void jw_string(json_writer_t *jw, const char *s);
/* Write raw bytes (e.g. an SSID) as a JSON string. Valid UTF-8 is kept;
 * if the bytes are not valid UTF-8, every byte >= 0x80 becomes '?'.
 * Control characters are \u-escaped. */
void jw_string_bytes(json_writer_t *jw, const uint8_t *s, size_t n);
void jw_hex(json_writer_t *jw, const uint8_t *s, size_t n); /* lowercase hex string */
void jw_int(json_writer_t *jw, int64_t v);
void jw_uint(json_writer_t *jw, uint64_t v);
void jw_bool(json_writer_t *jw, bool v);

/* Convenience: key + value. */
void jw_kv_string(json_writer_t *jw, const char *k, const char *v);
void jw_kv_int(json_writer_t *jw, const char *k, int64_t v);
void jw_kv_uint(json_writer_t *jw, const char *k, uint64_t v);
void jw_kv_bool(json_writer_t *jw, const char *k, bool v);

/* Append raw text (no separators). Used for the framing newline. */
void jw_raw(json_writer_t *jw, const char *s);

bool jw_utf8_valid(const uint8_t *s, size_t n);
