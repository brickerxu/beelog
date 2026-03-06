//go:build unix

package shell

import (
	"os/signal"
	"syscall"
)

// setupSignals 配置 Unix 平台的信号处理
// 忽略 SIGTSTP (Ctrl+Z)，让 readline 将其作为 undo 处理
func setupSignals() {
	signal.Ignore(syscall.SIGTSTP)
}

// resetSignals 重置 Unix 平台的信号处理
func resetSignals() {
	signal.Reset(syscall.SIGTSTP)
}
