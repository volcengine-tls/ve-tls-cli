package auth

import (
	"context"
	"strings"
	"time"
)

type Value struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ProviderName    string
	ExpiresAt       time.Time
}

func (v Value) Validate() error {
	if strings.TrimSpace(v.AccessKeyID) == "" || strings.TrimSpace(v.SecretAccessKey) == "" {
		return &Error{
			Kind:        ConfigInvalid,
			Description: "access key id and secret access key must both be non-empty",
		}
	}
	return nil
}

type Provider interface {
	Retrieve(context.Context) (Value, error)
}

type StaticProvider struct {
	Value Value
}

func (p StaticProvider) Retrieve(context.Context) (Value, error) {
	return p.Value, nil
}
