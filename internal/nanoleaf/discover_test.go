package nanoleaf

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"os"
	"testing"
	"time"
)

func testLogger(t *testing.T) *log.Logger {
	if testing.Verbose() {
		return log.New(os.Stderr, "    debug: ", 0)
	}
	return nil
}

// useFakeMDNS makes MDNS use a plain localhost socket instead of the
// multicast group, and returns the socket a fake responder should read from.
func useFakeMDNS(t *testing.T) *net.UDPConn {
	t.Helper()
	responder, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { responder.Close() })

	oldDest, oldListen, oldRetry := mdnsDestination, mdnsListen, mdnsRetryInterval
	mdnsDestination = responder.LocalAddr().(*net.UDPAddr)
	mdnsListen = func(*net.Interface) (*net.UDPConn, error) {
		return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	}
	mdnsRetryInterval = 100 * time.Millisecond
	t.Cleanup(func() { mdnsDestination, mdnsListen, mdnsRetryInterval = oldDest, oldListen, oldRetry })
	return responder
}

func TestMDNS(t *testing.T) {
	responder := useFakeMDNS(t)
	go func() {
		buf := make([]byte, 1500)
		n, from, err := responder.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n < dnsHeaderLen || binary.BigEndian.Uint16(buf[4:6]) != 1 {
			return
		}
		responder.WriteToUDP(buf[:n], from) // echo of the question: ignored
		answer := fakeDeviceAnswer(t)
		responder.WriteToUDP(answer, from)
		responder.WriteToUDP(answer, from) // duplicate: de-duplicated
	}()

	devices, err := Discovery{Log: testLogger(t), MDNSTimeout: 500 * time.Millisecond}.MDNS(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 {
		t.Fatalf("got %d devices, want 1: %+v", len(devices), devices)
	}
	d := devices[0]
	if d.ID != "00:55:da:53:3b:9b" || d.Host != "192.168.1.42" || d.Port != 16021 || d.Model != "NL22" {
		t.Errorf("device = %+v", d)
	}
	if d.Addr() != "192.168.1.42:16021" {
		t.Errorf("Addr() = %q", d.Addr())
	}
}

func TestMDNSRetries(t *testing.T) {
	responder := useFakeMDNS(t)
	go func() {
		buf := make([]byte, 1500)
		for i := 1; ; i++ {
			_, from, err := responder.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if i == 2 {
				responder.WriteToUDP(fakeDeviceAnswer(t), from)
			}
		}
	}()
	devices, err := Discovery{MDNSTimeout: 500 * time.Millisecond}.MDNS(context.Background())
	if err != nil || len(devices) != 1 {
		t.Fatalf("got %d devices, %v; want 1 (was the query re-sent?)", len(devices), err)
	}
}

func TestMDNSTimeoutWithNoResponder(t *testing.T) {
	useFakeMDNS(t)
	start := time.Now()
	devices, err := Discovery{MDNSTimeout: 200 * time.Millisecond}.MDNS(context.Background())
	if err != nil || len(devices) != 0 {
		t.Errorf("got %+v, %v; want none", devices, err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("timeout not respected: %s", elapsed)
	}
}
