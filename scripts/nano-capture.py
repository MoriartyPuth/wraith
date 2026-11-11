#!/usr/bin/env python3
"""Reset WRAITH Nano via RTS and capture its serial output for N seconds.

Development helper only (WRAITH Core has no Python runtime dependency).
Uses only the standard library. Writes raw output to stdout:

    scripts/nano-capture.py [port] [seconds] | (cd core && go run ./cmd/wraith validate)

Typical ESP32 dev boards wire RTS -> EN (reset) and DTR -> GPIO0 (boot mode).
We hold DTR released (normal boot) and pulse RTS to reset.
"""
import fcntl
import os
import struct
import sys
import termios
import time

port = sys.argv[1] if len(sys.argv) > 1 else "/dev/ttyUSB0"
seconds = float(sys.argv[2]) if len(sys.argv) > 2 else 20.0

fd = os.open(port, os.O_RDWR | os.O_NOCTTY | os.O_NONBLOCK)
try:
    attrs = termios.tcgetattr(fd)
    attrs[0] = 0                                  # iflag: raw
    attrs[1] = 0                                  # oflag
    attrs[2] = termios.CS8 | termios.CREAD | termios.CLOCAL
    attrs[3] = 0                                  # lflag: no echo/canonical
    attrs[4] = attrs[5] = termios.B115200
    termios.tcsetattr(fd, termios.TCSANOW, attrs)

    def modem(bits, on):
        req = termios.TIOCMBIS if on else termios.TIOCMBIC
        fcntl.ioctl(fd, req, struct.pack("I", bits))

    modem(termios.TIOCM_DTR, False)   # GPIO0 high -> normal boot
    modem(termios.TIOCM_RTS, True)    # EN low -> reset
    time.sleep(0.1)
    termios.tcflush(fd, termios.TCIFLUSH)
    modem(termios.TIOCM_RTS, False)   # release reset

    end = time.monotonic() + seconds
    out = sys.stdout.buffer
    while time.monotonic() < end:
        try:
            chunk = os.read(fd, 4096)
        except BlockingIOError:
            time.sleep(0.02)
            continue
        if chunk:
            out.write(chunk)
            out.flush()
finally:
    os.close(fd)
