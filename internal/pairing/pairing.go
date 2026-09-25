// Package pairing is the one place that knows how to pair with a Nanoleaf
// device: find it, wait while the user holds its power button, obtain a
// token, verify it, store it and record the device. The CLI and the plugin
// both call Run and only differ in how they show progress.
package pairing

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

// DefaultTimeout is how long Run keeps asking the device for a token. The
// user needs a few seconds to hold the button, and the device then accepts
// requests for about thirty seconds.
const DefaultTimeout = 90 * time.Second

// Instruction is the sentence shown while waiting. Same text everywhere.
const Instruction = "Hold the power button on the controller for 5 to 7 seconds, until the LEDs start flashing"

// Stage names reported through Options.Report, in the order they happen.
const (
	StageSearching = "searching"
	StageFound     = "found"
	StageWaiting   = "waiting"
	StagePaired    = "paired"
)

// Progress is one step of the flow, for display.
type Progress struct {
	Stage       string
	Message     string
	SecondsLeft int // meaningful during StageWaiting
}

// Options configures one pairing run.
type Options struct {
	// Addr is a manual "host" or "host:port"; empty means discover.
	Addr string
	// ID selects a device when discovery finds several. With Addr it names
	// the device in the config when mDNS is not consulted.
	ID string
	// Store receives the token. Required.
	Store secrets.Store
	// ConfigPath overrides the config file location (tests).
	ConfigPath string
	// Timeout for the button. Zero means DefaultTimeout.
	Timeout time.Duration
	// Force pairs again even if the store already has a token for the device.
	Force bool
	// Report, if set, is called at each stage. It must return quickly.
	Report func(Progress)
	Log    *log.Logger
}

// ErrAlreadyPaired is returned when the device has a token and Force is off.
var ErrAlreadyPaired = errors.New("device already paired")

// pollInterval is how often the token is requested; a variable so tests
// can hurry.
var pollInterval = time.Second

// Run performs the whole flow and returns the recorded device.
func Run(ctx context.Context, o Options) (config.Device, error) {
	if o.Store == nil {
		return config.Device{}, errors.New("pairing: no credential store")
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	report := o.Report
	if report == nil {
		report = func(Progress) {}
	}
	cfg, err := loadConfig(o.ConfigPath)
	if err != nil {
		return config.Device{}, err
	}

	// Step 1: decide which device to talk to.
	if o.Addr != "" {
		report(Progress{Stage: StageSearching, Message: "Using the device at " + o.Addr})
	} else {
		report(Progress{Stage: StageSearching, Message: "Looking for Nanoleaf devices on the network…"})
	}
	dev, err := pickDevice(ctx, o)
	if err != nil {
		return config.Device{}, err
	}
	report(Progress{Stage: StageFound, Message: fmt.Sprintf("Found %s at %s", displayName(dev), dev.Addr())})

	// Step 2: refuse to silently create a second token for a paired device.
	if !o.Force {
		if _, err := nanoleaf.LoadCredentials(o.Store, dev.ID); err == nil {
			return dev, fmt.Errorf("%w: %s already has a token in the %s", ErrAlreadyPaired, dev.ID, o.Store.Name())
		}
	}

	// Step 3: ask for a token until the device is in pairing mode.
	client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: dev.Addr(), Log: o.Log})
	deadline := time.Now().Add(timeout)
	report(Progress{Stage: StageWaiting, Message: Instruction, SecondsLeft: int(timeout.Seconds())})
	var token string
	for {
		token, err = client.RequestToken(ctx)
		if err == nil {
			break
		}
		if !errors.Is(err, nanoleaf.ErrNotInPairingMode) {
			return dev, err
		}
		left := int(time.Until(deadline).Seconds() + 0.5)
		if left <= 0 {
			return dev, fmt.Errorf("the device did not enter pairing mode within %s", timeout.Round(time.Second))
		}
		report(Progress{Stage: StageWaiting, Message: Instruction, SecondsLeft: left})
		select {
		case <-ctx.Done():
			return dev, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Step 4: verify the token and learn the device's own name and model.
	authed := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: dev.Addr(), Token: token, Log: o.Log})
	info, err := authed.Info(ctx)
	if err != nil {
		return dev, fmt.Errorf("token obtained but the device rejected it: %w", err)
	}
	if info.Name != "" {
		dev.Name = info.Name
	}
	if info.Model != "" {
		dev.Model = info.Model
	}
	if info.Firmware != "" {
		dev.Firmware = info.Firmware
	}

	// Step 5: persist. Secret in the store, everything else in the config.
	if err := nanoleaf.SaveCredentials(o.Store, dev.ID, nanoleaf.Credentials{Token: token}); err != nil {
		return dev, err
	}
	dev.PairedAt = time.Now()
	cfg.Put(dev)
	if err := cfg.Save(); err != nil {
		return dev, err
	}
	report(Progress{Stage: StagePaired, Message: fmt.Sprintf("Paired with %s. Token saved in the %s.", dev.Name, o.Store.Name())})
	return dev, nil
}

// pickDevice turns Addr/ID into a config.Device, discovering when needed.
func pickDevice(ctx context.Context, o Options) (config.Device, error) {
	if o.Addr != "" {
		addr := o.Addr
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, "16021")
		}
		host, portText, _ := net.SplitHostPort(addr)
		port := 16021
		fmt.Sscanf(portText, "%d", &port)
		id := strings.ToLower(o.ID)
		if id == "" {
			// No mDNS id available: the address is the only stable handle.
			id = host
		}
		return config.Device{ID: id, Host: host, Port: port}, nil
	}

	found, err := nanoleaf.Discovery{Log: o.Log}.Discover(ctx)
	if err != nil {
		return config.Device{}, err
	}
	id := strings.ToLower(o.ID)
	switch {
	case len(found) == 0:
		return config.Device{}, errors.New("no Nanoleaf device found on the network; give its address")
	case id != "":
		for _, d := range found {
			if d.ID == id {
				return toConfig(d), nil
			}
		}
		return config.Device{}, fmt.Errorf("device %s not found on the network", id)
	case len(found) > 1:
		var names []string
		for _, d := range found {
			names = append(names, fmt.Sprintf("%s (%s, %s)", d.Name, d.ID, d.Addr()))
		}
		return config.Device{}, fmt.Errorf("several devices found, choose one: %s", strings.Join(names, ", "))
	default:
		return toConfig(found[0]), nil
	}
}

func toConfig(d nanoleaf.Device) config.Device {
	return config.Device{ID: d.ID, Host: d.Host, Port: d.Port, Name: d.Name, Model: d.Model, Firmware: d.Firmware}
}

func displayName(d config.Device) string {
	if d.Name != "" {
		return d.Name
	}
	return "Nanoleaf device"
}

func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.LoadFrom(path)
	}
	return config.Load()
}
