package nanoleaf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// The device pushes changes on GET /api/v1/<token>/events?id=1,3 as
// Server-Sent Events (see sse.go). The "id" field of each SSE block is the
// event category and the data a JSON object listing attribute changes:
//
//	id: 1
//	data: {"events":[{"attr":1,"value":true}]}      state: attr 1 = on, 2 = brightness
//	id: 3
//	data: {"events":[{"attr":1,"value":"Forest"}]}  effects: attr 1 = selected effect
//
// Category 2 (layout) and 4 (touch) are not requested.

// Change is one attribute update from the stream.
type Change struct {
	// On is set when the on/off state changed.
	On *bool
	// Brightness is set when the brightness changed (0..100).
	Brightness *int
	// Effect is set when the selected effect changed.
	Effect string
}

// EventHandler receives what happens on the stream. All fields optional.
type EventHandler struct {
	OnConnect func()
	OnChange  func(Change)
	OnError   func(err error, retryIn time.Duration)
}

// StreamEvents opens the stream and blocks, calling h for each change,
// until ctx ends or the connection breaks. It returns nil on cancellation
// and the failure otherwise.
func (c *Client) StreamEvents(ctx context.Context, h EventHandler) error {
	path := c.tokenPath("events") + "?id=1,3"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+c.addr+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// The normal client has a whole-request timeout that would cut a
	// never-ending response; reuse its transport (debug log) without one.
	streaming := &http.Client{Transport: c.http.Transport}
	resp, err := streaming.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError("GET", path, resp.StatusCode, nil)
	}
	if h.OnConnect != nil {
		h.OnConnect()
	}

	err = readSSE(resp.Body, func(ev sseEvent) {
		debugf(c.log, "events: id=%s %s", ev.ID, ev.Data)
		for _, change := range parseChanges(ev.ID, []byte(ev.Data), c) {
			if h.OnChange != nil {
				h.OnChange(change)
			}
		}
	})
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		err = errors.New("event stream ended")
	}
	return err
}

// parseChanges decodes one SSE block. category is the SSE id ("1" state,
// "3" effects). Malformed input is logged and yields nothing.
func parseChanges(category string, data []byte, c *Client) []Change {
	var block struct {
		Events []struct {
			Attr  int             `json:"attr"`
			Value json.RawMessage `json:"value"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &block); err != nil {
		debugf(c.log, "events: ignoring undecodable payload: %v", err)
		return nil
	}
	var changes []Change
	for _, ev := range block.Events {
		var ch Change
		switch {
		case category == "1" && ev.Attr == 1: // on/off
			var on bool
			if json.Unmarshal(ev.Value, &on) != nil {
				continue
			}
			ch.On = &on
		case category == "1" && ev.Attr == 2: // brightness
			var b int
			if json.Unmarshal(ev.Value, &b) != nil {
				continue
			}
			ch.Brightness = &b
		case category == "3" && ev.Attr == 1: // selected effect
			var name string
			if json.Unmarshal(ev.Value, &name) != nil {
				continue
			}
			ch.Effect = name
		default:
			continue // hue, saturation, colour temperature, colour mode: not modelled
		}
		changes = append(changes, ch)
	}
	return changes
}

// Watch keeps a stream open until ctx ends, reconnecting after failures
// with a delay that doubles from one second up to thirty.
func (c *Client) Watch(ctx context.Context, h EventHandler) {
	delay := time.Second
	for {
		err := c.StreamEvents(ctx, h)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			err = errors.New("event stream closed")
		}
		if h.OnError != nil {
			h.OnError(err, delay)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}

// describeChange renders a change for logs and the CLI.
func (ch Change) String() string {
	switch {
	case ch.On != nil && *ch.On:
		return "on"
	case ch.On != nil:
		return "off"
	case ch.Brightness != nil:
		return fmt.Sprintf("brightness %d%%", *ch.Brightness)
	case ch.Effect != "":
		return "effect " + ch.Effect
	}
	return "no change"
}
