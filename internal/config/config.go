// Package config persists the non-secret state of the CLI and plugin: which
// Nanoleaf devices are known, where they are, and which one is the default.
// Secrets (the auth tokens) live in package secrets, never here.
//
// The file is JSON at $XDG_CONFIG_HOME/nanoleaf/config.json, which on Linux
// resolves to ~/.config/nanoleaf/config.json. It is safe to read, share and
// edit by hand.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Device is one paired (or about to be paired) Nanoleaf device: a set of
// panels with its controller.
type Device struct {
	ID       string    `json:"id"`
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	Name     string    `json:"name,omitempty"`
	Model    string    `json:"model,omitempty"`
	Firmware string    `json:"firmware,omitempty"`
	PairedAt time.Time `json:"paired_at"`
}

// Addr returns host:port.
func (d Device) Addr() string {
	return d.Host + ":" + strconv.Itoa(d.Port)
}

// Config is the whole file.
type Config struct {
	Version       int               `json:"version"`
	DefaultDevice string            `json:"default_device,omitempty"`
	Devices       map[string]Device `json:"devices"`
	// PluginDebug makes the OpenDeck plugin write full debug output
	// (protocol messages, HTTP dumps) to its log file. The plugin has no
	// command line of its own, so this is how --debug reaches it.
	PluginDebug bool `json:"plugin_debug,omitempty"`

	path string // where it was loaded from; not serialised (lower-case field)
}

// Dir returns the directory holding config.json and the credentials
// fallback file. It honours $XDG_CONFIG_HOME through os.UserConfigDir.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "nanoleaf"), nil
}

// PluginLogPath returns the plugin's log file, under $XDG_STATE_HOME
// (default ~/.local/state), the XDG location for logs and other state that
// is neither configuration nor cache.
func PluginLogPath() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "opendeck-nanoleaf", "plugin.log"), nil
}

// Load reads the config file. A missing file yields an empty, usable Config.
func Load() (*Config, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(filepath.Join(dir, "config.json"))
}

// LoadFrom is Load with an explicit path, for tests and unusual setups.
func LoadFrom(path string) (*Config, error) {
	cfg := &Config{Version: 1, Devices: map[string]Device{}, path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("config %s is corrupt: %w", path, err)
	}
	if cfg.Devices == nil {
		cfg.Devices = map[string]Device{}
	}
	return cfg, nil
}

// Path returns where Save will write.
func (c *Config) Path() string { return c.path }

// Save writes the file with owner-only permissions. Nothing in it is secret,
// but there is no reason for other users to read it either.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, append(raw, '\n'), 0o600)
}

// Put adds or replaces a device and makes it the default if none is set.
func (c *Config) Put(d Device) {
	c.Devices[d.ID] = d
	if c.DefaultDevice == "" {
		c.DefaultDevice = d.ID
	}
}

// Remove forgets a device, moving the default to another one if needed.
func (c *Config) Remove(id string) {
	delete(c.Devices, id)
	if c.DefaultDevice == id {
		c.DefaultDevice = ""
		for other := range c.Devices {
			c.DefaultDevice = other
			break
		}
	}
}

// Resolve returns the device with the given id, or the default when id is
// empty. The error explains what to do when nothing matches.
func (c *Config) Resolve(id string) (Device, error) {
	if id == "" {
		id = c.DefaultDevice
	}
	if id == "" {
		return Device{}, errors.New("no device paired yet; run `nanoleaf auth`")
	}
	d, ok := c.Devices[id]
	if !ok {
		return Device{}, fmt.Errorf("device %s is not paired; run `nanoleaf auth` or check `nanoleaf auth status`", id)
	}
	return d, nil
}
