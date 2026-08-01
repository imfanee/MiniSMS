// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package wirelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Acme Gateway (SMPP)": "Acme_Gateway_SMPP",
		"../etc/passwd":       "etc_passwd",
		"  ":                  "unknown",
		"a/b\\c":              "a_b_c",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q)=%q want %q", in, got, want)
		}
	}
}

func TestMaskAndBackstop(t *testing.T) {
	if Mask("") != "(empty)" {
		t.Errorf("empty mask")
	}
	if got := Mask("hunter2"); got != "***(7)" {
		t.Errorf("mask len, got %q", got)
	}
	if maskKV("password", "hunter2") != "***" {
		t.Errorf("sensitive key not masked")
	}
	if maskKV("dst", "+123") != "+123" {
		t.Errorf("non-sensitive key wrongly masked")
	}
}

func TestEmitWritesToNamedFileAndMasks(t *testing.T) {
	dir := t.TempDir()
	if err := Init(dir, true, 100, 5); err != nil {
		t.Fatalf("init: %v", err)
	}
	Emit("Some Carrier", "smpp", ">>", "bind_transceiver", "system_id", "esme1", "password", "topsecret")
	path := filepath.Join(dir, "Some_Carrier_smpp.log")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	line := string(b)
	if !strings.Contains(line, ">> bind_transceiver") || !strings.Contains(line, "system_id=esme1") {
		t.Errorf("event not written: %q", line)
	}
	if strings.Contains(line, "topsecret") || !strings.Contains(line, "password=***") {
		t.Errorf("password not masked: %q", line)
	}
}

func TestRotation(t *testing.T) {
	dir := t.TempDir()
	// tiny cap to force rotation: Init clamps maxMB<=0, so use the struct directly.
	m := &Manager{dir: dir, maxBytes: 200, maxFiles: 3, enabled: true, loggers: map[string]*fileLogger{}}
	for i := 0; i < 50; i++ {
		m.line("ent", "http", ">>", "request", "n", strings.Repeat("x", 20))
	}
	if _, err := os.Stat(filepath.Join(dir, "ent_http.log")); err != nil {
		t.Fatalf("live file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ent_http.log.1")); err != nil {
		t.Fatalf("rotated file .1 missing: %v", err)
	}
	// Oldest generation must be capped at maxFiles.
	if _, err := os.Stat(filepath.Join(dir, "ent_http.log.4")); err == nil {
		t.Fatalf("rotation exceeded maxFiles (.4 exists)")
	}
}
