package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// wireUsage is one entry of the egress proxy's /usage.
type wireUsage struct {
	Binding     string    `json:"binding"`
	Label       string    `json:"label"`
	Requests    int       `json:"requests"`
	OK          int       `json:"ok"`
	ClientError int       `json:"client_error"`
	RateLimited int       `json:"rate_limited"`
	ServerError int       `json:"server_error"`
	LastRequest time.Time `json:"last_request"`
	Recent      []int     `json:"recent"`
}

func (w wireUsage) usage() *Usage {
	return &Usage{
		Requests: w.Requests, OK: w.OK, ClientError: w.ClientError,
		RateLimited: w.RateLimited, ServerError: w.ServerError,
		Last: w.LastRequest, Recent: w.Recent,
	}
}

// fetchUsage reads the egress proxy's per-account request counts.
func fetchUsage(ctx context.Context, base string) ([]wireUsage, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/usage", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build usage request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read proxy usage: %w", err)
	}
	defer closeBody(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read proxy usage: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read proxy usage: %w", err)
	}
	var out []wireUsage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse proxy usage: %w", err)
	}
	return out, nil
}
