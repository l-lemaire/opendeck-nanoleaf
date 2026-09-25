package nanoleaf

// Go tests live next to the code in files ending in _test.go and are run by
// `go test`. Each test is a function named TestXxx taking *testing.T.
// Failures are reported with t.Errorf (continue) or t.Fatalf (stop this test).

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// The helpers below build DNS messages by hand so tests do not depend on the
// code under test to produce their input.

// appendRecord appends one resource record with the given raw RDATA.
func appendRecord(t *testing.T, buf []byte, name string, typ uint16, rdata []byte) []byte {
	t.Helper() // failures are reported at the caller's line, not here
	buf, err := appendName(buf, name)
	if err != nil {
		t.Fatal(err)
	}
	buf = binary.BigEndian.AppendUint16(buf, typ)
	buf = binary.BigEndian.AppendUint16(buf, dnsClassIN|0x8000) // cache-flush bit set, like real mDNS
	buf = binary.BigEndian.AppendUint32(buf, 120)               // TTL
	buf = binary.BigEndian.AppendUint16(buf, uint16(len(rdata)))
	return append(buf, rdata...)
}

// txtRData encodes strings as TXT RDATA.
func txtRData(items ...string) []byte {
	var out []byte
	for _, s := range items {
		out = append(out, byte(len(s)))
		out = append(out, s...)
	}
	return out
}

// header builds a 12-byte header with the given record count in ANCOUNT.
func header(answers int) []byte {
	h := make([]byte, dnsHeaderLen)
	binary.BigEndian.PutUint16(h[2:4], 0x8400) // response, authoritative
	binary.BigEndian.PutUint16(h[6:8], uint16(answers))
	return h
}

// fakeDeviceAnswer builds the kind of answer a real device sends, including
// a compression pointer in the SRV target so that path is exercised.
func fakeDeviceAnswer(t *testing.T) []byte {
	t.Helper()
	msg := header(4)

	// PTR _nanoleafapi._tcp.local. -> Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local.
	ptrTarget, _ := appendName(nil, "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local.")
	msg = appendRecord(t, msg, serviceName, dnsTypePTR, ptrTarget)

	// A Nanoleaf-Light-Panels-53-3b-9b.local. -> 192.168.1.42 ; remember where the name starts
	hostNameOffset := len(msg)
	msg = appendRecord(t, msg, "Nanoleaf-Light-Panels-53-3b-9b.local.", dnsTypeA, []byte{192, 168, 1, 42})

	// SRV instance -> port 16021, target = pointer to the A record's name
	srv := []byte{0, 0, 0, 0, 0x3E, 0x95} // priority 0, weight 0, port 16021
	srv = binary.BigEndian.AppendUint16(srv, 0xC000|uint16(hostNameOffset))
	msg = appendRecord(t, msg, "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local.", dnsTypeSRV, srv)

	// TXT instance -> id, md (model), srcvers (firmware)
	msg = appendRecord(t, msg, "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local.", dnsTypeTXT,
		txtRData("srcvers=3.3.3", "md=NL22", "id=00:55:DA:53:3B:9B"))
	return msg
}

func TestBuildQuery(t *testing.T) {
	got, err := buildQuery("_nanoleafapi._tcp.local.", dnsTypePTR)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, // header, QDCOUNT=1
		12, '_', 'n', 'a', 'n', 'o', 'l', 'e', 'a', 'f', 'a', 'p', 'i',
		4, '_', 't', 'c', 'p',
		5, 'l', 'o', 'c', 'a', 'l',
		0,
		0, 12, // PTR
		0, 1, // IN
	}
	if !bytes.Equal(got, want) {
		t.Errorf("query bytes\n got % x\nwant % x", got, want)
	}
}

func TestParseMessage(t *testing.T) {
	records, err := parseMessage(fakeDeviceAnswer(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("got %d records, want 4", len(records))
	}

	if r := records[0]; r.Type != dnsTypePTR || r.PTR != "Nanoleaf Light Panels 53:3b:9b._nanoleafapi._tcp.local." {
		t.Errorf("PTR record = %+v", r)
	}
	if r := records[1]; r.Type != dnsTypeA || r.A.String() != "192.168.1.42" {
		t.Errorf("A record = %+v", r)
	}
	if r := records[2]; r.Type != dnsTypeSRV || r.SRV.Port != 16021 || r.SRV.Target != "Nanoleaf-Light-Panels-53-3b-9b.local." {
		t.Errorf("SRV record = %+v (compression pointer not followed?)", r)
	}
	if r := records[3]; r.Type != dnsTypeTXT || len(r.TXT) != 3 || r.TXT[2] != "id=00:55:DA:53:3B:9B" {
		t.Errorf("TXT record = %+v", r)
	}
}

func TestParseMessageRejectsGarbage(t *testing.T) {
	// A table-driven test: one loop over cases is the idiomatic Go way to
	// cover many inputs without repeating the assertion code.
	cases := map[string][]byte{
		"empty":           {},
		"short header":    {0, 0, 0},
		"count but no rr": header(1),
		"pointer loop":    append(header(1), 0xC0, 12, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0),
	}
	for name, msg := range cases {
		if _, err := parseMessage(msg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestDeviceFromRecords(t *testing.T) {
	records, err := parseMessage(fakeDeviceAnswer(t))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := deviceFromRecords(records, nil)
	if !ok {
		t.Fatal("no device extracted")
	}
	want := Device{
		ID: "00:55:da:53:3b:9b", Host: "192.168.1.42", Port: 16021,
		Name: "Nanoleaf Light Panels 53:3b:9b", Model: "NL22", Firmware: "3.3.3",
	}
	if d != want {
		t.Errorf("\n got %+v\nwant %+v", d, want)
	}
}
