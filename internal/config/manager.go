package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// DefaultConfigPath 默认配置文件路径
const DefaultConfigPath = "~/.config/beelog/config.yaml"

// 默认值常量
const (
	defaultOutputMode        = "stream"
	defaultConcurrency       = 5
	defaultTimeout           = 60
	defaultMaxRetries        = 3
	defaultRetryDelay        = 5
	defaultKeepaliveInterval = 300
	defaultSaveDir           = "~/beelog_log/"
	defaultSaveFormat        = "text"
)

// configManager 实现 ConfigManager 接口
type configManager struct{}

// NewConfigManager 创建新的 ConfigManager 实例
func NewConfigManager() ConfigManager {
	return &configManager{}
}

// Load 从指定路径加载配置文件，应用默认值
func (m *configManager) Load(path string) (*Config, error) {
	expandedPath, err := ExpandPath(path)
	if err != nil {
		return nil, fmt.Errorf("expand config path: %w", err)
	}

	if _, err := os.Stat(expandedPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", expandedPath)
	}

	v := viper.New()
	v.SetConfigFile(expandedPath)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// 使用 yaml.Unmarshal 直接解析，避免 viper mapstructure 与 yaml tag 不匹配的问题
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	// 应用默认值（处理零值/未设置的字段）
	applyDefaults(&cfg)

	return &cfg, nil
}

// Validate 验证配置对象的有效性
func (m *configManager) Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	// 验证 JumpServer 必填字段
	if cfg.JumpServer.Host == "" {
		return fmt.Errorf("jumpserver.host is required")
	}
	if cfg.JumpServer.Port == 0 {
		return fmt.Errorf("jumpserver.port is required")
	}
	if cfg.JumpServer.User == "" {
		return fmt.Errorf("jumpserver.user is required")
	}
	if cfg.JumpServer.PrivateKey == "" {
		return fmt.Errorf("jumpserver.private_key is required")
	}
	if cfg.JumpServer.TOTPSeed == "" {
		return fmt.Errorf("jumpserver.totp_seed is required")
	}

	// 验证至少有一个分组且分组中有节点
	if len(cfg.Groups) == 0 {
		return fmt.Errorf("at least one group is required")
	}

	// 验证每个分组中的节点名称非空
	for groupName, members := range cfg.Groups {
		if len(members) == 0 {
			return fmt.Errorf("group %q has no nodes", groupName)
		}
		for i, name := range members {
			if name == "" {
				return fmt.Errorf("group %q member[%d] name is empty", groupName, i)
			}
		}
	}

	return nil
}

// GetNodesByGroup 根据分组名称获取节点名称列表
func (m *configManager) GetNodesByGroup(cfg *Config, group string) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	members, ok := cfg.Groups[group]
	if !ok {
		return nil, fmt.Errorf("group %q not found", group)
	}

	return members, nil
}

// SerializeToYAML 将配置对象序列化为 YAML 格式
func SerializeToYAML(cfg *Config) ([]byte, error) {
	return yaml.Marshal(cfg)
}

// ParseFromYAML 从 YAML 字节反序列化为配置对象
func ParseFromYAML(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	return &cfg, nil
}

// applyDefaults 为零值字段填充默认值
func applyDefaults(cfg *Config) {
	if cfg.Defaults.OutputMode == "" {
		cfg.Defaults.OutputMode = defaultOutputMode
	}
	if cfg.Defaults.Concurrency == 0 {
		cfg.Defaults.Concurrency = defaultConcurrency
	}
	if cfg.Defaults.Timeout == 0 {
		cfg.Defaults.Timeout = defaultTimeout
	}
	if cfg.Defaults.MaxRetries == 0 {
		cfg.Defaults.MaxRetries = defaultMaxRetries
	}
	if cfg.Defaults.RetryDelay == 0 {
		cfg.Defaults.RetryDelay = defaultRetryDelay
	}
	if cfg.Defaults.KeepaliveInterval == 0 {
		cfg.Defaults.KeepaliveInterval = defaultKeepaliveInterval
	}
	if cfg.Defaults.SaveDir == "" {
		cfg.Defaults.SaveDir = defaultSaveDir
	}
	if cfg.Defaults.SaveFormat == "" {
		cfg.Defaults.SaveFormat = defaultSaveFormat
	}
}

// ExpandPath 展开路径中的 ~ 为用户主目录
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
