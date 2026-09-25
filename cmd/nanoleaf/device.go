package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

// targetFlags are the flags every command that talks to a paired device
// shares.
type targetFlags struct {
	device *string
	store  *string
}

func addTargetFlags(fs *flag.FlagSet) *targetFlags {
	return &targetFlags{
		device: fs.String("device", "", "device id (default: the default device from `nanoleaf auth status`)"),
		store:  fs.String("store", secrets.BackendAuto, "credential store: auto, keyring or file"),
	}
}

// openStore opens the credential store with the file fallback next to the
// config file, warning when the weaker backend ends up in use unasked.
func (a *app) openStore(backend string) (secrets.Store, error) {
	dir, err := config.Dir()
	if err != nil {
		return nil, err
	}
	store, err := secrets.Open(backend, filepath.Join(dir, "credentials.json"), a.log)
	if err != nil {
		return nil, err
	}
	if backend != secrets.BackendFile && strings.HasPrefix(store.Name(), "file") {
		fmt.Fprintln(os.Stderr, "warning: no desktop keyring reachable, storing credentials in", store.Name())
	}
	return store, nil
}

// connect loads the device configuration and token and returns a ready
// client. The one place that assembles a client for a paired device.
func (a *app) connect(t *targetFlags) (*nanoleaf.Client, config.Device, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, config.Device{}, err
	}
	d, err := cfg.Resolve(*t.device)
	if err != nil {
		return nil, config.Device{}, err
	}
	store, err := a.openStore(*t.store)
	if err != nil {
		return nil, config.Device{}, err
	}
	creds, err := nanoleaf.LoadCredentials(store, d.ID)
	if err != nil {
		return nil, config.Device{}, fmt.Errorf("%w (run `nanoleaf auth`)", err)
	}
	return nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: a.log}), d, nil
}

// parseArgs parses flags and requires exactly `want` positional words after
// them. Flags must come before the positional words.
func parseArgs(fs *flag.FlagSet, args []string, want int, names ...string) ([]string, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	rest := fs.Args()
	switch {
	case len(rest) < want:
		return nil, fmt.Errorf("%s: missing %s", fs.Name(), names[len(rest)])
	case len(rest) > want:
		extra := rest[want]
		hint := ""
		if len(extra) > 0 && extra[0] == '-' {
			hint = " (flags go before the name)"
		}
		return nil, fmt.Errorf("%s: unexpected argument %q%s", fs.Name(), extra, hint)
	}
	return rest, nil
}
