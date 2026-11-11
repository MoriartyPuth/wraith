// Package config loads WRAITH Core configuration from (in increasing
// precedence) defaults, an optional JSON file, environment variables (WRAITH_*)
// and command-line flags.
package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

// Config is the full Core configuration.
type Config struct {
	ListenAddr   string   `json:"listen_addr"` // keep on loopback unless you know why
	DBPath       string   `json:"db_path"`
	LogLevel     string   `json:"log_level"`   // debug, info, warn, error
	LogFormat    string   `json:"log_format"`  // text, json
	SerialPort   string   `json:"serial_port"` // e.g. /dev/ttyUSB0; empty = ESP32 provider disabled
	SerialBaud   int      `json:"serial_baud"`
	DashboardDir string   `json:"dashboard_dir"` // optional built dashboard to serve at /
	AllowedHosts []string `json:"allowed_hosts"` // extra Host headers accepted (DNS-rebinding guard)
}

// Default returns the built-in defaults.
func Default() Config {
	data := os.Getenv("XDG_DATA_HOME")
	if data == "" {
		home, _ := os.UserHomeDir()
		data = filepath.Join(home, ".local", "share")
	}
	return Config{
		ListenAddr: "127.0.0.1:8088",
		DBPath:     filepath.Join(data, "wraith", "wraith.db"),
		LogLevel:   "info",
		LogFormat:  "text",
		SerialBaud: 115200,
	}
}

// Load builds the configuration from file, env and args (args excludes argv[0]).
func Load(args []string) (Config, error) {
	cfg := Default()
	fs := flag.NewFlagSet("wraith", flag.ContinueOnError)
	file := fs.String("config", os.Getenv("WRAITH_CONFIG"), "path to JSON config file")
	listen := fs.String("listen", "", "HTTP listen address")
	db := fs.String("db", "", "SQLite database path")
	serial := fs.String("serial", "", "ESP32 serial port (e.g. /dev/ttyUSB0)")
	baud := fs.Int("baud", 0, "serial baud rate")
	level := fs.String("log-level", "", "log level: debug|info|warn|error")
	format := fs.String("log-format", "", "log format: text|json")
	dash := fs.String("dashboard-dir", "", "serve a built dashboard from this directory")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return cfg, fmt.Errorf("config file: %w", err)
		}
		dec := json.NewDecoder(bytesReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("config file %s: %w", *file, err)
		}
	}

	envStr(&cfg.ListenAddr, "WRAITH_LISTEN")
	envStr(&cfg.DBPath, "WRAITH_DB")
	envStr(&cfg.LogLevel, "WRAITH_LOG_LEVEL")
	envStr(&cfg.LogFormat, "WRAITH_LOG_FORMAT")
	envStr(&cfg.SerialPort, "WRAITH_SERIAL")
	envStr(&cfg.DashboardDir, "WRAITH_DASHBOARD_DIR")
	if v := os.Getenv("WRAITH_SERIAL_BAUD"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("WRAITH_SERIAL_BAUD: %w", err)
		}
		cfg.SerialBaud = n
	}

	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&cfg.ListenAddr, *listen)
	set(&cfg.DBPath, *db)
	set(&cfg.SerialPort, *serial)
	set(&cfg.LogLevel, *level)
	set(&cfg.LogFormat, *format)
	set(&cfg.DashboardDir, *dash)
	if *baud != 0 {
		cfg.SerialBaud = *baud
	}
	return cfg, cfg.Validate()
}

// Validate checks the configuration for obvious mistakes.
func (c Config) Validate() error {
	var errs []error
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		errs = append(errs, fmt.Errorf("listen_addr: %w", err))
	}
	if c.DBPath == "" {
		errs = append(errs, errors.New("db_path: empty"))
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("log_level: %q", c.LogLevel))
	}
	switch c.LogFormat {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("log_format: %q", c.LogFormat))
	}
	if c.SerialBaud <= 0 {
		errs = append(errs, fmt.Errorf("serial_baud: %d", c.SerialBaud))
	}
	return errors.Join(errs...)
}

func envStr(dst *string, key string) {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		*dst = v
	}
}
