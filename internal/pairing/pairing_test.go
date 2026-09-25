package pairing

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf/nanoleaftest"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

type memStore map[string]string

func (m memStore) Name() string { return "memory" }
func (m memStore) Get(k string) (string, error) {
	v, ok := m[k]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (m memStore) Set(k, v string) error { m[k] = v; return nil }
func (m memStore) Delete(k string) error { delete(m, k); return nil }

func fastPolling(t *testing.T) {
	old := pollInterval
	pollInterval = 20 * time.Millisecond
	t.Cleanup(func() { pollInterval = old })
}

func TestRunPairsAndRecords(t *testing.T) {
	fastPolling(t)
	fd := nanoleaftest.New(t)
	fd.Refusals.Store(3) // pairing mode "entered" on the 4th request
	store := memStore{}
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	var stages []string
	var lastLeft int
	dev, err := Run(context.Background(), Options{
		Addr: fd.Addr(), ID: "aa:bb:cc:dd:ee:ff", Store: store, ConfigPath: cfgPath, Timeout: 5 * time.Second,
		Report: func(p Progress) {
			stages = append(stages, p.Stage)
			if p.Stage == StageWaiting {
				lastLeft = p.SecondsLeft
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dev.ID != "aa:bb:cc:dd:ee:ff" || dev.Name != "Fake Aurora" || dev.Model != "NL22" || dev.Firmware != "5.3.1" {
		t.Errorf("device = %+v", dev)
	}
	if store["aa:bb:cc:dd:ee:ff"] != `{"token":"`+fd.Token+`"}` {
		t.Errorf("stored credentials = %q", store["aa:bb:cc:dd:ee:ff"])
	}
	cfg, _ := config.LoadFrom(cfgPath)
	if got, err := cfg.Resolve(""); err != nil || got.ID != dev.ID || got.Addr() != fd.Addr() {
		t.Errorf("config default device = %+v, %v", got, err)
	}
	if stages[0] != StageSearching || stages[1] != StageFound || stages[2] != StageWaiting || stages[len(stages)-1] != StagePaired {
		t.Errorf("stages = %v", stages)
	}
	if lastLeft <= 0 || lastLeft > 5 {
		t.Errorf("countdown = %d", lastLeft)
	}

	// Second run without Force is refused before asking the device.
	_, err = Run(context.Background(), Options{Addr: fd.Addr(), ID: "aa:bb:cc:dd:ee:ff", Store: store, ConfigPath: cfgPath})
	if !errors.Is(err, ErrAlreadyPaired) {
		t.Errorf("second pairing: got %v, want ErrAlreadyPaired", err)
	}
}

func TestRunTimesOutWhenButtonNotHeld(t *testing.T) {
	fastPolling(t)
	fd := nanoleaftest.New(t)
	fd.Refusals.Store(1_000_000)
	_, err := Run(context.Background(), Options{
		Addr: fd.Addr(), Store: memStore{}, ConfigPath: filepath.Join(t.TempDir(), "c.json"), Timeout: 300 * time.Millisecond,
	})
	if err == nil || errors.Is(err, ErrAlreadyPaired) {
		t.Fatalf("expected a timeout error, got %v", err)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	fastPolling(t)
	fd := nanoleaftest.New(t)
	fd.Refusals.Store(1_000_000)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(60 * time.Millisecond); cancel() }()
	_, err := Run(ctx, Options{Addr: fd.Addr(), Store: memStore{}, ConfigPath: filepath.Join(t.TempDir(), "c.json"), Timeout: 10 * time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
