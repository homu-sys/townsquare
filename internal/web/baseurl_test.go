package web

import (
	"crypto/tls"
	"net/http/httptest"
	"testing"

	"townsquare/internal/config"
)

func TestRequestBaseURL(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		tls        bool
		trustProxy bool
		proto      string
		fwdHost    string
		want       string
	}{
		{name: "host", host: "example.test:8090", want: "http://example.test:8090"},
		{name: "tls", host: "example.test", tls: true, want: "https://example.test"},
		{name: "forwarded", host: "internal:8090", trustProxy: true, proto: "https", fwdHost: "public.test", want: "https://public.test"},
		{name: "forwarded lists", host: "internal:8090", trustProxy: true, proto: "https,http", fwdHost: "public.test, proxy", want: "https://public.test"},
		{name: "untrusted proxy", host: "internal:8090", proto: "https", fwdHost: "public.test", want: "http://internal:8090"},
		{name: "invalid host", host: "bad/host", want: "http://localhost"},
		{name: "invalid forwarded host", host: "internal:8090", trustProxy: true, fwdHost: "bad/host", want: "http://internal:8090"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "http://"+tc.host+"/", nil)
			r.Host = tc.host
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			if tc.fwdHost != "" {
				r.Header.Set("X-Forwarded-Host", tc.fwdHost)
			}
			if got := requestBaseURL(r, tc.trustProxy); got != tc.want {
				t.Fatalf("requestBaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBaseURLOverride(t *testing.T) {
	s := &Server{cfg: config.Config{BaseURL: "https://fixed.test"}}
	r := httptest.NewRequest("GET", "http://internal.example/", nil)
	if got := s.baseURL(r); got != "https://fixed.test" {
		t.Fatalf("baseURL() = %q, want fixed override", got)
	}
}
