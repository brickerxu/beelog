package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brickerxu/beelog/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newConfigCmd 构造 `beelog config` 命令树，包含 init/show/edit 三个子命令。
func newConfigCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"c"},
		Short:   "管理 beelog 配置文件（可用别名 c）",
		Long:    "查看、编辑或初始化 beelog 配置文件。所有子命令都遵循 --config 指定的路径（默认 ~/.config/beelog/config.yaml）。\n可用 `beelog c ...` 作为别名。",
	}
	configCmd.AddCommand(newConfigInitCmd())
	configCmd.AddCommand(newConfigShowCmd())
	configCmd.AddCommand(newConfigEditCmd())
	return configCmd
}

// newConfigInitCmd 构造 `beelog config init` 子命令：交互式向导仅覆盖 jumpserver 段。
func newConfigInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "init",
		Short:        "交互式向导配置 JumpServer 账号信息",
		Long:         "问答式设置 host/port/user/private_key/passphrase/totp_seed。已存在的 groups/workdirs/defaults 会被完整保留。",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Root().PersistentFlags().GetString("config")
			return runConfigInit(path)
		},
	}
}

// newConfigShowCmd 构造 `beelog config show` 子命令：打印脱敏后的 yaml。
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "打印当前配置（TOTP 种子与 passphrase 脱敏为 ***）",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Root().PersistentFlags().GetString("config")
			return runConfigShow(path)
		},
	}
}

// newConfigEditCmd 构造 `beelog config edit` 子命令：用 $EDITOR 打开配置文件。
func newConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit",
		Short:        "用 $EDITOR 打开配置文件（默认 vim / vi / nano）",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := cmd.Root().PersistentFlags().GetString("config")
			return runConfigEdit(path)
		},
	}
}

// runConfigInit 执行 config init 逻辑。
// 存在旧文件：作为默认值 prefill，回车保留原值；只覆盖 jumpserver 段。
// 不存在：确保父目录存在，从空 Config 起走完向导。
func runConfigInit(path string) error {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return fmt.Errorf("展开配置路径失败: %w", err)
	}

	cfg := &config.Config{Groups: map[string][]string{}}
	hasExisting := false
	if _, err := os.Stat(expanded); err == nil {
		hasExisting = true
		loaded, lerr := config.NewConfigManager().Load(expanded)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "警告: 加载现有配置失败 (%v)，将从空配置开始\n", lerr)
		} else {
			cfg = loaded
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("检查配置文件失败: %w", err)
	}

	if hasExisting {
		fmt.Printf("检测到已有配置: %s（回车保留原值，输入新值将覆盖）\n\n", expanded)
	} else {
		if err := os.MkdirAll(filepath.Dir(expanded), 0700); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}
		fmt.Printf("创建新配置: %s\n\n", expanded)
	}

	reader := bufio.NewReader(os.Stdin)

	// host
	cfg.JumpServer.Host = promptString(reader, "JumpServer host", cfg.JumpServer.Host, true)

	// port（默认 22）
	defaultPort := cfg.JumpServer.Port
	if defaultPort == 0 {
		defaultPort = 22
	}
	cfg.JumpServer.Port = promptInt(reader, "JumpServer port", defaultPort)

	// user
	cfg.JumpServer.User = promptString(reader, "JumpServer user", cfg.JumpServer.User, true)

	// private key path
	defaultKey := cfg.JumpServer.PrivateKey
	if defaultKey == "" {
		defaultKey = "~/.ssh/id_rsa"
	}
	cfg.JumpServer.PrivateKey = promptString(reader, "SSH private key path", defaultKey, true)

	// passphrase（可空，隐藏输入）
	newPassphrase, err := promptSecret("SSH key passphrase (回车跳过或保留原值)", cfg.JumpServer.Passphrase != "")
	if err != nil {
		return err
	}
	if newPassphrase != "" {
		cfg.JumpServer.Passphrase = newPassphrase
	}

	// totp seed（必填，隐藏输入）
	newTOTP, err := promptSecret("TOTP seed (回车保留原值)", cfg.JumpServer.TOTPSeed != "")
	if err != nil {
		return err
	}
	if newTOTP != "" {
		cfg.JumpServer.TOTPSeed = newTOTP
	}
	if cfg.JumpServer.TOTPSeed == "" {
		return fmt.Errorf("TOTP seed 不能为空")
	}

	data, err := config.SerializeToYAML(cfg)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	if err := os.WriteFile(expanded, data, 0600); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	fmt.Printf("\n✓ 配置已写入: %s\n", expanded)
	if hasExisting {
		fmt.Println("提示: 若你在原 yaml 中有注释，此次覆盖会丢失；下次改字段推荐用 `beelog config edit`")
	}
	return nil
}

