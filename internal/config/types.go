package config

// Config 表示完整的应用配置
type Config struct {
	JumpServer JumpServerConfig    `yaml:"jumpserver"`
	Groups     map[string][]string `yaml:"groups"`
	WorkDirs   map[string]string   `yaml:"workdirs,omitempty"` // 每个分组的默认工作目录
	Defaults   DefaultsConfig      `yaml:"defaults"`
}

// JumpServerConfig JumpServer 跳板机连接配置
type JumpServerConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	User       string `yaml:"user"`
	PrivateKey string `yaml:"private_key"`
	Passphrase string `yaml:"passphrase,omitempty"`
	TOTPSeed   string `yaml:"totp_seed"`
}

// DefaultsConfig 默认配置项
type DefaultsConfig struct {
	OutputMode        string `yaml:"output_mode"`        // grouped | merged | stream
	Concurrency       int    `yaml:"concurrency"`        // 最大并发数 (2-20)
	Timeout           int    `yaml:"timeout"`            // 命令超时秒数
	MaxRetries        int    `yaml:"max_retries"`        // 连接重试次数
	RetryDelay        int    `yaml:"retry_delay"`        // 重试间隔秒数
	KeepaliveInterval int    `yaml:"keepalive_interval"` // 心跳间隔秒数 (默认 300，即 5 分钟)
	SaveDir           string `yaml:"save_dir"`           // 结果保存目录，默认 ~/beelog_log/
	SaveFormat        string `yaml:"save_format"`        // 保存格式: text | structured | json | csv
}

// ConfigManager 配置管理接口
type ConfigManager interface {
	// Load 从指定路径加载配置文件
	Load(path string) (*Config, error)
	// Validate 验证配置对象的有效性
	Validate(cfg *Config) error
	// GetNodesByGroup 根据分组名称获取节点名称列表
	GetNodesByGroup(cfg *Config, group string) ([]string, error)
}
