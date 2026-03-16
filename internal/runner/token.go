package runner

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type confirmTokenPayload struct {
	ExpMs int64  `json:"exp_ms"`
	Sig   string `json:"sig"`
}

func GenerateConfirmToken(secret []byte, req ConfirmRequest, now time.Time, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("missing secret")
	}
	exp := now.Add(ttl).UnixMilli()
	sig := signConfirm(secret, req, exp)
	p := confirmTokenPayload{ExpMs: exp, Sig: sig}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ValidateConfirmToken(secret []byte, req ConfirmRequest, token string, now time.Time) error {
	if len(secret) == 0 {
		return errors.New(ErrConfirmTokenNoKey)
	}
	t := strings.TrimSpace(token)
	if t == "" {
		return errors.New(ErrConfirmTokenMiss)
	}
	b, err := base64.RawURLEncoding.DecodeString(t)
	if err != nil {
		return errors.New(ErrConfirmToken)
	}
	var p confirmTokenPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return errors.New(ErrConfirmToken)
	}
	if now.UnixMilli() > p.ExpMs {
		return errors.New(ErrConfirmTokenExp)
	}
	want := signConfirm(secret, req, p.ExpMs)
	if !hmac.Equal([]byte(want), []byte(p.Sig)) {
		return errors.New(ErrConfirmToken)
	}
	return nil
}

func signConfirm(secret []byte, req ConfirmRequest, expMs int64) string {
	msg := strings.Join([]string{
		strings.TrimSpace(req.Account),
		strings.TrimSpace(req.Region),
		strings.TrimSpace(req.Profile),
		strings.TrimSpace(req.Action),
		strings.TrimSpace(req.ArgsSig),
		strconvFormatInt(expMs),
	}, "\n")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func strconvFormatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
