package openaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

// Handlers holds one optional function per event the plugin cares about.
// A nil function means the event is ignored. This is a plain struct rather
// than an interface so a plugin only writes the handlers it needs.
//
// Handlers run one at a time, in the order events arrive, on the goroutine
// that called Run. A handler that needs to do slow work (a network call to
// the device) should either be quick about it or start its own goroutine,
// otherwise key presses queue up behind it.
type Handlers struct {
	WillAppear                 func(ctx context.Context, ev Event, p AppearPayload) error
	WillDisappear              func(ctx context.Context, ev Event, p AppearPayload) error
	KeyDown                    func(ctx context.Context, ev Event, p KeyPayload) error
	KeyUp                      func(ctx context.Context, ev Event, p KeyPayload) error
	DidReceiveSettings         func(ctx context.Context, ev Event, p SettingsPayload) error
	DidReceiveGlobalSettings   func(ctx context.Context, p GlobalSettingsPayload) error
	SendToPlugin               func(ctx context.Context, ev Event, payload json.RawMessage) error
	PropertyInspectorDidAppear func(ctx context.Context, ev Event) error
	SystemDidWakeUp            func(ctx context.Context) error
	// Unknown receives every event with no dedicated handler above.
	Unknown func(ctx context.Context, ev Event) error
}

// Run receives events until the connection closes or ctx ends, dispatching
// each to the matching handler. Handler errors are logged and do not stop
// the loop: one failing key press must not take the plugin down.
//
// Run returns nil when the host closed the connection normally (OpenDeck
// shutting down) or ctx was cancelled, and an error otherwise.
func (c *Conn) Run(ctx context.Context, h Handlers) error {
	for {
		ev, err := c.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				debugf(c.log, "openaction: host closed the connection")
				return nil
			}
			return fmt.Errorf("receive: %w", err)
		}
		if err := c.dispatch(ctx, h, ev); err != nil {
			debugf(c.log, "openaction: %s handler for %s failed: %v", ev.Event, ev.Context, err)
			if c.log == nil {
				// Even without a debug logger the host log should know.
				c.LogMessage(ctx, fmt.Sprintf("%s handler failed: %v", ev.Event, err))
			}
		}
	}
}

// dispatch decodes the payload for the event name and calls the handler.
func (c *Conn) dispatch(ctx context.Context, h Handlers, ev Event) error {
	switch ev.Event {
	case EventWillAppear:
		return callWith(ctx, ev, h.WillAppear)
	case EventWillDisappear:
		return callWith(ctx, ev, h.WillDisappear)
	case EventKeyDown:
		return callWith(ctx, ev, h.KeyDown)
	case EventKeyUp:
		return callWith(ctx, ev, h.KeyUp)
	case EventDidReceiveSettings:
		return callWith(ctx, ev, h.DidReceiveSettings)
	case EventDidReceiveGlobalSettings:
		if h.DidReceiveGlobalSettings == nil {
			return nil
		}
		var p GlobalSettingsPayload
		if err := decodePayload(ev, &p); err != nil {
			return err
		}
		return h.DidReceiveGlobalSettings(ctx, p)
	case EventSendToPlugin:
		if h.SendToPlugin == nil {
			return nil
		}
		return h.SendToPlugin(ctx, ev, ev.Payload)
	case EventPropertyInspectorDidAppear:
		if h.PropertyInspectorDidAppear == nil {
			return nil
		}
		return h.PropertyInspectorDidAppear(ctx, ev)
	case EventSystemDidWakeUp:
		if h.SystemDidWakeUp == nil {
			return nil
		}
		return h.SystemDidWakeUp(ctx)
	default:
		if h.Unknown == nil {
			return nil
		}
		return h.Unknown(ctx, ev)
	}
}

// callWith decodes ev.Payload into a P and calls fn, if fn is set. It is
// generic over the payload type so the four button events above share one
// implementation.
func callWith[P any](ctx context.Context, ev Event, fn func(context.Context, Event, P) error) error {
	if fn == nil {
		return nil
	}
	var p P
	if err := decodePayload(ev, &p); err != nil {
		return err
	}
	return fn(ctx, ev, p)
}

func decodePayload(ev Event, into any) error {
	if len(ev.Payload) == 0 {
		return errors.New("event has no payload")
	}
	if err := json.Unmarshal(ev.Payload, into); err != nil {
		return fmt.Errorf("decode %s payload: %w", ev.Event, err)
	}
	return nil
}
