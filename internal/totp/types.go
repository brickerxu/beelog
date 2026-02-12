package totp

// TOTPGenerator TOTP 验证码生成接口
type TOTPGenerator interface {
	// Generate 根据 Base32 密钥种子生成当前 6 位 TOTP 验证码
	Generate(seed string) (string, error)
	// ValidateSeed 验证种子格式是否为有效的 Base32 编码
	ValidateSeed(seed string) error
}
