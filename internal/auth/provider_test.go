package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestStaticProviderReturnsUnchangedValue(t *testing.T) {
	want := Value{
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		SessionToken:    "token",
		ProviderName:    "static-test",
		ExpiresAt:       time.Unix(1_900_000_000, 123),
	}
	provider := StaticProvider{Value: want}

	got, err := provider.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Retrieve value mismatch:\ngot:  %+v\nwant: %+v", got, want)
	}

	var _ Provider = provider
}

func TestValueValidateRequiresAKSKPair(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   Value
		wantErr bool
	}{
		{name: "empty", value: Value{}, wantErr: true},
		{name: "access key only", value: Value{AccessKeyID: "ak"}, wantErr: true},
		{name: "secret key only", value: Value{SecretAccessKey: "sk"}, wantErr: true},
		{name: "whitespace access key", value: Value{AccessKeyID: " ", SecretAccessKey: "sk"}, wantErr: true},
		{name: "pair without token", value: Value{AccessKeyID: "ak", SecretAccessKey: "sk"}},
		{name: "pair with token", value: Value{AccessKeyID: "ak", SecretAccessKey: "sk", SessionToken: "token"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.value.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("Validate unexpectedly succeeded")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if tc.wantErr {
				var authErr *Error
				if !errors.As(err, &authErr) {
					t.Fatalf("Validate error=%T %v, want *Error", err, err)
				}
				if authErr.Kind != ConfigInvalid {
					t.Fatalf("Validate error kind=%q, want %q", authErr.Kind, ConfigInvalid)
				}
			}
		})
	}
}
