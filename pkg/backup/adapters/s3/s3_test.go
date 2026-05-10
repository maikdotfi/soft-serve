package s3

import (
	"strings"
	"testing"
)

// TestNewAdapter_NormalizesEndpoint verifies that NewAdapter accepts endpoints
// in the forms users naturally write them (full URLs with scheme, bare hosts,
// host:port pairs). The MinIO SDK requires a bare host[:port], so the adapter
// must strip any scheme/path before handing the endpoint off.
//
// Regression: passing an R2 endpoint URL such as
// "https://<account>.r2.cloudflarestorage.com" used to fail with
// "Endpoint url cannot have fully qualified paths." because the scheme was
// passed through to minio.New verbatim.
func TestNewAdapter_NormalizesEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		endpoint    string
		secureInput bool
		wantErr     bool
	}{
		{
			name:     "https URL is accepted",
			endpoint: "https://abc123.r2.cloudflarestorage.com",
		},
		{
			name:     "https URL with trailing slash is accepted",
			endpoint: "https://abc123.r2.cloudflarestorage.com/",
		},
		{
			name:     "http URL is accepted",
			endpoint: "http://localhost:9000",
		},
		{
			name:     "bare host is accepted",
			endpoint: "s3.amazonaws.com",
		},
		{
			name:     "host:port is accepted",
			endpoint: "127.0.0.1:9000",
		},
		{
			name:        "secure flag set explicitly is accepted",
			endpoint:    "minio.example.com",
			secureInput: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAdapter(S3Config{
				Endpoint:  tt.endpoint,
				Region:    "auto",
				Bucket:    "test-bucket",
				AccessKey: "key",
				SecretKey: "secret",
				Secure:    tt.secureInput,
			})
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NewAdapter(%q): expected error, got nil", tt.endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewAdapter(%q): unexpected error: %v", tt.endpoint, err)
			}
		})
	}
}

// TestNewAdapter_SchemeInfersSecure confirms that an explicit scheme on the
// endpoint takes precedence over the (default) Secure=false zero value, so a
// caller pasting "https://..." gets TLS without having to also flip a flag.
func TestNewAdapter_SchemeInfersSecure(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		wantSecure bool
	}{
		{name: "https scheme implies secure", endpoint: "https://s3.example.com", wantSecure: true},
		{name: "http scheme implies insecure", endpoint: "http://s3.example.com", wantSecure: false},
		{name: "no scheme on remote host implies secure", endpoint: "s3.example.com", wantSecure: true},
		{name: "no scheme on localhost implies insecure", endpoint: "localhost:9000", wantSecure: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewAdapter(S3Config{
				Endpoint:  tt.endpoint,
				Region:    "auto",
				Bucket:    "b",
				AccessKey: "k",
				SecretKey: "s",
			})
			if err != nil {
				t.Fatalf("NewAdapter: %v", err)
			}
			gotURL := a.client.EndpointURL().String()
			gotSecure := strings.HasPrefix(gotURL, "https://")
			if gotSecure != tt.wantSecure {
				t.Fatalf("endpoint URL %q: secure=%v, want %v", gotURL, gotSecure, tt.wantSecure)
			}
		})
	}
}
