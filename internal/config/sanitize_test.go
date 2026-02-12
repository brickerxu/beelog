package config

import (
	"fmt"
	"strings"
	"testing"
)

func TestSanitizeConfig_MasksSensitiveFields(t *testing.T) {
	cfg := &Config{
		JumpServer: JumpServerConfig{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "admin",
			PrivateKey: "~/.ssh/id_rsa",
			Passphrase: "my-secret-passphrase",
			TOTPSeed:   "JBSWY3DPEHPK3PXP",
		},
		Groups: map[string][]string{
			"web": {"web-1"},
		},
		Defaults: DefaultsConfig{
			OutputMode:  "stream",
			Concurrency: 5,
		},
	}

	sanitized := SanitizeConfig(cfg)

	if sanitized.JumpServer.TOTPSeed != "***" {
		t.Errorf("expected TOTPSeed masked, got %q", sanitized.JumpServer.TOTPSeed)
	}
	if sanitized.JumpServer.Passphrase != "***" {
		t.Errorf("expected Passphrase masked, got %q", sanitized.JumpServer.Passphrase)
	}

	// Non-sensitive fields should be preserved
	if sanitized.JumpServer.Host != "bastion.example.com" {
		t.Errorf("expected Host preserved, got %q", sanitized.JumpServer.Host)
	}
	if sanitized.JumpServer.User != "admin" {
		t.Errorf("expected User preserved, got %q", sanitized.JumpServer.User)
	}
	if len(sanitized.Groups["web"]) != 1 || sanitized.Groups["web"][0] != "web-1" {
		t.Errorf("expected Groups preserved, got %v", sanitized.Groups)
	}
}

func TestSanitizeConfig_EmptySensitiveFieldsStayEmpty(t *testing.T) {
	cfg := &Config{
		JumpServer: JumpServerConfig{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "admin",
			PrivateKey: "~/.ssh/id_rsa",
			Passphrase: "",
			TOTPSeed:   "",
		},
	}

	sanitized := SanitizeConfig(cfg)

	if sanitized.JumpServer.TOTPSeed != "" {
		t.Errorf("expected empty TOTPSeed to stay empty, got %q", sanitized.JumpServer.TOTPSeed)
	}
	if sanitized.JumpServer.Passphrase != "" {
		t.Errorf("expected empty Passphrase to stay empty, got %q", sanitized.JumpServer.Passphrase)
	}
}

func TestSanitizeConfig_DoesNotMutateOriginal(t *testing.T) {
	cfg := &Config{
		JumpServer: JumpServerConfig{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "admin",
			PrivateKey: "~/.ssh/id_rsa",
			Passphrase: "secret",
			TOTPSeed:   "SEED123",
		},
		Groups: map[string][]string{
			"g": {"n1"},
		},
	}

	_ = SanitizeConfig(cfg)

	if cfg.JumpServer.TOTPSeed != "SEED123" {
		t.Errorf("original TOTPSeed was mutated to %q", cfg.JumpServer.TOTPSeed)
	}
	if cfg.JumpServer.Passphrase != "secret" {
		t.Errorf("original Passphrase was mutated to %q", cfg.JumpServer.Passphrase)
	}
}

func TestSanitizeSensitive_ReplacesValues(t *testing.T) {
	text := "connecting with seed JBSWY3DPEHPK3PXP and passphrase my-secret"
	result := SanitizeSensitive(text, []string{"JBSWY3DPEHPK3PXP", "my-secret"})

	if strings.Contains(result, "JBSWY3DPEHPK3PXP") {
		t.Error("expected TOTP seed to be masked")
	}
	if strings.Contains(result, "my-secret") {
		t.Error("expected passphrase to be masked")
	}
	if !strings.Contains(result, "***") {
		t.Error("expected masked value in output")
	}
}

