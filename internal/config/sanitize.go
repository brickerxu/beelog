package config

import (
	"fmt"
	"strings"
)

const maskedValue = "***"

// SanitizeConfig returns a copy of the config with sensitive fields masked.
// Sensitive fields: JumpServer.TOTPSeed, JumpServer.Passphrase
func SanitizeConfig(cfg *Config) Config {
	sanitized := *cfg

	// Deep copy groups map
	if cfg.Groups != nil {
		sanitized.Groups = make(map[string][]string, len(cfg.Groups))
		for k, v := range cfg.Groups {
			members := make([]string, len(v))
			copy(members, v)
			sanitized.Groups[k] = members
		}
	}

	// Deep copy workdirs map
	if cfg.WorkDirs != nil {
		sanitized.WorkDirs = make(map[string]string, len(cfg.WorkDirs))
		for k, v := range cfg.WorkDirs {
			sanitized.WorkDirs[k] = v
		}
	}

	// Mask sensitive fields
	if sanitized.JumpServer.TOTPSeed != "" {
		sanitized.JumpServer.TOTPSeed = maskedValue
	}
	if sanitized.JumpServer.Passphrase != "" {
		sanitized.JumpServer.Passphrase = maskedValue
	}

	return sanitized
}

// SanitizeSensitive replaces any occurrence of sensitive values in text with "***".
// Empty sensitive values are skipped.
func SanitizeSensitive(text string, sensitiveValues []string) string {
	for _, val := range sensitiveValues {
		if val == "" {
			continue
		}
		text = strings.ReplaceAll(text, val, maskedValue)
	}
	return text
}

// CollectSensitiveValues extracts all non-empty sensitive values from a Config.
func CollectSensitiveValues(cfg *Config) []string {
	var values []string
	if cfg.JumpServer.TOTPSeed != "" {
		values = append(values, cfg.JumpServer.TOTPSeed)
	}
	if cfg.JumpServer.Passphrase != "" {
		values = append(values, cfg.JumpServer.Passphrase)
	}
	return values
}

// String returns a string representation of JumpServerConfig with sensitive fields masked.
// This prevents accidental leaking via fmt.Printf("%v", cfg) or similar.
func (c JumpServerConfig) String() string {
	totpDisplay := maskedValue
	if c.TOTPSeed == "" {
		totpDisplay = ""
	}
	passphraseDisplay := maskedValue
	if c.Passphrase == "" {
		passphraseDisplay = ""
	}
	return fmt.Sprintf(
		"{Host:%s Port:%d User:%s PrivateKey:%s Passphrase:%s TOTPSeed:%s}",
		c.Host, c.Port, c.User, c.PrivateKey, passphraseDisplay, totpDisplay,
	)
}
