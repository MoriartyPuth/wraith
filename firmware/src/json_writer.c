#include "json_writer.h"

#include <inttypes.h>
#include <stdio.h>
#include <string.h>

void jw_init(json_writer_t *jw, char *buf, size_t cap)
{
    memset(jw, 0, sizeof(*jw));
    jw->buf = buf;
    jw->cap = cap;
    jw->first[0] = true;
    if (cap > 0) {
        buf[0] = '\0';
    } else {
        jw->overflow = true;
    }
}

static void put(json_writer_t *jw, const char *s, size_t n)
{
    if (jw->overflow) {
        return;
    }
    if (jw->len + n + 1 > jw->cap) { /* keep room for NUL */
        jw->overflow = true;
        return;
    }
    memcpy(jw->buf + jw->len, s, n);
    jw->len += n;
    jw->buf[jw->len] = '\0';
}

static void putc_(json_writer_t *jw, char c) { put(jw, &c, 1); }

/* Emit a comma if this is not the first member/element at this depth. */
static void sep(json_writer_t *jw)
{
    if (!jw->first[jw->depth]) {
        putc_(jw, ',');
    }
    jw->first[jw->depth] = false;
}

static void value_prefix(json_writer_t *jw)
{
    if (jw->after_key) { /* value directly after a key: no separator */
        jw->after_key = false;
        return;
    }
    sep(jw);
}

static void open_(json_writer_t *jw, char c)
{
    value_prefix(jw);
    putc_(jw, c);
    if (jw->depth + 1 >= JW_MAX_DEPTH) {
        jw->overflow = true;
        return;
    }
    jw->depth++;
    jw->first[jw->depth] = true;
}

static void close_(json_writer_t *jw, char c)
{
    if (jw->depth == 0) {
        jw->overflow = true;
        return;
    }
    jw->depth--;
    putc_(jw, c);
}

void jw_object_begin(json_writer_t *jw) { open_(jw, '{'); }
void jw_object_end(json_writer_t *jw) { close_(jw, '}'); }
void jw_array_begin(json_writer_t *jw) { open_(jw, '['); }
void jw_array_end(json_writer_t *jw) { close_(jw, ']'); }

void jw_key(json_writer_t *jw, const char *key)
{
    sep(jw);
    putc_(jw, '"');
    put(jw, key, strlen(key));
    put(jw, "\":", 2);
    jw->after_key = true;
}

bool jw_utf8_valid(const uint8_t *s, size_t n)
{
    size_t i = 0;
    while (i < n) {
        uint8_t c = s[i];
        size_t extra;
        uint32_t cp;
        if (c < 0x80) {
            i++;
            continue;
        } else if ((c & 0xE0) == 0xC0) {
            extra = 1;
            cp = c & 0x1F;
        } else if ((c & 0xF0) == 0xE0) {
            extra = 2;
            cp = c & 0x0F;
        } else if ((c & 0xF8) == 0xF0) {
            extra = 3;
            cp = c & 0x07;
        } else {
            return false;
        }
        if (i + extra >= n) { /* truncated sequence */
            return false;
        }
        for (size_t k = 1; k <= extra; k++) {
            if ((s[i + k] & 0xC0) != 0x80) {
                return false;
            }
            cp = (cp << 6) | (s[i + k] & 0x3F);
        }
        /* Reject overlong encodings, surrogates and out-of-range code points. */
        if ((extra == 1 && cp < 0x80) || (extra == 2 && cp < 0x800) || (extra == 3 && cp < 0x10000) ||
            (cp >= 0xD800 && cp <= 0xDFFF) || cp > 0x10FFFF) {
            return false;
        }
        i += extra + 1;
    }
    return true;
}

static void write_escaped(json_writer_t *jw, const uint8_t *s, size_t n, bool utf8_ok)
{
    putc_(jw, '"');
    for (size_t i = 0; i < n; i++) {
        uint8_t c = s[i];
        char esc[8];
        switch (c) {
        case '"': put(jw, "\\\"", 2); break;
        case '\\': put(jw, "\\\\", 2); break;
        case '\n': put(jw, "\\n", 2); break;
        case '\r': put(jw, "\\r", 2); break;
        case '\t': put(jw, "\\t", 2); break;
        default:
            if (c < 0x20 || c == 0x7F) {
                snprintf(esc, sizeof esc, "\\u%04x", c);
                put(jw, esc, 6);
            } else if (c >= 0x80 && !utf8_ok) {
                putc_(jw, '?');
            } else {
                putc_(jw, (char)c);
            }
        }
    }
    putc_(jw, '"');
}

void jw_string(json_writer_t *jw, const char *s)
{
    value_prefix(jw);
    size_t n = strlen(s);
    write_escaped(jw, (const uint8_t *)s, n, jw_utf8_valid((const uint8_t *)s, n));
}

void jw_string_bytes(json_writer_t *jw, const uint8_t *s, size_t n)
{
    value_prefix(jw);
    write_escaped(jw, s, n, jw_utf8_valid(s, n));
}

void jw_hex(json_writer_t *jw, const uint8_t *s, size_t n)
{
    static const char hx[] = "0123456789abcdef";
    value_prefix(jw);
    putc_(jw, '"');
    for (size_t i = 0; i < n; i++) {
        char two[2] = {hx[s[i] >> 4], hx[s[i] & 0xF]};
        put(jw, two, 2);
    }
    putc_(jw, '"');
}

void jw_int(json_writer_t *jw, int64_t v)
{
    char tmp[24];
    value_prefix(jw);
    int n = snprintf(tmp, sizeof tmp, "%" PRId64, v);
    put(jw, tmp, (size_t)n);
}

void jw_uint(json_writer_t *jw, uint64_t v)
{
    char tmp[24];
    value_prefix(jw);
    int n = snprintf(tmp, sizeof tmp, "%" PRIu64, v);
    put(jw, tmp, (size_t)n);
}

void jw_bool(json_writer_t *jw, bool v)
{
    value_prefix(jw);
    if (v) {
        put(jw, "true", 4);
    } else {
        put(jw, "false", 5);
    }
}

void jw_kv_string(json_writer_t *jw, const char *k, const char *v) { jw_key(jw, k); jw_string(jw, v); }
void jw_kv_int(json_writer_t *jw, const char *k, int64_t v) { jw_key(jw, k); jw_int(jw, v); }
void jw_kv_uint(json_writer_t *jw, const char *k, uint64_t v) { jw_key(jw, k); jw_uint(jw, v); }
void jw_kv_bool(json_writer_t *jw, const char *k, bool v) { jw_key(jw, k); jw_bool(jw, v); }

void jw_raw(json_writer_t *jw, const char *s) { put(jw, s, strlen(s)); }
