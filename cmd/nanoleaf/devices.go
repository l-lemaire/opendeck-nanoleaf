package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
)

// The control commands mirror the Hue CLI's verb-then-kind grammar, with a
// single kind for now:
//
//	nanoleaf list   devices
//	nanoleaf on     device [name or id]      (the default device when omitted)
//	nanoleaf off    device [name or id]
//	nanoleaf toggle device [name or id]

func parseKind(word string) error {
	switch word {
	case "device", "devices":
		return nil
	default:
		return fmt.Errorf("unknown kind %q (want: device)", word)
	}
}

// list implements `nanoleaf list devices`: every paired device with its
// live state.
func (a *app) list(args []string) error {
	if len(args) == 0 {
		return errors.New("list: what to list is required (devices)")
	}
	if err := parseKind(args[0]); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	fs := flag.NewFlagSet("nanoleaf list devices", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	backend := fs.String("store", "auto", "credential store: auto, keyring or file")
	if err := parseFlags(fs, args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Devices) == 0 {
		return errors.New("no device paired yet; run `nanoleaf auth`")
	}
	store, err := a.openStore(*backend)
	if err != nil {
		return err
	}

	type row struct {
		config.Device
		On         *bool  `json:"on,omitempty"`
		Brightness *int   `json:"brightness,omitempty"`
		Effect     string `json:"effect,omitempty"`
		Error      string `json:"error,omitempty"`
	}
	var rows []row
	for _, d := range cfg.Devices {
		r := row{Device: d}
		creds, err := nanoleaf.LoadCredentials(store, d.ID)
		if err != nil {
			r.Error = err.Error()
		} else {
			client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: a.log})
			info, err := client.Info(a.ctx)
			if err != nil {
				r.Error = err.Error()
			} else {
				on, b := info.State.On.Value, info.State.Brightness.Value
				r.On, r.Brightness, r.Effect = &on, &b, info.Effects.Selected
			}
		}
		rows = append(rows, r)
	}
	if *asJSON {
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tBRIGHT\tEFFECT\tADDRESS\tID")
	for _, r := range rows {
		state, bright := "?", "-"
		if r.Error != "" {
			state = "error: " + r.Error
		} else if r.On != nil {
			state = onOff(*r.On)
			bright = fmt.Sprintf("%d%%", *r.Brightness)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, state, bright, r.Effect, r.Addr(), r.ID)
	}
	return w.Flush()
}

// power implements `nanoleaf on|off|toggle device [name]`.
func (a *app) power(verb string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s: what to switch is required (device)", verb)
	}
	if err := parseKind(args[0]); err != nil {
		return fmt.Errorf("%s: %w", verb, err)
	}
	fs := flag.NewFlagSet("nanoleaf "+verb+" device", flag.ContinueOnError)
	backend := fs.String("store", "auto", "credential store: auto, keyring or file")
	dryRun := fs.Bool("dry-run", false, "print the request that would change the device instead of sending it")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) > 1 {
		hint := ""
		if strings.HasPrefix(rest[1], "-") {
			hint = " (flags go before the name)"
		}
		return fmt.Errorf("%s: unexpected argument %q%s", fs.Name(), rest[1], hint)
	}
	query := ""
	if len(rest) == 1 {
		query = rest[0]
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := matchDevice(cfg, query)
	if err != nil {
		return err
	}
	store, err := a.openStore(*backend)
	if err != nil {
		return err
	}
	creds, err := nanoleaf.LoadCredentials(store, d.ID)
	if err != nil {
		return fmt.Errorf("%w (run `nanoleaf auth`)", err)
	}
	var dry io.Writer
	if *dryRun {
		dry = os.Stdout
	}
	client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: a.log, DryRun: dry})

	var nowOn bool
	switch verb {
	case "on":
		nowOn, err = true, client.SetOn(a.ctx, true)
	case "off":
		nowOn, err = false, client.SetOn(a.ctx, false)
	default:
		nowOn, err = client.Toggle(a.ctx)
	}
	if err != nil {
		return err
	}
	if !*dryRun {
		fmt.Printf("device %s: %s\n", d.Name, onOff(nowOn))
	}
	return nil
}

// matchDevice resolves a name or id among the paired devices: empty means
// the default; otherwise exact id, exact name (case-insensitive), then a
// unique prefix of either.
func matchDevice(cfg *config.Config, query string) (config.Device, error) {
	if query == "" {
		return cfg.Resolve("")
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var prefix []config.Device
	for _, d := range cfg.Devices {
		id, name := strings.ToLower(d.ID), strings.ToLower(d.Name)
		if id == q || name == q {
			return d, nil
		}
		if strings.HasPrefix(id, q) || strings.HasPrefix(name, q) {
			prefix = append(prefix, d)
		}
	}
	switch len(prefix) {
	case 1:
		return prefix[0], nil
	case 0:
		return config.Device{}, fmt.Errorf("no paired device matches %q (see `nanoleaf list devices`)", query)
	default:
		var names []string
		for _, d := range prefix {
			names = append(names, fmt.Sprintf("%s (%s)", d.Name, d.ID))
		}
		return config.Device{}, fmt.Errorf("%q is ambiguous, matches: %s", query, strings.Join(names, ", "))
	}
}

// watch implements `nanoleaf watch`: print every change the device reports.
func (a *app) watch(args []string) error {
	fs := flag.NewFlagSet("nanoleaf watch", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print one JSON object per change")
	target := addTargetFlags(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	client, d, err := a.connect(target)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Watching %s (%s); Ctrl-C to stop.\n", d.Name, d.Addr())
	client.Watch(a.ctx, nanoleaf.EventHandler{
		OnConnect: func() { fmt.Fprintln(os.Stderr, "connected to the event stream") },
		OnError: func(err error, retryIn time.Duration) {
			fmt.Fprintf(os.Stderr, "stream lost: %v; retrying in %s\n", err, retryIn)
		},
		OnChange: func(ch nanoleaf.Change) {
			if *asJSON {
				out, _ := json.Marshal(ch)
				fmt.Println(string(out))
				return
			}
			fmt.Printf("%s %s: %s\n", time.Now().Format("15:04:05"), d.Name, ch)
		},
	})
	return nil
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
