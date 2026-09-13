package satoken

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// TokenStyle 取值与 Sa-Token 的 token-style 配置一致。
const (
	StyleUUID       = "uuid"
	StyleSimpleUUID = "simple-uuid"
	StyleRandom32   = "random-32"
	StyleRandom64   = "random-64"
	StyleRandom128  = "random-128"
	StyleTik        = "tik"
)

// 去掉连字符的 uuid 字符串是 32 位十六进制。
const simpleUUIDLen = 32

// NewTokenValue 按 token-style 生成 token 值。
//
// 对应 SaStrategy.generateTokenValue：
//   - uuid      -> UUID.randomUUID().toString()（带连字符）
//   - simple-uuid -> 去掉连字符
//   - random-32/64/128 -> 随机字母数字
//   - tik       -> 2_14_16__
func NewTokenValue(style string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "", StyleUUID:
		id, err := uuid.NewRandom()
		if err != nil {
			return "", fmt.Errorf("生成 uuid 失败: %w", err)
		}
		return id.String(), nil
	case StyleSimpleUUID:
		return strings.ReplaceAll(uuid.NewString(), "-", ""), nil
	case StyleRandom32:
		return randomString(32)
	case StyleRandom64:
		return randomString(64)
	case StyleRandom128:
		return randomString(128)
	case StyleTik:
		a, err := randomString(2)
		if err != nil {
			return "", err
		}
		b, err := randomString(14)
		if err != nil {
			return "", err
		}
		c, err := randomString(16)
		if err != nil {
			return "", err
		}
		return a + "_" + b + "_" + c + "__", nil
	default:
		return "", fmt.Errorf("token-style 取值无效: %s", style)
	}
}

// NewSessionID 生成 Account-Session 的 id，与 Java 侧 UUID.randomUUID().toString() 对齐。
func NewSessionID() string {
	return uuid.NewString()
}

const alphaNum = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomString(length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机 token 失败: %w", err)
	}
	out := make([]byte, length)
	for i, b := range buf {
		out[i] = alphaNum[int(b)%len(alphaNum)]
	}
	return string(out), nil
}

// RandomHex 生成 n 字节的十六进制字符串（内部使用，测试可预期长度）。
func RandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机串失败: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
