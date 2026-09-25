package nanoleaf

import (
	"context"
	"log"
	"net"
	"sort"
	"strconv"
	"time"
)

// DefaultPort is where every Nanoleaf device serves its API.
const DefaultPort = 16021

// Device is a Nanoleaf installation found on the network, before pairing.
type Device struct {
	// ID is the controller's hardware address as announced over mDNS,
	// lower case, e.g. "00:55:da:53:3b:9b". It is the key under which the
	// token is stored.
	ID string `json:"id"`
	// Host is the IP address to connect to.
	Host string `json:"host"`
	// Port is the HTTP port, DefaultPort unless something exotic is going on.
	Port int `json:"port"`
	// Name is the mDNS instance name, e.g. "Nanoleaf Light Panels 53:3b:9b".
	Name string `json:"name,omitempty"`
	// Model is the hardware model code, e.g. "NL22" (Light Panels).
	Model string `json:"model,omitempty"`
	// Firmware is the version announced over mDNS.
	Firmware string `json:"firmware,omitempty"`
}

// Addr returns "host:port".
func (d Device) Addr() string {
	return net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
}

// Discovery finds devices. The zero value is usable. Nanoleaf has no cloud
// lookup, so mDNS is the only mechanism; a manual address remains the
// fallback for networks that block it.
type Discovery struct {
	Log         *log.Logger
	MDNSTimeout time.Duration
	// Interface is the network interface to query on; empty lets the kernel
	// choose.
	Interface string
}

// Discover returns every device on the local network. The result may be
// empty with a nil error when the query worked but nobody answered.
func (d Discovery) Discover(ctx context.Context) ([]Device, error) {
	return d.MDNS(ctx)
}

func sortDevices(devices []Device) {
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name+devices[i].ID < devices[j].Name+devices[j].ID })
}

func itoa(n int) string { return strconv.Itoa(n) }
