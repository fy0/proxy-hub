package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteIPLookupUsesOneProxyRequestPerLookup(t *testing.T) {
	var requests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if r.URL.String() != "http://lookup.invalid/" {
			t.Errorf("request URL = %q", r.URL)
		}
		wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:secret"))
		if r.Header.Get("Proxy-Authorization") != wantAuth {
			t.Error("proxy credentials missing")
		}
		if r.Header.Get("Accept") != "application/json" || r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("lookup headers missing")
		}
		fmt.Fprintf(w, `{"success":true,"ip":"203.0.113.%d","country":"Country %d","region":"Region","city":"City","connection":{"isp":"ISP"}}`, n, n)
	}))
	defer proxy.Close()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL.User = url.UserPassword("user", "secret")
	for n := int32(1); n <= 2; n++ {
		result, err := executeIPLookup(context.Background(), proxyURL, "http://lookup.invalid/", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if requests.Load() != n {
			t.Fatalf("requests = %d, want %d", requests.Load(), n)
		}
		if result.IP != fmt.Sprintf("203.0.113.%d", n) || result.Country != fmt.Sprintf("Country %d", n) {
			t.Fatalf("IP and country must come from this request: %+v", result)
		}
		if result.Region != "Region" || result.City != "City" || result.ISP != "ISP" || result.CheckedAt.IsZero() {
			t.Fatalf("incomplete result: %+v", result)
		}
	}
}

func TestExecuteIPLookupResponseHandling(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		wantIP string
	}{
		{name: "IP without location", status: 200, body: `{"success":true,"ip":"203.0.113.1"}`, wantIP: "203.0.113.1"},
		{name: "IPv6", status: 200, body: `{"success":true,"ip":"2001:db8::1"}`, wantIP: "2001:db8::1"},
		{name: "invalid IP", status: 200, body: `{"success":true,"ip":"not an IP"}`},
		{name: "zone", status: 200, body: `{"success":true,"ip":"fe80::1%eth0"}`},
		{name: "provider error", status: 200, body: `{"success":false,"message":"rate limit"}`},
		{name: "missing success", status: 200, body: `{"ip":"203.0.113.1"}`},
		{name: "malformed JSON", status: 200, body: `<html>error</html>`},
		{name: "rate limit", status: 429, body: `{}`},
		{name: "redirect", status: 302, body: `{}`},
		{name: "oversized", status: 200, body: strings.Repeat(" ", 64*1024+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Location", "http://lookup.invalid/redirected")
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer proxy.Close()
			proxyURL, err := url.Parse(proxy.URL)
			if err != nil {
				t.Fatal(err)
			}
			result, err := executeIPLookup(context.Background(), proxyURL, "http://lookup.invalid/", time.Second)
			if tt.wantIP == "" {
				if err == nil {
					t.Fatalf("expected lookup failure, got %+v", result)
				}
			} else if err != nil || result.IP != tt.wantIP {
				t.Fatalf("lookup = %+v, %v; want IP %s", result, err, tt.wantIP)
			}
			if requests.Load() != 1 {
				t.Fatalf("requests = %d, want 1 with no redirect or retry", requests.Load())
			}
		})
	}
}

func TestExecuteIPLookupRequiresProxyAndHonorsCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"success":true,"ip":"203.0.113.1"}`)
	}))
	defer server.Close()
	if _, err := executeIPLookup(context.Background(), nil, server.URL, time.Second); err == nil {
		t.Fatal("lookup without proxy must fail")
	}
	proxyURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executeIPLookup(ctx, proxyURL, "http://lookup.invalid/", time.Second); err == nil {
		t.Fatal("canceled lookup must fail")
	}
	if requests.Load() != 0 {
		t.Fatalf("unexpected requests = %d", requests.Load())
	}
}
