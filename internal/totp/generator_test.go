package totp

import (
	"regexp"
	"testing"
)

// 有效的 Base32 TOTP 种子（RFC 4648 标准 Base32 编码）
const validSeed = "JBSWY3DPEHPK3PXP"

func TestGenerate_ValidSeed_Returns6DigitCode(t *testing.T) {
	gen := NewTOTPGenerator()
	code, err := gen.Generate(validSeed)
	if err != nil {
		t.Fatalf("Generate() 返回错误: %v", err)
	}

	matched, _ := regexp.MatchString(`^\d{6}$`, code)
	if !matched {
		t.Errorf("期望 6 位数字验证码，实际得到: %q", code)
	}
}

func TestGenerate_InvalidSeed_ReturnsError(t *testing.T) {
	gen := NewTOTPGenerator()
	_, err := gen.Generate("!!!invalid-base32!!!")
	if err == nil {
		t.Fatal("Generate() 应该对无效种子返回错误")
	}
}

func TestGenerate_EmptySeed_ReturnsError(t *testing.T) {
	gen := NewTOTPGenerator()
	_, err := gen.Generate("")
	if err == nil {
		t.Fatal("Generate() 应该对空种子返回错误")
	}
}

func TestValidateSeed_ValidSeed_NoError(t *testing.T) {
	gen := NewTOTPGenerator()
	err := gen.ValidateSeed(validSeed)
	if err != nil {
		t.Fatalf("ValidateSeed() 对有效种子返回错误: %v", err)
	}
}

func TestValidateSeed_EmptySeed_ReturnsError(t *testing.T) {
	gen := NewTOTPGenerator()
	err := gen.ValidateSeed("")
	if err == nil {
		t.Fatal("ValidateSeed() 应该对空种子返回错误")
	}
}

func TestValidateSeed_InvalidBase32_ReturnsError(t *testing.T) {
	gen := NewTOTPGenerator()
	err := gen.ValidateSeed("!!!not-base32!!!")
	if err == nil {
		t.Fatal("ValidateSeed() 应该对无效 Base32 返回错误")
	}
}
