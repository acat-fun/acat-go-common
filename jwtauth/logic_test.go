// Package jwtauth 的单元测试。
package jwtauth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/acat-fun/acat-go-common/jwtauth"
	"github.com/acat-fun/acat-go-common/satoken"
)

func TestLoginCheckLogout(t *testing.T) {
	logic := jwtauth.NewLogic(jwtauth.Config{
		Secret:     "test-secret-key-32bytes-minimum!",
		Issuer:     "test-issuer",
		TTLSeconds: 3600,
	})
	ctx := context.Background()

	token, err := logic.Login(ctx, "42")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("期望非空 token")
	}

	loginID, err := logic.CheckLogin(ctx, token)
	if err != nil {
		t.Fatalf("CheckLogin: %v", err)
	}
	if loginID != "42" {
		t.Fatalf("loginID=%q", loginID)
	}

	session, err := logic.GetSession(ctx, loginID)
	if err != nil || session == nil {
		t.Fatalf("GetSession: sess=%v err=%v", session, err)
	}
	if session.LoginID != "42" {
		t.Fatalf("session.LoginID=%v", session.LoginID)
	}

	if err := logic.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	// 无状态：logout 后 token 仍有效。
	if _, err := logic.CheckLogin(ctx, token); err != nil {
		t.Fatalf("logout 后 JWT 仍应有效: %v", err)
	}
}

func TestCheckLoginExpired(t *testing.T) {
	secret := "test-secret-key-32bytes-minimum!"
	issuer := "test"
	past := time.Now().Add(-2 * time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "1",
		Issuer:    issuer,
		IssuedAt:  jwt.NewNumericDate(past.Add(-time.Hour)),
		ExpiresAt: jwt.NewNumericDate(past),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	logic := jwtauth.NewLogic(jwtauth.Config{Secret: secret, Issuer: issuer})
	_, err = logic.CheckLogin(context.Background(), signed)
	if !errors.Is(err, satoken.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}

func TestCheckLoginTampered(t *testing.T) {
	logic := jwtauth.NewLogic(jwtauth.Config{Secret: "secret-a", Issuer: "x"})
	ctx := context.Background()
	token, err := logic.Login(ctx, "1")
	if err != nil {
		t.Fatal(err)
	}
	other := jwtauth.NewLogic(jwtauth.Config{Secret: "secret-b", Issuer: "x"})
	_, err = other.CheckLogin(ctx, token)
	if !errors.Is(err, satoken.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，得到 %v", err)
	}
}

func TestTokenNameDefault(t *testing.T) {
	logic := jwtauth.NewLogic(jwtauth.Config{Secret: "s"})
	if logic.TokenName() != jwtauth.DefaultTokenName {
		t.Fatalf("TokenName=%q", logic.TokenName())
	}
}
