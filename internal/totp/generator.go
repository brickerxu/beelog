package totp

import (
	"encoding/base32"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp/totp"
)

// generator 实现 TOTPGenerator 接口
type generator struct{}

// NewTOTPGenerator 创建一个新的 TOTP 生成器实例
func NewTOTPGenerator() TOTPGenerator {
	return &generator{}
}

// Generate 根据 Base32 密钥种子生成当前 6 位 TOTP 验证码
func (g *generator) Generate(seed string) (string, error) {
	if err := g.ValidateSeed(seed); err != nil {
		return "", err
	}

	code, err := totp.GenerateCode(seed, time.Now())
	if err != nil {
		return "", fmt.Errorf("生成 TOTP 验证码失败: %w", err)
	}

	return code, nil
}

// ValidateSeed 验证种子格式是否为有效的 Base32 编码
func (g *generator) ValidateSeed(seed string) error {
	if seed == "" {
		return fmt.Errorf("TOTP 密钥种子不能为空")
	}

	// Base32 编码使用大写字母 A-Z 和数字 2-7，可能带 = 填充
	// pquerna/otp 内部会处理大小写和填充，但我们先验证基本格式
	upper := strings.ToUpper(strings.TrimRight(seed, "="))
	if upper == "" {
		return fmt.Errorf("TOTP 密钥种子无效: 去除填充后为空")
	}

	// 尝试 Base32 解码来验证
	_, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(upper)
	if err != nil {
		return fmt.Errorf("TOTP 密钥种子不是有效的 Base32 编码: %w", err)
	}

	return nil
}
