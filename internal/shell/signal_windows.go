//go:build windows

package shell

// setupSignals 配置 Windows 平台的信号处理
// Windows 不支持 SIGTSTP，无需处理
// Readline 库会在 Windows 上自动将 Ctrl+Z 处理为 undo
func setupSignals() {
	// No-op on Windows
}

// resetSignals 重置 Windows 平台的信号处理
// Windows 不支持 SIGTSTP，无需处理
func resetSignals() {
	// No-op on Windows
}
