package ssh

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// jumpServerInteractor 实现 JumpServerInteractor 接口
// 使用 expect-like 模式与 JumpServer 资产菜单交互
type jumpServerInteractor struct {
	timeout time.Duration
	debug   bool // 调试模式：打印所有读取到的内容
}

// NewJumpServerInteractor 创建 JumpServerInteractor 实例
// timeout 用于每个等待步骤的超时时间
func NewJumpServerInteractor(timeout time.Duration) JumpServerInteractor {
	return &jumpServerInteractor{
		timeout: timeout,
	}
}

// NewJumpServerInteractorWithDebug 创建带调试模式的 JumpServerInteractor 实例
func NewJumpServerInteractorWithDebug(timeout time.Duration, debug bool) JumpServerInteractor {
	return &jumpServerInteractor{
		timeout: timeout,
		debug:   debug,
	}
}

// NavigateToNode 在 JumpServer shell 中导航到目标节点
// 流程:
//  1. 等待 JumpServer 菜单提示符 (Opt>, opt>, ]>, $, #)
//  2. 发送目标节点名称 + 回车（PTY 模式下使用 \r）
//  3. 等待后续输出，处理可能的系统用户选择菜单
//  4. 等待目标节点 shell 就绪 (检测行尾 $, #, > 提示符)
func (j *jumpServerInteractor) NavigateToNode(session *gossh.Session, stdin io.WriteCloser, stdout io.Reader, nodeName string) error {
	// Step 1: 等待 JumpServer 菜单提示符
	if err := j.waitForMenuPrompt(stdout); err != nil {
		return fmt.Errorf("等待 JumpServer 菜单提示符超时: %w", err)
	}

	// Step 2: 发送目标节点名称（PTY 模式下用 \r 作为回车）
	if _, err := fmt.Fprintf(stdin, "%s\r", nodeName); err != nil {
		return fmt.Errorf("发送节点名称失败: %w", err)
	}

	// Step 3: 等待目标节点 shell 就绪
	// JumpServer 可能会弹出系统用户选择菜单（如 "1) root"），
	// 此时需要自动选择第一个（发送 "1\r"）
	if err := j.waitForShellOrUserSelect(stdin, stdout); err != nil {
		return fmt.Errorf("等待目标节点 shell 就绪超时: %w", err)
	}

	return nil
}

// menuPromptPatterns 是 JumpServer 菜单提示符的匹配模式
// 常见模式: "Opt>", "opt>", "]>", "$", "#"
var menuPromptPatterns = []string{
	"Opt>",
	"opt>",
	"]>",
	"$ ",
	"# ",
	"$",
	"#",
}

// shellPromptSuffixes 是目标节点 shell 就绪的提示符后缀
// 检测行尾的 $, #, > 字符（前面可能有空格）
var shellPromptSuffixes = []byte{'$', '#', '>'}

// waitForMenuPrompt 读取 stdout 直到检测到 JumpServer 菜单提示符
func (j *jumpServerInteractor) waitForMenuPrompt(stdout io.Reader) error {
	return j.waitForPattern(stdout, func(buf []byte) bool {
		for _, pattern := range menuPromptPatterns {
			if bytes.Contains(buf, []byte(pattern)) {
				return true
			}
		}
		return false
	})
}

// waitForShellPrompt 读取 stdout 直到检测到目标节点 shell 提示符
// 检测逻辑: 行尾（去除空白后）以 $, #, > 结尾
func (j *jumpServerInteractor) waitForShellPrompt(stdout io.Reader) error {
	return j.waitForPattern(stdout, func(buf []byte) bool {
		// 从缓冲区末尾向前查找最后一个非空白字符
		trimmed := bytes.TrimRight(buf, " \t\r\n")
		if len(trimmed) == 0 {
			return false
		}
		lastChar := trimmed[len(trimmed)-1]
		for _, suffix := range shellPromptSuffixes {
			if lastChar == suffix {
				return true
			}
		}
		return false
	})
}

