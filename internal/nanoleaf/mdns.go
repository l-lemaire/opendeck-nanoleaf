package nanoleaf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Nanoleaf devices advertise themselves with multicast DNS under the
// service type "_nanoleafapi._tcp.local.". A query for that name is answered
// by each device with:
//
//	PTR  _nanoleafapi._tcp.local.        -> "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local."
//	SRV  <instance>                      -> target Nanoleaf-Light-Panels-53-3b-9b.local., port 16021
//	TXT  <instance>                      -> id=00:55:DA:53:3B:9B  md=NL22  srcvers=3.3.3
//	A    Nanoleaf-Light-Panels-....local. -> 192.168.1.42
//
// The listening strategy is the same as for Hue and for the same reasons:
// join the multicast group on port 5353 (firewalls drop answers sent to a
// random port), and re-send the question every second because a responder
// will not repeat an answer within one second (RFC 6762 §6).

const serviceName = "_nanoleafapi._tcp.local."

// mdnsGroup is the IPv4 mDNS multicast address and port.
var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

// Test hooks: a fake responder on localhost and a faster retry.
var (
	mdnsDestination   = mdnsGroup
	mdnsListen        = listenMulticast
	mdnsRetryInterval = time.Second
)

func listenMulticast(ifi *net.Interface) (*net.UDPConn, error) {
	return net.ListenMulticastUDP("udp4", ifi, mdnsGroup)
}

// DefaultMDNSTimeout is how long MDNS listens when the caller does not set
// Discovery.MDNSTimeout.
const DefaultMDNSTimeout = 2 * time.Second

// MDNS looks for devices on the local network and returns every distinct
// one that answered before the timeout.
func (d Discovery) MDNS(ctx context.Context) ([]Device, error) {
	timeout := d.MDNSTimeout
	if timeout <= 0 {
		timeout = DefaultMDNSTimeout
	}

	var ifi *net.Interface
	if d.Interface != "" {
		var err error
		if ifi, err = net.InterfaceByName(d.Interface); err != nil {
			return nil, fmt.Errorf("mdns: interface %q: %w", d.Interface, err)
		}
	}
	conn, err := mdnsListen(ifi)
	if err != nil {
		return nil, fmt.Errorf("mdns: open socket: %w", err)
	}
	defer conn.Close()

	query, err := buildQuery(serviceName, dnsTypePTR)
	if err != nil {
		return nil, err
	}
	sendQuery := func() error {
		if _, err := conn.WriteToUDP(query, mdnsDestination); err != nil {
			return fmt.Errorf("mdns: send query: %w", err)
		}
		debugf(d.Log, "mdns: sent %d-byte PTR query for %s to %s from %s (interface: %s)",
			len(query), serviceName, mdnsDestination, conn.LocalAddr(), interfaceName(ifi))
		return nil
	}
	if err := sendQuery(); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	stop := context.AfterFunc(ctx, func() { conn.SetReadDeadline(time.Now()) })
	defer stop()

	found := make(map[string]Device)
	buf := make([]byte, 9000)
	ignored := 0
	nextRetry := time.Now().Add(mdnsRetryInterval)

	for {
		wake := nextRetry
		if deadline.Before(wake) {
			wake = deadline
		}
		if ctx.Err() == nil {
			if err := conn.SetReadDeadline(wake); err != nil {
				return nil, err
			}
		}

		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) {
				return nil, err
			}
			if ctx.Err() != nil || !time.Now().Before(deadline) {
				break
			}
			if err := sendQuery(); err != nil {
				return nil, err
			}
			nextRetry = time.Now().Add(mdnsRetryInterval)
			continue
		}
		packet := buf[:n]
		if !isResponse(packet) {
			ignored++
			continue
		}
		records, err := parseMessage(packet)
		if err != nil {
			debugf(d.Log, "mdns: ignoring %d-byte packet from %s: %v", n, from, err)
			continue
		}
		if !mentionsService(records) {
			ignored++
			continue
		}

		debugf(d.Log, "mdns: %d-byte answer from %s with %d records", n, from, len(records))
		for _, r := range records {
			debugf(d.Log, "mdns:   %s", describeRecord(r))
		}
		dev, ok := deviceFromRecords(records, from.IP)
		if !ok {
			debugf(d.Log, "mdns: answer from %s has no usable id, skipped", from)
			continue
		}
		found[dev.ID] = dev
	}
	debugf(d.Log, "mdns: done, %d device(s), %d unrelated mDNS packet(s) ignored", len(found), ignored)

	if ctx.Err() != nil && len(found) == 0 {
		return nil, ctx.Err()
	}
	devices := make([]Device, 0, len(found))
	for _, dev := range found {
		devices = append(devices, dev)
	}
	sortDevices(devices)
	return devices, nil
}

func isResponse(msg []byte) bool {
	return len(msg) >= dnsHeaderLen && msg[2]&0x80 != 0
}

func mentionsService(records []dnsRecord) bool {
	for _, r := range records {
		if strings.HasSuffix(r.Name, serviceName) || strings.HasSuffix(r.PTR, serviceName) {
			return true
		}
	}
	return false
}

// deviceFromRecords assembles one Device from the records of a single mDNS
// answer. The id comes from the TXT "id" entry (the controller's MAC
// address); without it the instance name is used, so a device with a sparse
// TXT record is still found.
func deviceFromRecords(records []dnsRecord, from net.IP) (Device, bool) {
	var d Device
	for _, r := range records {
		switch r.Type {
		case dnsTypeTXT:
			for _, kv := range r.TXT {
				key, value, ok := strings.Cut(kv, "=")
				if !ok {
					continue
				}
				switch key {
				case "id":
					d.ID = strings.ToLower(value)
				case "md":
					d.Model = value
				case "srcvers":
					d.Firmware = value
				}
			}
			d.Name = instanceName(r.Name)
		case dnsTypeSRV:
			d.Port = int(r.SRV.Port)
			d.Name = instanceName(r.Name)
		case dnsTypeA:
			if d.Host == "" {
				d.Host = r.A.String()
			}
		}
	}
	if d.ID == "" {
		d.ID = strings.ToLower(d.Name)
	}
	if d.ID == "" {
		return Device{}, false
	}
	if d.Host == "" && from != nil {
		d.Host = from.String()
	}
	if d.Port == 0 {
		d.Port = DefaultPort
	}
	return d, true
}

// instanceName turns "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local."
// into "Nanoleaf Light Panels 53:3b:9b".
func instanceName(fullName string) string {
	return strings.TrimSuffix(fullName, "."+serviceName)
}

func interfaceName(ifi *net.Interface) string {
	if ifi == nil {
		return "system default"
	}
	return ifi.Name
}

func describeRecord(r dnsRecord) string {
	switch r.Type {
	case dnsTypeA:
		return "A    " + r.Name + " -> " + r.A.String()
	case dnsTypePTR:
		return "PTR  " + r.Name + " -> " + r.PTR
	case dnsTypeSRV:
		return "SRV  " + r.Name + " -> " + net.JoinHostPort(r.SRV.Target, itoa(int(r.SRV.Port)))
	case dnsTypeTXT:
		return "TXT  " + r.Name + " -> " + strings.Join(r.TXT, " ")
	case dnsTypeAAAA:
		return "AAAA " + r.Name + " (IPv6, not decoded)"
	case dnsTypeNSEC:
		return "NSEC " + r.Name
	default:
		return "type " + itoa(int(r.Type)) + " " + r.Name
	}
}
