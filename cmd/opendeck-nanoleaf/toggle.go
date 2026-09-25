package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/openaction"
)

const deviceTimeout = 10 * time.Second

const (
	stateOff = 0
	stateOn  = 1
)

// onKeyDown toggles the button's device in a goroutine and reports the
// result through SetState, or ShowAlert on failure.
func (p *plugin) onKeyDown(ctx context.Context, ev openaction.Event, raw json.RawMessage) error {
	s, err := decodeSettings(raw)
	if err != nil {
		p.conn.ShowAlert(ctx, ev.Context)
		return err
	}
	if s.Device == "" {
		p.info.Printf("key %s pressed but no device chosen yet", ev.Context)
		return p.conn.ShowAlert(ctx, ev.Context)
	}
	if !p.inflight.begin(ev.Context) {
		p.info.Printf("key %s pressed while a toggle is still in progress; ignored", ev.Context)
		return nil
	}
	go func() {
		defer p.inflight.end(ev.Context)
		ctx, cancel := context.WithTimeout(ctx, deviceTimeout)
		defer cancel()
		client, _, err := p.devices.Connect(ctx, s.Device)
		var nowOn bool
		if err == nil {
			nowOn, err = client.Toggle(ctx)
		}
		if err != nil {
			p.info.Printf("toggle %q failed: %v", s.Name, err)
			p.conn.ShowAlert(ctx, ev.Context)
			return
		}
		p.info.Printf("toggled %q -> %s", s.Name, onOff(nowOn))
		if err := p.conn.SetState(ctx, ev.Context, stateIndex(nowOn)); err != nil {
			p.info.Printf("setState for %s failed: %v", ev.Context, err)
		}
	}()
	return nil
}

// refreshState reads the device's state and updates the button.
func (p *plugin) refreshState(ctx context.Context, ev openaction.Event, s Settings) {
	if s.Device == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, deviceTimeout)
		defer cancel()
		client, _, err := p.devices.Connect(ctx, s.Device)
		var on bool
		if err == nil {
			on, err = client.IsOn(ctx)
		}
		if err != nil {
			p.info.Printf("refresh %q failed: %v", s.Name, err)
			return
		}
		if err := p.conn.SetState(ctx, ev.Context, stateIndex(on)); err != nil {
			p.info.Printf("setState for %s failed: %v", ev.Context, err)
		}
	}()
}

func stateIndex(on bool) int {
	if on {
		return stateOn
	}
	return stateOff
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
