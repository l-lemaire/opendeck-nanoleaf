package nanoleaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Client talks to one device. Build it with NewClient.
type Client struct {
	addr  string // host:port
	token string // empty until paired
	log   *log.Logger
	http  *http.Client
}

// ClientOptions is everything NewClient needs. Only Addr is mandatory.
type ClientOptions struct {
	// Addr is "host:port", e.g. "192.168.1.42:16021".
	Addr string
	// Token authenticates requests. Empty for pairing.
	Token string
	// Log receives debug output (full HTTP dumps, token redacted). nil = silent.
	Log *log.Logger
	// Timeout caps one whole request. Default 10 s.
	Timeout time.Duration
}

// NewClient wires the debug logger into a plain HTTP client. Nanoleaf's local
// API has no TLS, so there is nothing to verify; the token is the only
// protection, which is why it must never leak into logs.
func NewClient(o ClientOptions) *Client {
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{
		addr:  o.Addr,
		token: o.Token,
		log:   o.Log,
		http: &http.Client{
			Timeout:   timeout,
			Transport: loggingTransport{next: http.DefaultTransport, log: o.Log},
		},
	}
}

// Addr returns the host:port this client talks to.
func (c *Client) Addr() string { return c.addr }

// Errors callers can test for with errors.Is.
var (
	// ErrNotInPairingMode is what the device says until its power button
	// has been held: HTTP 403 on POST /api/v1/new.
	ErrNotInPairingMode = errors.New("device is not in pairing mode")
	// ErrUnauthorized means the token was rejected (HTTP 401).
	ErrUnauthorized = errors.New("auth token rejected by the device")
)

// tokenPath builds "/api/v1/<token>/<rest>". rest may be "" for the root.
func (c *Client) tokenPath(rest string) string {
	return "/api/v1/" + c.token + "/" + strings.TrimPrefix(rest, "/")
}

// do performs one request and returns the status code and body. Every
// request goes through here, so headers and the base URL live in one place.
func (c *Client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+c.addr+path, reader)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response body: %w", err)
	}
	return resp.StatusCode, data, nil
}

// statusError turns a non-2xx status into an error, mapping the two codes
// that have a specific meaning. path is already redacted for the message.
func statusError(method, path string, status int, body []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s %s: %w", method, RedactPath(path), ErrUnauthorized)
	case http.StatusForbidden:
		return fmt.Errorf("%s %s: %w", method, RedactPath(path), ErrNotInPairingMode)
	default:
		return fmt.Errorf("%s %s: HTTP %d: %s", method, RedactPath(path), status, trim(body))
	}
}

// RequestToken asks the device for a new auth token. It succeeds only
// within about 30 seconds after the power button was held, otherwise the
// device answers 403 and this returns ErrNotInPairingMode.
func (c *Client) RequestToken(ctx context.Context) (string, error) {
	status, body, err := c.do(ctx, http.MethodPost, "/api/v1/new", nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", statusError("POST", "/api/v1/new", status, body)
	}
	var reply struct {
		Token string `json:"auth_token"`
	}
	if err := json.Unmarshal(body, &reply); err != nil || reply.Token == "" {
		return "", fmt.Errorf("POST /api/v1/new: unexpected response: %s", trim(body))
	}
	return reply.Token, nil
}

// DeleteToken revokes this client's token on the device.
func (c *Client) DeleteToken(ctx context.Context) error {
	path := "/api/v1/" + c.token
	status, body, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return statusError("DELETE", path, status, body)
	}
	return nil
}

// Info is what GET /api/v1/<token>/ returns, reduced to what we use.
type Info struct {
	Name         string `json:"name"`
	SerialNo     string `json:"serialNo"`
	Manufacturer string `json:"manufacturer"`
	Firmware     string `json:"firmwareVersion"`
	Model        string `json:"model"`
	State        State  `json:"state"`
	Effects      struct {
		Selected string   `json:"select"`
		List     []string `json:"effectsList"`
	} `json:"effects"`
}

// State is the device's light state.
type State struct {
	On struct {
		Value bool `json:"value"`
	} `json:"on"`
	Brightness struct {
		Value int `json:"value"`
		Max   int `json:"max"`
		Min   int `json:"min"`
	} `json:"brightness"`
}

// Info fetches everything about the device. It doubles as the token check:
// a wrong token yields ErrUnauthorized.
func (c *Client) Info(ctx context.Context) (Info, error) {
	path := c.tokenPath("")
	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return Info{}, err
	}
	if status != http.StatusOK {
		return Info{}, statusError("GET", path, status, body)
	}
	var info Info
	if err := json.Unmarshal(body, &info); err != nil {
		return Info{}, fmt.Errorf("GET %s: bad JSON: %w", RedactPath(path), err)
	}
	return info, nil
}

// trim shortens a body for inclusion in an error message.
func trim(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
