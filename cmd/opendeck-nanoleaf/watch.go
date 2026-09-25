package main

import (
	"context"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/openaction"
)

// Live state: one event stream per device that a button references, and a
// registry of buttons so a change updates every key showing that device.

type button struct {
	action   string
	settings Settings
}

// trackButton records a configured button and starts watching its device
// if not already watched. Called on willAppear and didReceiveSettings.
func (p *plugin) trackButton(ctx context.Context, ev openaction.Event, s Settings) {
	p.mu.Lock()
	if p.buttons == nil {
		p.buttons = map[string]button{}
		p.watchers = map[string]bool{}
	}
	p.buttons[ev.Context] = button{action: ev.Action, settings: s}
	start := !p.watchers[s.Device]
	p.watchers[s.Device] = true
	p.mu.Unlock()
	if start {
		go p.watchDevice(ctx, s.Device)
	}
}

func (p *plugin) forgetButton(buttonContext string) {
	p.mu.Lock()
	delete(p.buttons, buttonContext)
	p.mu.Unlock()
}

// watchDevice runs for the plugin's lifetime for one device.
func (p *plugin) watchDevice(ctx context.Context, deviceID string) {
	var client *nanoleaf.Client
	for client == nil {
		c, d, err := p.devices.Connect(ctx, deviceID)
		if err == nil {
			client = c
			p.info.Printf("watching %s (%s) for changes", d.Name, d.ID)
			break
		}
		p.info.Printf("cannot watch device %q yet: %v (retry in 30s)", deviceID, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
	client.Watch(ctx, nanoleaf.EventHandler{
		OnConnect: func() {
			p.info.Printf("event stream connected (device %s); refreshing buttons", deviceID)
			p.refreshAll(ctx, deviceID)
		},
		OnChange: func(ch nanoleaf.Change) {
			if ch.On != nil {
				p.applyChange(ctx, deviceID, *ch.On)
			}
		},
		OnError: func(err error, retryIn time.Duration) {
			p.info.Printf("event stream lost (device %s): %v; retrying in %s", deviceID, err, retryIn)
		},
	})
}

// applyChange updates every button showing the device.
func (p *plugin) applyChange(ctx context.Context, deviceID string, on bool) {
	p.mu.Lock()
	var affected []string
	for buttonContext, b := range p.buttons {
		if b.settings.Device == deviceID {
			affected = append(affected, buttonContext)
		}
	}
	p.mu.Unlock()
	for _, buttonContext := range affected {
		p.info.Printf("device %s is now %s; updating button %s", deviceID, onOff(on), buttonContext)
		if err := p.conn.SetState(ctx, buttonContext, stateIndex(on)); err != nil {
			p.info.Printf("setState for %s failed: %v", buttonContext, err)
		}
	}
}

// refreshAll re-reads the state of every button on one device.
func (p *plugin) refreshAll(ctx context.Context, deviceID string) {
	p.mu.Lock()
	type item struct {
		ctx string
		b   button
	}
	var todo []item
	for buttonContext, b := range p.buttons {
		if b.settings.Device == deviceID {
			todo = append(todo, item{buttonContext, b})
		}
	}
	p.mu.Unlock()
	for _, it := range todo {
		p.refreshState(ctx, openaction.Event{Action: it.b.action, Context: it.ctx}, it.b.settings)
	}
}
