package ssh

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckKeyPermissions_Correct600(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa")
	if err := os.WriteFile(keyFile, []byte("fake-key"), 0600); err != nil {
		t.Fatal(err)
	}

	err := CheckKeyPermissions(keyFile)
	if err != nil {
		t.Errorf("expected no error for 0600 permissions, got: %v", err)
	}
}

func TestCheckKeyPermissions_Insecure644(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa")
	if err := os.WriteFile(keyFile, []byte("fake-key"), 0644); err != nil {
		t.Fatal(err)
	}

	err := CheckKeyPermissions(keyFile)
	if err == nil {
		t.Fatal("expected warning for 0644 permissions, got nil")
	}
	if !strings.Contains(err.Error(), "[WARN]") {
		t.Errorf("expected [WARN] in message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "0644") {
		t.Errorf("expected current permission 0644 in message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Errorf("expected recommended 0600 in message, got: %v", err)
	}
}

func TestCheckKeyPermissions_Insecure755(t *testing.T) {
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "id_rsa")
	if err := os.WriteFile(keyFile, []byte("fake-key"), 0755); err != nil {
		t.Fatal(err)
	}

	err := CheckKeyPermissions(keyFile)
	if err == nil {
		t.Fatal("expected warning for 0755 permissions, got nil")
	}
	if !strings.Contains(err.Error(), "0755") {
		t.Errorf("expected current permission 0755 in message, got: %v", err)
	}
}

func TestCheckKeyPermissions_FileNotFound(t *testing.T) {
	err := CheckKeyPermissions("/nonexistent/path/id_rsa")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !strings.Contains(err.Error(), "无法检查私钥文件") {
		t.Errorf("expected stat error message, got: %v", err)
	}
}

func TestCheckKeyPermissions_TildeExpansion(t *testing.T) {
	// This test verifies that ~ expansion works by checking that a path
	// starting with ~/ doesn't cause a panic and resolves correctly.
	// We use a nonexistent file under home to verify expansion happened.
	err := CheckKeyPermissions("~/.beelog_test_nonexistent_key")
	if err == nil {
		t.Fatal("expected error for nonexistent file under ~, got nil")
	}
	// The error should reference the expanded path (not contain ~/)
	if strings.Contains(err.Error(), "~/") {
		t.Errorf("expected ~ to be expanded in error message, got: %v", err)
	}
}