// runConfigShow 打印脱敏后的 yaml。
func runConfigShow(path string) error {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return fmt.Errorf("展开配置路径失败: %w", err)
	}
	fmt.Printf("# %s\n", expanded)

	cfg, err := config.NewConfigManager().Load(expanded)
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	sanitized := config.SanitizeConfig(cfg)
	data, err := config.SerializeToYAML(&sanitized)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	fmt.Print(string(data))

	// Validate 失败不阻断 show，末尾提示即可
	if verr := config.NewConfigManager().Validate(cfg); verr != nil {
		fmt.Fprintf(os.Stderr, "\n警告: 配置校验失败: %v\n", verr)
	}
	return nil
}

// runConfigEdit 用 $EDITOR / vim / vi / nano 打开配置文件；退出后 Validate。
func runConfigEdit(path string) error {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return fmt.Errorf("展开配置路径失败: %w", err)
	}
	if _, err := os.Stat(expanded); os.IsNotExist(err) {
		return fmt.Errorf("配置文件不存在: %s\n请先运行 `beelog config init`", expanded)
	} else if err != nil {
		return fmt.Errorf("检查配置文件失败: %w", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		for _, cand := range []string{"vim", "vi", "nano"} {
			if _, lerr := exec.LookPath(cand); lerr == nil {
				editor = cand
				break
			}
		}
	}
	if editor == "" {
		return fmt.Errorf("未找到编辑器，请设置 $EDITOR 或安装 vim/vi/nano")
	}

	cmd := exec.Command(editor, expanded)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("编辑器退出异常: %w", err)
	}

	// 退出后加载并 Validate，仅提示不阻断
	cfg, lerr := config.NewConfigManager().Load(expanded)
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "警告: 保存后加载失败: %v\n", lerr)
		return nil
	}
	if verr := config.NewConfigManager().Validate(cfg); verr != nil {
		fmt.Fprintf(os.Stderr, "警告: 配置校验失败: %v\n", verr)
	} else {
		fmt.Println("✓ 配置已更新并校验通过")
	}
	return nil
}

// promptString 显示 [默认值] 提示；回车保留默认；required 且默认为空时循环追问。
func promptString(reader *bufio.Reader, label, def string, required bool) string {
	for {
		if def != "" {
			fmt.Printf("%s [%s]: ", label, def)
		} else {
			fmt.Printf("%s: ", label)
		}
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			if def != "" || !required {
				return def
			}
			fmt.Fprintln(os.Stderr, "  此项必填，请重新输入")
			continue
		}
		return line
	}
}

// promptInt 按整数读取输入；回车保留默认；无效输入循环追问。
func promptInt(reader *bufio.Reader, label string, def int) int {
	for {
		fmt.Printf("%s [%d]: ", label, def)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return def
		}
		n, err := strconv.Atoi(line)
		if err != nil || n <= 0 {
			fmt.Fprintln(os.Stderr, "  请输入正整数")
			continue
		}
		return n
	}
}

// promptSecret 隐藏输入（term.ReadPassword）；hasExisting 决定提示语。
// 返回空串表示用户回车跳过（调用方据此保留原值）。
func promptSecret(label string, hasExisting bool) (string, error) {
	if hasExisting {
		fmt.Printf("%s [***]: ", label)
	} else {
		fmt.Printf("%s: ", label)
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// 非交互环境（管道/重定向）退化为明文读取，方便脚本
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		return strings.TrimSpace(line), nil
	}
	buf, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("读取隐藏输入失败: %w", err)
	}
	return strings.TrimSpace(string(buf)), nil
}
