package config

import (
	"os"
	"path/filepath"
	"testing"
)

// helper: write a temp YAML config file and return its path
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

const validConfigYAML = `
jumpserver:
  host: "bastion.example.com"
  port: 22
  user: "admin"
  private_key: "~/.ssh/id_rsa"
  totp_seed: "JBSWY3DPEHPK3PXP"

groups:
  web: ["web-1", "web-2"]
  all: ["web-1", "web-2"]

defaults:
  output_mode: "grouped"
  concurrency: 10
  timeout: 120
  max_retries: 5
  retry_delay: 10
  keepalive_interval: 600
`

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML)
	mgr := NewConfigManager()

	cfg, err := mgr.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.JumpServer.Host != "bastion.example.com" {
		t.Errorf("expected host bastion.example.com, got %s", cfg.JumpServer.Host)
	}
	if cfg.JumpServer.Port != 22 {
		t.Errorf("expected port 22, got %d", cfg.JumpServer.Port)
	}
	if len(cfg.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(cfg.Groups))
	}
	if cfg.Defaults.OutputMode != "grouped" {
		t.Errorf("expected output_mode grouped, got %s", cfg.Defaults.OutputMode)
	}
	if cfg.Defaults.Concurrency != 10 {
		t.Errorf("expected concurrency 10, got %d", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.KeepaliveInterval != 600 {
		t.Errorf("expected keepalive_interval 600, got %d", cfg.Defaults.KeepaliveInterval)
	}
}

func TestLoad_DefaultValues(t *testing.T) {
	yaml := `
jumpserver:
  host: "bastion.example.com"
  port: 22
  user: "admin"
  private_key: "~/.ssh/id_rsa"
  totp_seed: "JBSWY3DPEHPK3PXP"

groups:
  web: ["web-1"]
`
	path := writeTempConfig(t, yaml)
	mgr := NewConfigManager()

	cfg, err := mgr.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Defaults.OutputMode != "stream" {
		t.Errorf("expected default output_mode stream, got %s", cfg.Defaults.OutputMode)
	}
	if cfg.Defaults.Concurrency != 5 {
		t.Errorf("expected default concurrency 5, got %d", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.Timeout != 60 {
		t.Errorf("expected default timeout 60, got %d", cfg.Defaults.Timeout)
	}
	if cfg.Defaults.MaxRetries != 3 {
		t.Errorf("expected default max_retries 3, got %d", cfg.Defaults.MaxRetries)
	}
	if cfg.Defaults.RetryDelay != 5 {
		t.Errorf("expected default retry_delay 5, got %d", cfg.Defaults.RetryDelay)
	}
	if cfg.Defaults.KeepaliveInterval != 300 {
		t.Errorf("expected default keepalive_interval 300, got %d", cfg.Defaults.KeepaliveInterval)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	mgr := NewConfigManager()
	_, err := mgr.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTempConfig(t, `{invalid yaml: [`)
	mgr := NewConfigManager()
	_, err := mgr.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "admin",
			PrivateKey: "~/.ssh/id_rsa",
			TOTPSeed:   "JBSWY3DPEHPK3PXP",
		},
		Groups: map[string][]string{
			"web": {"web-1"},
		},
	}
	if err := mgr.Validate(cfg); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	mgr := NewConfigManager()
	if err := mgr.Validate(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestValidate_MissingJumpServerHost(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Port: 22, User: "admin", PrivateKey: "key", TOTPSeed: "seed"},
		Groups:     map[string][]string{"g": {"n1"}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestValidate_MissingJumpServerPort(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", User: "admin", PrivateKey: "key", TOTPSeed: "seed"},
		Groups:     map[string][]string{"g": {"n1"}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for missing port")
	}
}

func TestValidate_MissingJumpServerUser(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, PrivateKey: "key", TOTPSeed: "seed"},
		Groups:     map[string][]string{"g": {"n1"}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for missing user")
	}
}

func TestValidate_MissingPrivateKey(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, User: "u", TOTPSeed: "seed"},
		Groups:     map[string][]string{"g": {"n1"}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for missing private_key")
	}
}

func TestValidate_MissingTOTPSeed(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, User: "u", PrivateKey: "key"},
		Groups:     map[string][]string{"g": {"n1"}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for missing totp_seed")
	}
}

func TestValidate_NoGroups(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, User: "u", PrivateKey: "key", TOTPSeed: "seed"},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for no groups")
	}
}

func TestValidate_EmptyGroupMembers(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, User: "u", PrivateKey: "key", TOTPSeed: "seed"},
		Groups:     map[string][]string{"web": {}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for empty group members")
	}
}

func TestValidate_EmptyNodeNameInGroup(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		JumpServer: JumpServerConfig{Host: "h", Port: 22, User: "u", PrivateKey: "key", TOTPSeed: "seed"},
		Groups:     map[string][]string{"web": {"web-1", ""}},
	}
	if err := mgr.Validate(cfg); err == nil {
		t.Fatal("expected error for empty node name in group")
	}
}

func TestGetNodesByGroup_ValidGroup(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		Groups: map[string][]string{
			"web": {"web-1", "web-2"},
			"db":  {"db-1"},
		},
	}

	nodes, err := mgr.GetNodesByGroup(cfg, "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0] != "web-1" || nodes[1] != "web-2" {
		t.Errorf("unexpected node names: %s, %s", nodes[0], nodes[1])
	}
}

func TestGetNodesByGroup_UnknownGroup(t *testing.T) {
	mgr := NewConfigManager()
	cfg := &Config{
		Groups: map[string][]string{"web": {"web-1"}},
	}
	_, err := mgr.GetNodesByGroup(cfg, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown group")
	}
}

func TestGetNodesByGroup_NilConfig(t *testing.T) {
	mgr := NewConfigManager()
	_, err := mgr.GetNodesByGroup(nil, "web")
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestSerializeAndParse_RoundTrip(t *testing.T) {
	original := &Config{
		JumpServer: JumpServerConfig{
			Host:       "bastion.example.com",
			Port:       22,
			User:       "admin",
			PrivateKey: "~/.ssh/id_rsa",
			TOTPSeed:   "JBSWY3DPEHPK3PXP",
		},
		Groups: map[string][]string{
			"web": {"web-1"},
		},
		Defaults: DefaultsConfig{
			OutputMode:        "stream",
			Concurrency:       5,
			Timeout:           60,
			MaxRetries:        3,
			RetryDelay:        5,
			KeepaliveInterval: 300,
		},
	}

	data, err := SerializeToYAML(original)
	if err != nil {
		t.Fatalf("serialize error: %v", err)
	}

	parsed, err := ParseFromYAML(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if parsed.JumpServer.Host != original.JumpServer.Host {
		t.Errorf("host mismatch: %s vs %s", parsed.JumpServer.Host, original.JumpServer.Host)
	}
	if len(parsed.Groups["web"]) != 1 {
		t.Errorf("groups web count mismatch: %d vs 1", len(parsed.Groups["web"]))
	}
	if parsed.Defaults.KeepaliveInterval != original.Defaults.KeepaliveInterval {
		t.Errorf("keepalive_interval mismatch: %d vs %d", parsed.Defaults.KeepaliveInterval, original.Defaults.KeepaliveInterval)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	result, err := ExpandPath("~/test/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, "test/path")
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestExpandPath_AbsolutePath(t *testing.T) {
	result, err := ExpandPath("/absolute/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "/absolute/path" {
		t.Errorf("expected /absolute/path, got %s", result)
	}
}
