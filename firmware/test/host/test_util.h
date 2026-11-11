#pragma once
#include <stdio.h>

extern int failures;
#define CHECK(cond)                                                                \
    do {                                                                           \
        if (!(cond)) {                                                             \
            fprintf(stderr, "FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond);        \
            failures++;                                                            \
        }                                                                          \
    } while (0)

void run_wifi_tests(void);
void emit_wifi_samples(void);
