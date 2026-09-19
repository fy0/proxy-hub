package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const ipLookupURL = "https://ipwho.is/"

func MappingIPLookup(ctx context.Context, mappingID string) (*IPLookupResultDTO, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	mapping, err := MappingGet(ctx, nil, mappingID)
	if err != nil {
		return nil, err
	}
	result := &IPLookupResultDTO{CheckedAt: time.Now()}
	if !mapping.Enabled {
		result.Error = "port mapping is disabled"
		return result, nil
	}
	status := RuntimeStatusGet()
	if failure := runtimeFailureForMapping(status, mapping.ID); failure != nil {
		result.Error = failure.Error
		return result, nil
	}
	if !runtimeHasInboundForMapping(status, mapping.ID) {
		result.Error = "port mapping runtime is not running"
		return result, nil
	}
	proxyURL, err := mappingProbeProxyURL(mapping)
	if err != nil {
		return nil, err
	}
	cfg := normalizeHealthConfig(currentHealthConfig())
	lookup, err := executeIPLookup(ctx, proxyURL, ipLookupURL, cfg.Timeout)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	return lookup, nil
}

func executeIPLookup(ctx context.Context, proxyURL *url.URL, lookupURL string, timeout time.Duration) (*IPLookupResultDTO, error) {
	if proxyURL == nil {
		return nil, fmt.Errorf("IP lookup requires a proxy")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		// A redirect or retry could select a different exit in a rotating pool.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	checkedAt := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("IP lookup status %d", resp.StatusCode)
	}
	const maxBodySize = 64 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodySize {
		return nil, fmt.Errorf("IP lookup response is too large")
	}
	var data struct {
		Success    bool   `json:"success"`
		Message    string `json:"message"`
		IP         string `json:"ip"`
		Country    string `json:"country"`
		Region     string `json:"region"`
		City       string `json:"city"`
		Connection struct {
			ISP string `json:"isp"`
		} `json:"connection"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid IP lookup response: %w", err)
	}
	if !data.Success {
		message := strings.TrimSpace(data.Message)
		if message == "" {
			message = "provider returned an unsuccessful response"
		}
		return nil, fmt.Errorf("IP lookup failed: %s", message)
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(data.IP))
	if err != nil || ip.Zone() != "" {
		return nil, fmt.Errorf("IP lookup returned an invalid IP address")
	}
	return &IPLookupResultDTO{
		IP:        ip.Unmap().String(),
		Country:   strings.TrimSpace(data.Country),
		Region:    strings.TrimSpace(data.Region),
		City:      strings.TrimSpace(data.City),
		ISP:       strings.TrimSpace(data.Connection.ISP),
		CheckedAt: checkedAt,
	}, nil
}