// waitForShellOrUserSelect 等待目标节点 shell 就绪，同时处理系统用户选择菜单
// JumpServer 在选择资产后可能弹出系统用户选择菜单（如 "1) root\n请输入序号:"），
// 此时自动选择第一个用户（发送 "1\r"），然后继续等待 shell 就绪。
func (j *jumpServerInteractor) waitForShellOrUserSelect(stdin io.WriteCloser, stdout io.Reader) error {
	userSelectSent := false

	return j.waitForPattern(stdout, func(buf []byte) bool {
		// 先检查是否已经到达目标节点 shell
		trimmed := bytes.TrimRight(buf, " \t\r\n")
		if len(trimmed) == 0 {
			return false
		}

		// 去除 ANSI 转义序列后再检查
		cleaned := stripANSI(trimmed)
		cleaned = bytes.TrimRight(cleaned, " \t\r\n")
		if len(cleaned) == 0 {
			return false
		}

		lastChar := cleaned[len(cleaned)-1]

		// 检查是否是 shell 提示符（排除 JumpServer 菜单中的 Opt> ）
		for _, suffix := range shellPromptSuffixes {
			if lastChar == suffix {
				// 排除 JumpServer 自身的 "Opt>" 提示
				if bytes.HasSuffix(cleaned, []byte("Opt>")) {
					continue
				}
				return true
			}
		}

		// 检查是否是系统用户选择菜单
		// JumpServer 常见模式: "1) root" 或 "请选择" 或 "select" 或 "[1]:"
		if !userSelectSent {
			if bytes.Contains(buf, []byte("1)")) || bytes.Contains(buf, []byte("请选择")) ||
				bytes.Contains(buf, []byte("select")) || bytes.Contains(buf, []byte("Select")) ||
				bytes.Contains(buf, []byte("[1]:")) || bytes.Contains(buf, []byte("ID)")) {
				// 自动选择第一个系统用户
				fmt.Fprintf(stdin, "1\r")
				userSelectSent = true
				if j.debug {
					fmt.Fprintf(os.Stderr, "[DEBUG] 检测到系统用户选择菜单，自动发送 '1'\n")
				}
			}
		}

		return false
	})
}

// waitForPattern 通用的 expect-like 等待函数
// 从 stdout 读取数据，直到 matchFn 返回 true 或超时
func (j *jumpServerInteractor) waitForPattern(stdout io.Reader, matchFn func([]byte) bool) error {
	// 使用环形缓冲区保留最近读取的数据用于模式匹配
	// 保留最近 4KB 数据足以覆盖菜单输出和提示符
	const bufWindow = 4096

	var accumulated bytes.Buffer
	readBuf := make([]byte, 256)
	deadline := time.After(j.timeout)

	for {
		select {
		case <-deadline:
			return fmt.Errorf("超时 (%v)，已读取内容: %s", j.timeout, truncateForError(accumulated.Bytes()))
		default:
		}

		// 使用带超时的读取：通过 goroutine + channel 实现非阻塞读取
		type readResult struct {
			n   int
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			n, err := stdout.Read(readBuf)
			ch <- readResult{n: n, err: err}
		}()

		// 等待读取结果或超时
		select {
		case <-deadline:
			return fmt.Errorf("超时 (%v)，已读取内容: %s", j.timeout, truncateForError(accumulated.Bytes()))
		case result := <-ch:
			if result.n > 0 {
				accumulated.Write(readBuf[:result.n])

				// 调试模式：实时打印读取到的内容
				if j.debug {
					fmt.Fprintf(os.Stderr, "[DEBUG] 读取 %d 字节: %q\n", result.n, readBuf[:result.n])
				}

				// 只保留最近 bufWindow 字节用于匹配
				window := accumulated.Bytes()
				if len(window) > bufWindow {
					window = window[len(window)-bufWindow:]
				}

				if matchFn(window) {
					return nil
				}
			}
			if result.err != nil {
				if result.err == io.EOF {
					return fmt.Errorf("连接意外关闭 (EOF)，已读取内容: %s", truncateForError(accumulated.Bytes()))
				}
				return fmt.Errorf("读取输出失败: %w", result.err)
			}
		}
	}
}

// ansiRegex 匹配 ANSI 转义序列
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI 去除字节切片中的 ANSI 转义序列
func stripANSI(data []byte) []byte {
	return ansiRegex.ReplaceAll(data, nil)
}

// truncateForError 截断字节切片用于错误消息展示，最多显示最后 512 字节
func truncateForError(data []byte) string {
	const maxLen = 512
	if len(data) <= maxLen {
		return string(data)
	}
	return "..." + string(data[len(data)-maxLen:])
}
