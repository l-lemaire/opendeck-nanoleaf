package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/l-lemaire/opendeck-nanoleaf/internal/nanoleaf"
)

// discover implements `nanoleaf discover`: mDNS on the local network.
func (a *app) discover(args []string) error {
	fs := flag.NewFlagSet("nanoleaf discover", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print the result as JSON")
	timeout := fs.Duration("timeout", nanoleaf.DefaultMDNSTimeout, "how long to wait for mDNS answers")
	iface := fs.String("interface", "", "network interface to query on (default: let the kernel choose)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	devices, err := nanoleaf.Discovery{Log: a.log, MDNSTimeout: *timeout, Interface: *iface}.Discover(a.ctx)
	if err != nil {
		return err
	}

	if *asJSON {
		out, err := json.MarshalIndent(devices, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		if len(devices) == 0 {
			return errors.New("no device found")
		}
		return nil
	}
	if len(devices) == 0 {
		return fmt.Errorf("no device found after %s (is it on the same network and powered? try --debug)", *timeout)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tMODEL\tFIRMWARE\tID")
	for _, d := range devices {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", d.Name, d.Addr(), d.Model, d.Firmware, d.ID)
	}
	return w.Flush()
}
