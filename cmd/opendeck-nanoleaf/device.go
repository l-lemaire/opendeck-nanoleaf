package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/pairing"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

// deviceConnector hands out a ready nanoleaf.Client for a device id (empty
// for the default). It reads the same config file and credential store as
// the CLI and caches one client per device. It is an interface so tests can
// substitute a connector pointing at a fake device.
type deviceConnector interface {
	Connect(ctx context.Context, deviceID string) (*nanoleaf.Client, config.Device, error)
	// Devices lists the paired devices for the property inspector.
	Devices() ([]config.Device, string, error)
	// Discover finds devices on the network, for the pairing wizard.
	Discover(ctx context.Context) ([]nanoleaf.Device, error)
	// Pair runs the pairing flow (see internal/pairing), reporting progress.
	Pair(ctx context.Context, addr, id string, report func(pairing.Progress)) (config.Device, error)
}

// fileConnector is the real implementation backed by ~/.config/nanoleaf.
type fileConnector struct {
	log *log.Logger

	mu      sync.Mutex
	clients map[string]*nanoleaf.Client
}

func newFileConnector(debug *log.Logger) *fileConnector {
	return &fileConnector{log: debug, clients: map[string]*nanoleaf.Client{}}
}

func (f *fileConnector) Devices() ([]config.Device, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	devices := make([]config.Device, 0, len(cfg.Devices))
	for _, d := range cfg.Devices {
		devices = append(devices, d)
	}
	return devices, cfg.DefaultDevice, nil
}

func (f *fileConnector) Discover(ctx context.Context) ([]nanoleaf.Device, error) {
	return nanoleaf.Discovery{Log: f.log}.Discover(ctx)
}

func (f *fileConnector) Pair(ctx context.Context, addr, id string, report func(pairing.Progress)) (config.Device, error) {
	store, err := f.store()
	if err != nil {
		return config.Device{}, err
	}
	return pairing.Run(ctx, pairing.Options{Addr: addr, ID: id, Store: store, Report: report, Log: f.log})
}

func (f *fileConnector) store() (secrets.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	return secrets.Open(secrets.BackendAuto, filepath.Join(dir, "credentials.json"), f.log)
}

func (f *fileConnector) Connect(ctx context.Context, deviceID string) (*nanoleaf.Client, config.Device, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Device{}, err
	}
	d, err := cfg.Resolve(deviceID)
	if err != nil {
		return nil, config.Device{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[d.ID]; ok {
		return c, d, nil
	}
	store, err := f.store()
	if err != nil {
		return nil, config.Device{}, err
	}
	creds, err := nanoleaf.LoadCredentials(store, d.ID)
	if err != nil {
		return nil, config.Device{}, fmt.Errorf("%w (pair from the key panel or with `nanoleaf auth`)", err)
	}
	c := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: f.log})
	f.clients[d.ID] = c
	return c, d, nil
}
