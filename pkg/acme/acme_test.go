package acme_test

import (
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/soft-serve/pkg/acme"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		domains []string
		want    error
	}{
		{"accepts a single hostname", []string{"git.example.com"}, nil},
		{"accepts several hostnames", []string{"git.example.com", "code.example.com"}, nil},
		{"rejects no domains", nil, acme.ErrNoDomains},
		{"rejects an empty domain", []string{""}, acme.ErrInvalidDomain},
		{"rejects a wildcard", []string{"*.example.com"}, acme.ErrInvalidDomain},
		{"rejects a port", []string{"git.example.com:443"}, acme.ErrInvalidDomain},
		{"rejects a URL", []string{"https://git.example.com"}, acme.ErrInvalidDomain},
		{"rejects an IP address", []string{"192.0.2.1"}, acme.ErrInvalidDomain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := acme.Config{Domains: tt.domains}.Validate()
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestHostPolicy(t *testing.T) {
	policy := acme.HostPolicy([]string{"Git.Example.com."})
	tests := []struct {
		host string
		want error
	}{
		{"git.example.com", nil},
		{"GIT.EXAMPLE.COM", nil},
		{"git.example.com.", nil},
		{"other.example.com", acme.ErrHostNotAllowed},
		{"", acme.ErrHostNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if err := policy(context.Background(), tt.host); !errors.Is(err, tt.want) {
				t.Fatalf("policy(%q) = %v, want %v", tt.host, err, tt.want)
			}
		})
	}
}
