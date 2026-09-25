package openaction

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
)

// Args are the four command-line arguments the host passes at launch.
type Args struct {
	Port          int
	PluginUUID    string
	RegisterEvent string
	Info          Info
	// RawInfo keeps the -info JSON as received, for debug output.
	RawInfo string
}

// Info describes the host, taken from the -info argument.
type Info struct {
	Application struct {
		Font            string `json:"font"`
		Language        string `json:"language"`
		Platform        string `json:"platform"`
		PlatformVersion string `json:"platformVersion"`
		Version         string `json:"version"`
	} `json:"application"`
	Plugin struct {
		UUID    string `json:"uuid"`
		Version string `json:"version"`
	} `json:"plugin"`
	Devices []Device `json:"devices"`
}

// Device is one connected deck.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Size struct {
		Rows    int `json:"rows"`
		Columns int `json:"columns"`
	} `json:"size"`
}

// ParseArgs reads the launch arguments (os.Args[1:]). The host uses
// single-dash long flags such as -port, which Go's flag package accepts.
func ParseArgs(argv []string) (Args, error) {
	var a Args
	fs := flag.NewFlagSet("plugin", flag.ContinueOnError)
	fs.IntVar(&a.Port, "port", 0, "WebSocket port of the host")
	fs.StringVar(&a.PluginUUID, "pluginUUID", "", "identifier to register with")
	fs.StringVar(&a.RegisterEvent, "registerEvent", "", "name of the registration event")
	fs.StringVar(&a.RawInfo, "info", "", "JSON describing the host")
	if err := fs.Parse(argv); err != nil {
		return Args{}, err
	}
	if fs.NArg() > 0 {
		return Args{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	switch {
	case a.Port <= 0 || a.Port > 65535:
		return Args{}, errors.New("-port is required and must be 1-65535")
	case a.PluginUUID == "":
		return Args{}, errors.New("-pluginUUID is required")
	case a.RegisterEvent == "":
		return Args{}, errors.New("-registerEvent is required")
	}
	if a.RawInfo != "" {
		if err := json.Unmarshal([]byte(a.RawInfo), &a.Info); err != nil {
			return Args{}, fmt.Errorf("-info is not valid JSON: %w", err)
		}
	}
	return a, nil
}
