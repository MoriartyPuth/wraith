package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrecedence(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.json")
	os.WriteFile(f, []byte(`{"listen_addr":"127.0.0.1:1111","serial_port":"/dev/fromfile"}`), 0o600)
	t.Setenv("WRAITH_LISTEN", "127.0.0.1:2222")

	cfg, err := Load([]string{"-config", f, "-serial", "/dev/fromflag"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:2222" {
		t.Errorf("env should override file: %s", cfg.ListenAddr)
	}
	if cfg.SerialPort != "/dev/fromflag" {
		t.Errorf("flag should override file: %s", cfg.SerialPort)
	}
}

func TestRejectsUnknownFileKeysAndBadValues(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "c.json")
	os.WriteFile(f, []byte(`{"listen":"x"}`), 0o600)
	if _, err := Load([]string{"-config", f}); err == nil {
		t.Error("unknown key accepted")
	}
	if _, err := Load([]string{"-log-level", "loud"}); err == nil {
		t.Error("bad log level accepted")
	}
	if _, err := Load([]string{"-listen", "nonsense"}); err == nil {
		t.Error("bad listen addr accepted")
	}
}
