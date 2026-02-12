package ssh

import (
	"fmt"
	"os"
	"strings"
)

// CheckKeyPermissions checks the SSH private key file permissions.
// Returns a warning message if permissions are not 0600, nil otherwise.
// This is a non-blocking check — callers should print the warning but not abort.
func CheckKeyPermissions(keyPath string) error {
	// Expand ~ in the path
	if strings.HasPrefix(keyPath, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("获取用户主目录失败: %w", err)
		}
		keyPath = home + keyPath[1:]
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		return fmt.Errorf("无法检查私钥文件: %w", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		return fmt.Errorf("[WARN] SSH 私钥文件权限不安全 (当前: %04o, 建议: 0600): %s", perm, keyPath)
	}

	return nil
}
