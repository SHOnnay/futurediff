package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type Client struct {
	SocketPath string
	HTTP       *http.Client
}

func NewClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		d := net.Dialer{Timeout: 5 * time.Second}
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{SocketPath: socket, HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
}
func (c *Client) Do(method, path string, body any) (json.RawMessage, error) {
	return c.do(method, path, body, "")
}

func (c *Client) DoIdempotent(method, path string, body any, idempotencyKey string) (json.RawMessage, error) {
	if idempotencyKey == "" {
		return nil, fmt.Errorf("idempotency key is required")
	}
	return c.do(method, path, body, idempotencyKey)
}

func (c *Client) do(method, path string, body any, idempotencyKey string) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://unix"+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("daemon returned %s: %s", resp.Status, string(b))
	}
	return b, nil
}
