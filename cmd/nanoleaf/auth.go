package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/config"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/pairing"
	"github.com/l-lemaire/opendeck-nanoleaf/internal/secrets"
)

// auth dispatches `nanoleaf auth`, `auth status` and `auth forget`.
func (a *app) auth(args []string) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "status":
			return a.authStatus(args[1:])
		case "forget":
			return a.authForget(args[1:])
		default:
			return fmt.Errorf("auth: unknown subcommand %q (want: status, forget, or flags for pairing)", args[0])
		}
	}
	return a.authPair(args)
}

// authPair implements `nanoleaf auth`: the shared pairing flow with progress
// printed to the terminal.
func (a *app) authPair(args []string) error {
	fs := flag.NewFlagSet("nanoleaf auth", flag.ContinueOnError)
	ip := fs.String("ip", "", "device address (host or host:port) instead of discovery")
	id := fs.String("id", "", "device id to pair with when several are found")
	backend := fs.String("store", secrets.BackendAuto, "where to keep the token: auto, keyring or file")
	timeout := fs.Duration("timeout", pairing.DefaultTimeout, "how long to wait for the button")
	force := fs.Bool("force", false, "pair again even if this device already has a token")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	store, err := a.openStore(*backend)
	if err != nil {
		return err
	}

	lastLeft := -1
	_, err = pairing.Run(a.ctx, pairing.Options{
		Addr: *ip, ID: *id, Store: store, Timeout: *timeout, Force: *force, Log: a.log,
		Report: func(p pairing.Progress) {
			switch p.Stage {
			case pairing.StageWaiting:
				if lastLeft < 0 {
					fmt.Printf("\n%s.\nWaiting up to %ds ", p.Message, p.SecondsLeft)
				} else if p.SecondsLeft/10 != lastLeft/10 {
					fmt.Printf(" %ds", p.SecondsLeft) // a tick every ten seconds
				}
				lastLeft = p.SecondsLeft
			default:
				if lastLeft >= 0 {
					fmt.Println()
					lastLeft = -1
				}
				fmt.Println(p.Message)
			}
		},
	})
	if lastLeft >= 0 {
		fmt.Println()
	}
	if err != nil {
		if errors.Is(err, pairing.ErrAlreadyPaired) {
			return fmt.Errorf("%v; use --force to pair again", err)
		}
		return err
	}
	dir, _ := config.Dir()
	fmt.Printf("Device details saved in %s.\n", filepath.Join(dir, "config.json"))
	return nil
}

// authStatus implements `nanoleaf auth status`.
func (a *app) authStatus(args []string) error {
	fs := flag.NewFlagSet("nanoleaf auth status", flag.ContinueOnError)
	backend := fs.String("store", secrets.BackendAuto, "credential store to read: auto, keyring or file")
	offline := fs.Bool("offline", false, "do not contact the devices")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(cfg.Devices) == 0 {
		fmt.Println("No device paired. Run `nanoleaf auth`.")
		return nil
	}
	store, err := a.openStore(*backend)
	if err != nil {
		return err
	}
	fmt.Println("Credential store:", store.Name())
	fmt.Println("Config file:     ", cfg.Path())
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tADDRESS\tNAME\tMODEL\tDEFAULT\tTOKEN\tSTATUS")
	for _, d := range cfg.Devices {
		isDefault := ""
		if d.ID == cfg.DefaultDevice {
			isDefault = "*"
		}
		tokenState, status := "present", "not checked"
		creds, err := nanoleaf.LoadCredentials(store, d.ID)
		switch {
		case errors.Is(err, secrets.ErrNotFound):
			tokenState, status = "missing", "run `nanoleaf auth --force`"
		case err != nil:
			tokenState, status = "error", err.Error()
		case !*offline:
			client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: a.log})
			info, err := client.Info(a.ctx)
			switch {
			case err != nil:
				status = err.Error()
			case info.State.On.Value:
				status = fmt.Sprintf("ok, on at %d%%", info.State.Brightness.Value)
			default:
				status = "ok, off"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Addr(), d.Name, d.Model, isDefault, tokenState, status)
	}
	return w.Flush()
}

// authForget implements `nanoleaf auth forget`: revokes the token on the
// device when reachable, then removes it locally along with the device.
func (a *app) authForget(args []string) error {
	fs := flag.NewFlagSet("nanoleaf auth forget", flag.ContinueOnError)
	id := fs.String("id", "", "device id (default: the default device)")
	backend := fs.String("store", secrets.BackendAuto, "credential store: auto, keyring or file")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	keep := fs.Bool("keep-remote", false, "do not revoke the token on the device")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := cfg.Resolve(*id)
	if err != nil {
		return err
	}
	store, err := a.openStore(*backend)
	if err != nil {
		return err
	}
	if !*yes {
		fmt.Printf("Forget device %s (%s) and delete its token from the %s? [y/N] ", d.ID, d.Name, store.Name())
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return errors.New("aborted")
		}
	}

	// Nanoleaf lets a token delete itself, so unlike Hue the device can be
	// left clean. Failure to reach it is reported but does not stop the
	// local cleanup.
	if creds, err := nanoleaf.LoadCredentials(store, d.ID); err == nil && !*keep {
		client := nanoleaf.NewClient(nanoleaf.ClientOptions{Addr: d.Addr(), Token: creds.Token, Log: a.log})
		if err := client.DeleteToken(a.ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not revoke the token on the device: %v\n", err)
		} else {
			fmt.Println("Token revoked on the device.")
		}
	}
	if err := store.Delete(d.ID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return err
	}
	cfg.Remove(d.ID)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Forgot device %s.\n", d.ID)
	return nil
}