func TestSanitizeSensitive_SkipsEmptyValues(t *testing.T) {
	text := "some log output"
	result := SanitizeSensitive(text, []string{"", ""})

	if result != text {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestSanitizeSensitive_NoSensitiveValues(t *testing.T) {
	text := "normal log line"
	result := SanitizeSensitive(text, nil)

	if result != text {
		t.Errorf("expected unchanged text, got %q", result)
	}
}

func TestSanitizeSensitive_MultipleOccurrences(t *testing.T) {
	text := "seed=ABC123 and again seed=ABC123"
	result := SanitizeSensitive(text, []string{"ABC123"})

	if strings.Contains(result, "ABC123") {
		t.Error("expected all occurrences to be masked")
	}
	expected := "seed=*** and again seed=***"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestCollectSensitiveValues(t *testing.T) {
	cfg := &Config{
		JumpServer: JumpServerConfig{
			TOTPSeed:   "SEED123",
			Passphrase: "pass456",
		},
	}

	values := CollectSensitiveValues(cfg)
	if len(values) != 2 {
		t.Fatalf("expected 2 sensitive values, got %d", len(values))
	}
	if values[0] != "SEED123" || values[1] != "pass456" {
		t.Errorf("unexpected values: %v", values)
	}
}

func TestCollectSensitiveValues_EmptyFields(t *testing.T) {
	cfg := &Config{
		JumpServer: JumpServerConfig{},
	}

	values := CollectSensitiveValues(cfg)
	if len(values) != 0 {
		t.Errorf("expected 0 sensitive values for empty fields, got %d", len(values))
	}
}

func TestJumpServerConfig_String_MasksSensitive(t *testing.T) {
	cfg := JumpServerConfig{
		Host:       "bastion.example.com",
		Port:       22,
		User:       "admin",
		PrivateKey: "~/.ssh/id_rsa",
		Passphrase: "my-secret-passphrase",
		TOTPSeed:   "JBSWY3DPEHPK3PXP",
	}

	str := cfg.String()

	if strings.Contains(str, "JBSWY3DPEHPK3PXP") {
		t.Error("String() should not contain raw TOTP seed")
	}
	if strings.Contains(str, "my-secret-passphrase") {
		t.Error("String() should not contain raw passphrase")
	}
	if !strings.Contains(str, "bastion.example.com") {
		t.Error("String() should contain host")
	}
	if !strings.Contains(str, "admin") {
		t.Error("String() should contain user")
	}
}

func TestJumpServerConfig_String_EmptySensitiveFields(t *testing.T) {
	cfg := JumpServerConfig{
		Host:       "bastion.example.com",
		Port:       22,
		User:       "admin",
		PrivateKey: "~/.ssh/id_rsa",
	}

	str := cfg.String()

	// Should show empty strings for empty fields, not "***"
	if strings.Contains(str, "Passphrase:***") {
		t.Error("empty Passphrase should not show ***")
	}
	if strings.Contains(str, "TOTPSeed:***") {
		t.Error("empty TOTPSeed should not show ***")
	}
}

func TestJumpServerConfig_FmtPrintf_DoesNotLeak(t *testing.T) {
	cfg := JumpServerConfig{
		Host:       "bastion.example.com",
		Port:       22,
		User:       "admin",
		PrivateKey: "~/.ssh/id_rsa",
		Passphrase: "super-secret",
		TOTPSeed:   "JBSWY3DPEHPK3PXP",
	}

	// fmt.Sprintf with %v and %s should use the String() method
	output := fmt.Sprintf("%v", cfg)
	if strings.Contains(output, "JBSWY3DPEHPK3PXP") {
		t.Errorf("fmt.Sprintf with verb v should not leak TOTP seed, got: %s", output)
	}
	if strings.Contains(output, "super-secret") {
		t.Errorf("fmt.Sprintf with verb v should not leak passphrase, got: %s", output)
	}

	output2 := fmt.Sprintf("%s", cfg)
	if strings.Contains(output2, "JBSWY3DPEHPK3PXP") {
		t.Errorf("fmt.Sprintf with verb s should not leak TOTP seed, got: %s", output2)
	}
	if strings.Contains(output2, "super-secret") {
		t.Errorf("fmt.Sprintf with verb s should not leak passphrase, got: %s", output2)
	}
}
