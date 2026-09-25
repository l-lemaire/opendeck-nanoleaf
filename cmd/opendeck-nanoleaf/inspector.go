package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/openaction"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/pairing"
)

// The property inspector talks to the plugin through the host. Message
// shapes:
//
//	inspector -> plugin   {"event":"listTargets"}
//	plugin -> inspector   {"event":"targets","items":[{"id","name","on","reachable"}],"default":"<id>"}
//	plugin -> inspector   {"event":"error","code":"not_paired"|"","message":"..."}
//
//	inspector -> plugin   {"event":"discover"}
//	plugin -> inspector   {"event":"devices","items":[{"id","host","port","name","model","firmware"}],"message":"<why, if none>"}
//	inspector -> plugin   {"event":"pair","address":"<host[:port]>","id":"<device id>"}
//	plugin -> inspector   {"event":"pairing","stage":"searching|found|waiting|paired|error",
//	                       "message":"...","seconds_left":N}

type inspectorRequest struct {
	Event   string `json:"event"`
	Address string `json:"address"`
	ID      string `json:"id"`
}

type targetItem struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	On        bool   `json:"on"`
	Reachable bool   `json:"reachable"`
}

type targetsReply struct {
	Event   string       `json:"event"`
	Items   []targetItem `json:"items"`
	Default string       `json:"default"`
}

type devicesReply struct {
	Event   string            `json:"event"`
	Items   []nanoleaf.Device `json:"items"`
	Message string            `json:"message,omitempty"`
}

type pairingReply struct {
	Event       string `json:"event"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	SecondsLeft int    `json:"seconds_left,omitempty"`
}

type errorReply struct {
	Event   string `json:"event"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

var errNotPaired = errors.New("no device paired yet")

// handleInspectorMessage serves sendToPlugin events.
func (p *plugin) handleInspectorMessage(ctx context.Context, ev openaction.Event, payload json.RawMessage) error {
	var req inspectorRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("inspector sent invalid JSON: %w", err)
	}
	switch req.Event {
	case "listTargets":
		go func() { // contacts every device; keep the event loop free
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			reply, err := p.listTargets(ctx, ev.Action)
			if err != nil {
				code := ""
				if errors.Is(err, errNotPaired) {
					code = "not_paired"
				}
				p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context, errorReply{Event: "error", Code: code, Message: err.Error()})
				return
			}
			p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context, reply)
		}()
		return nil
	case "discover":
		return p.startDiscovery(ctx, ev)
	case "pair":
		return p.startPairing(ctx, ev, req.Address, req.ID)
	default:
		return fmt.Errorf("inspector sent unknown event %q", req.Event)
	}
}

// listTargets lists the paired devices with their live on/off state.
func (p *plugin) listTargets(ctx context.Context, action string) (targetsReply, error) {
	if err := checkAction(action); err != nil {
		return targetsReply{}, err
	}
	known, defaultID, err := p.devices.Devices()
	if err != nil {
		return targetsReply{}, err
	}
	if len(known) == 0 {
		return targetsReply{}, errNotPaired
	}
	reply := targetsReply{Event: "targets", Default: defaultID, Items: []targetItem{}}
	for _, d := range known {
		item := targetItem{ID: d.ID, Name: d.Name}
		if client, _, err := p.devices.Connect(ctx, d.ID); err == nil {
			if on, err := client.IsOn(ctx); err == nil {
				item.On, item.Reachable = on, true
			}
		}
		reply.Items = append(reply.Items, item)
	}
	return reply, nil
}

// startDiscovery looks for devices in a goroutine and sends the list.
func (p *plugin) startDiscovery(ctx context.Context, ev openaction.Event) error {
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		devices, err := p.devices.Discover(ctx)
		reply := devicesReply{Event: "devices", Items: devices}
		if reply.Items == nil {
			reply.Items = []nanoleaf.Device{}
		}
		if err != nil {
			reply.Message = err.Error()
			p.info.Printf("discovery for the panel failed: %v", err)
		} else {
			p.info.Printf("discovery for the panel found %d device(s)", len(devices))
		}
		p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context, reply)
	}()
	return nil
}

// startPairing runs the pairing flow in a goroutine, forwarding progress.
func (p *plugin) startPairing(ctx context.Context, ev openaction.Event, address, id string) error {
	if !p.inflight.begin("pairing") {
		return p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context,
			pairingReply{Event: "pairing", Stage: "error", Message: "A pairing is already in progress"})
	}
	p.info.Printf("pairing requested from the panel (address %q, id %q)", address, id)
	go func() {
		defer p.inflight.end("pairing")
		ctx, cancel := context.WithTimeout(ctx, pairing.DefaultTimeout+30*time.Second)
		defer cancel()
		dev, err := p.devices.Pair(ctx, address, id, func(pr pairing.Progress) {
			p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context,
				pairingReply{Event: "pairing", Stage: pr.Stage, Message: pr.Message, SecondsLeft: pr.SecondsLeft})
		})
		if err != nil {
			p.info.Printf("pairing failed: %v", err)
			p.conn.SendToPropertyInspector(ctx, ev.Action, ev.Context,
				pairingReply{Event: "pairing", Stage: "error", Message: err.Error()})
			return
		}
		p.info.Printf("paired with device %s (%s)", dev.ID, dev.Name)
	}()
	return nil
}
