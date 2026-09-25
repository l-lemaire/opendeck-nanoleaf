package nanoleaf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// On/off is the whole of what this plugin controls for now. The API:
//
//	GET /api/v1/<token>/state/on   -> {"value": true}
//	PUT /api/v1/<token>/state      <- {"on": {"value": false}}   (204 No Content)

// IsOn reports whether the panels are on.
func (c *Client) IsOn(ctx context.Context) (bool, error) {
	path := c.tokenPath("state/on")
	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, statusError("GET", path, status, body)
	}
	var reply struct {
		Value bool `json:"value"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return false, fmt.Errorf("GET %s: bad JSON: %w", RedactPath(path), err)
	}
	return reply.Value, nil
}

// SetOn switches the panels on or off.
func (c *Client) SetOn(ctx context.Context, on bool) error {
	path := c.tokenPath("state")
	body := map[string]any{"on": map[string]bool{"value": on}}
	status, resp, err := c.do(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return statusError("PUT", path, status, resp)
	}
	return nil
}

// Toggle reads the state and flips it, returning the new state. Two
// requests: the API has no atomic toggle.
func (c *Client) Toggle(ctx context.Context) (nowOn bool, err error) {
	on, err := c.IsOn(ctx)
	if err != nil {
		return false, err
	}
	nowOn = !on
	return nowOn, c.SetOn(ctx, nowOn)
}
