// opendeck-nanoleaf is the OpenDeck plugin. OpenDeck starts it, keeps it
// running for as long as OpenDeck itself runs, and talks to it over a
// WebSocket (see internal/openaction). It has no terminal: everything it has
// to say goes to its log file, whose path `nanoleaf plugin status` prints.
//
// It shares the Nanoleaf client, the device configuration and the credential
// store with the CLI. Pairing can be done from the key panel or with
// `nanoleaf auth`.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/openaction"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		// stderr is usually invisible under OpenDeck, but harmless, and
		// visible when the binary is started by hand for troubleshooting.
		fmt.Fprintln(os.Stderr, "opendeck-nanoleaf:", err)
		os.Exit(1)
	}
}

func run() error {
	args, err := openaction.ParseArgs(os.Args[1:])
	if err != nil {
		return fmt.Errorf("%w\nusage: opendeck-nanoleaf -port N -pluginUUID ID -registerEvent EVENT -info JSON (OpenDeck passes these)", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	info, debug, closeLog, err := openLogs(cfg.PluginDebug)
	if err != nil {
		return err
	}
	defer closeLog()

	info.Printf("opendeck-nanoleaf %s starting: host %s %s on %s, %d device(s), debug=%v",
		version, args.Info.Application.Version, args.Info.Plugin.UUID, args.Info.Application.Platform,
		len(args.Info.Devices), cfg.PluginDebug)
	if debug != nil {
		debug.Printf("launch args: port=%d uuid=%s registerEvent=%s info=%s", args.Port, args.PluginUUID, args.RegisterEvent, args.RawInfo)
	}

	// OpenDeck stops the plugin with SIGTERM when it quits; Ctrl-C matters
	// only when started by hand.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := openaction.Connect(ctx, args, debug)
	if err != nil {
		info.Printf("ERROR %v", err)
		return err
	}
	defer conn.Close()
	info.Printf("connected to OpenDeck on port %d", args.Port)

	p := &plugin{conn: conn, info: info, debug: debug, devices: newFileConnector(debug)}
	err = conn.Run(ctx, p.handlers())
	if err != nil {
		info.Printf("ERROR %v", err)
		return err
	}
	info.Printf("stopped")
	return nil
}

// openLogs opens the log file and returns two loggers writing to it: info
// (always) and debug (nil unless enabled, so the shared packages stay
// silent). The returned function closes the file.
func openLogs(debugEnabled bool) (info, debug *log.Logger, closeFn func(), err error) {
	path, err := config.PluginLogPath()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, nil, err
	}
	// Also mirror to stderr when started by hand from a terminal.
	var w io.Writer = f
	if fileInfo, err := os.Stderr.Stat(); err == nil && fileInfo.Mode()&os.ModeCharDevice != 0 {
		w = io.MultiWriter(f, os.Stderr)
	}
	flags := log.Ldate | log.Ltime | log.Lmicroseconds
	info = log.New(w, "", flags)
	if debugEnabled {
		debug = log.New(w, "debug: ", flags)
	}
	return info, debug, func() { f.Close() }, nil
}

// plugin holds what the event handlers need.
type plugin struct {
	conn     *openaction.Conn
	info     *log.Logger
	debug    *log.Logger
	devices  deviceConnector
	inflight inflightSet

	// mu guards the two maps below, which the event loop and the watcher
	// goroutines both touch.
	mu       sync.Mutex
	buttons  map[string]button // by button context
	watchers map[string]bool   // device ids with a running watchDevice
}

func (p *plugin) handlers() openaction.Handlers {
	return openaction.Handlers{
		WillAppear: func(ctx context.Context, ev openaction.Event, pl openaction.AppearPayload) error {
			p.info.Printf("button appeared: action=%s context=%s at row %d col %d settings=%s",
				shortAction(ev.Action), ev.Context, pl.Coordinates.Row, pl.Coordinates.Column, compact(pl.Settings))
			return p.onSettings(ctx, ev, pl.Settings)
		},
		WillDisappear: func(ctx context.Context, ev openaction.Event, pl openaction.AppearPayload) error {
			p.info.Printf("button removed: action=%s context=%s", shortAction(ev.Action), ev.Context)
			p.forgetButton(ev.Context)
			return nil
		},
		KeyDown: func(ctx context.Context, ev openaction.Event, pl openaction.KeyPayload) error {
			p.info.Printf("key pressed: action=%s context=%s", shortAction(ev.Action), ev.Context)
			return p.onKeyDown(ctx, ev, pl.Settings)
		},
		DidReceiveSettings: func(ctx context.Context, ev openaction.Event, pl openaction.SettingsPayload) error {
			p.info.Printf("settings for %s: %s", ev.Context, compact(pl.Settings))
			return p.onSettings(ctx, ev, pl.Settings)
		},
		SendToPlugin: func(ctx context.Context, ev openaction.Event, payload json.RawMessage) error {
			p.info.Printf("inspector request for %s: %s", ev.Context, compact(payload))
			return p.handleInspectorMessage(ctx, ev, payload)
		},
		Unknown: func(ctx context.Context, ev openaction.Event) error {
			p.info.Printf("event %s ignored", ev.Event)
			return nil
		},
	}
}

// onSettings runs when a button appears or its settings change: show the
// target's name as the title and fetch its real state. Until a target is
// chosen the button is left alone.
func (p *plugin) onSettings(ctx context.Context, ev openaction.Event, raw json.RawMessage) error {
	s, err := decodeSettings(raw)
	if err != nil {
		return err
	}
	if s.Device == "" {
		return nil
	}
	if text, set := s.title(); set {
		if err := p.conn.SetTitle(ctx, ev.Context, text); err != nil {
			return err
		}
	}
	p.trackButton(ctx, ev, s)
	p.refreshState(ctx, ev, s)
	return nil
}

// compact renders raw JSON on one line, or "{}" when empty.
func compact(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	out, _ := json.Marshal(v)
	return string(out)
}
