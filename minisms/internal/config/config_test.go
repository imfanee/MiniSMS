// Architected and Developed by :- Faisal Hanif | imfanee@gmail.com.
package config

import (
	"testing"
)

func TestLoad_Required(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SECRET_KEY", "")
	t.Setenv("CSRF_AUTH_KEY", "")
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD_HASH", "")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoad_OK(t *testing.T) {
	t.Chdir(t.TempDir())
	hex32 := "0000000000000000000000000000000000000000000000000000000000000000"
	t.Setenv("DATABASE_URL", "postgres://u:p@127.0.0.1:5/db?sslmode=disable")
	t.Setenv("SECRET_KEY", hex32)
	t.Setenv("CSRF_AUTH_KEY", hex32)
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD_HASH", "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi") // bcrypt of "password"
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.AppEnv != "development" {
		t.Fatal("default APP_ENV")
	}
	if c.Port != "8080" {
		t.Fatal("default PORT")
	}
	if c.CarrierDispatchTimeoutSecs != 10 {
		t.Fatal("default CARRIER dispatch timeout")
	}
	if c.SMPPEnquireLinkSecs != 30 || c.SMPPWindowSize != 10 || c.SMPPThroughputPerS != 50 {
		t.Fatalf("SMPP defaults: enquire=%d window=%d throughput=%d",
			c.SMPPEnquireLinkSecs, c.SMPPWindowSize, c.SMPPThroughputPerS)
	}
	if c.SMPPServerEnabled || c.SMPPListenAddr != ":2775" {
		t.Fatal("SMPP server defaults")
	}
}
