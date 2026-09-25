package nanoleaf

// This file is a minimal DNS message codec: just enough of RFC 1035 (DNS)
// and RFC 2782 (SRV records) to send an mDNS question and read the answers a
// Hue bridge sends back. Go's standard library has no DNS message parser, and
// pulling in a full library to read four record types would hide what is
// actually on the wire.
//
// Wire layout of a DNS message (all integers big-endian):
//
//	Header      12 bytes: ID, Flags, QDCOUNT, ANCOUNT, NSCOUNT, ARCOUNT
//	Questions   QDCOUNT x (NAME, TYPE, CLASS)
//	Records     (ANCOUNT+NSCOUNT+ARCOUNT) x (NAME, TYPE, CLASS, TTL, RDLENGTH, RDATA)
//
// A NAME is a sequence of labels, each a length byte followed by that many
// bytes, ending with a zero byte: "\x04_hue\x04_tcp\x05local\x00". To save
// space, a name may end with a 2-byte pointer to an earlier name in the same
// message ("compression"). readName handles both.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Record types and class we care about. `uint16` is an unsigned 16-bit int;
// naming the type in a const block is optional but documents the wire size.
const (
	dnsTypeA   uint16 = 1  // IPv4 address
	dnsTypePTR uint16 = 12 // pointer: service type -> service instance name
	dnsTypeTXT uint16 = 16 // text key=value pairs (bridgeid, modelid)
	dnsTypeSRV uint16 = 33 // service location: target host + port
	dnsClassIN uint16 = 1  // "Internet" class, the only one in use

	// Seen in bridge answers but not decoded; named only for debug output.
	dnsTypeAAAA uint16 = 28 // IPv6 address
	dnsTypeNSEC uint16 = 47 // "no other records exist" marker used by mDNS
)

const dnsHeaderLen = 12

var errDNSTruncated = errors.New("dns: message truncated")

// dnsRecord is one decoded resource record. Only the field matching Type is
// filled in; the others keep their zero value (nil slice, empty string, ...).
// Go has no unions, and a flat struct is easier to read than an interface
// hierarchy for four cases.
type dnsRecord struct {
	Name string
	Type uint16

	A   net.IP   // when Type == dnsTypeA
	PTR string   // when Type == dnsTypePTR: the name pointed to
	SRV dnsSRV   // when Type == dnsTypeSRV
	TXT []string // when Type == dnsTypeTXT: one entry per string, e.g. "bridgeid=abc"
}

type dnsSRV struct {
	Target string // host name providing the service, e.g. "ecb5fafffe1a2b3c.local."
	Port   uint16
}

// buildQuery encodes a standard query for one name and type.
func buildQuery(name string, qtype uint16) ([]byte, error) {
	// make(type, length, capacity): 12 zero bytes now, room to grow without
	// reallocating. The header is all zeros except QDCOUNT = 1. ID 0 and
	// flags 0 are what mDNS expects for a plain query.
	msg := make([]byte, dnsHeaderLen, dnsHeaderLen+len(name)+6)
	binary.BigEndian.PutUint16(msg[4:6], 1) // QDCOUNT

	msg, err := appendName(msg, name)
	if err != nil {
		return nil, err
	}
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, dnsClassIN)
	return msg, nil
}

// appendName encodes "a.b.c." as length-prefixed labels plus a terminating
// zero and appends it to buf. Go slices grow on append and the result must be
// used (append may return a new backing array), hence `buf = append(...)`.
func appendName(buf []byte, name string) ([]byte, error) {
	name = strings.TrimSuffix(name, ".")
	if name != "" {
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 {
				return nil, fmt.Errorf("dns: invalid label %q in name %q", label, name)
			}
			buf = append(buf, byte(len(label)))
			buf = append(buf, label...) // `s...` spreads a string's bytes as arguments
		}
	}
	return append(buf, 0), nil
}

// parseMessage decodes every resource record in a DNS message. Questions are
// skipped. Answer, authority and additional sections are merged into one list
// because mDNS responders spread the useful records across all three.
func parseMessage(msg []byte) ([]dnsRecord, error) {
	if len(msg) < dnsHeaderLen {
		return nil, errDNSTruncated
	}
	questions := int(binary.BigEndian.Uint16(msg[4:6]))
	answers := int(binary.BigEndian.Uint16(msg[6:8]))
	authority := int(binary.BigEndian.Uint16(msg[8:10]))
	additional := int(binary.BigEndian.Uint16(msg[10:12]))

	off := dnsHeaderLen
	for i := 0; i < questions; i++ {
		// A question is NAME + TYPE(2) + CLASS(2). We only need to step over it.
		_, next, err := readName(msg, off)
		if err != nil {
			return nil, err
		}
		off = next + 4
		if off > len(msg) {
			return nil, errDNSTruncated
		}
	}

	total := answers + authority + additional
	records := make([]dnsRecord, 0, total)
	for i := 0; i < total; i++ {
		rec, next, err := readRecord(msg, off)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
		off = next
	}
	return records, nil
}

// readRecord decodes one resource record starting at off and returns it with
// the offset of the next record. Multiple return values are ordinary in Go;
// (value, next, err) is the usual shape for parsers.
func readRecord(msg []byte, off int) (dnsRecord, int, error) {
	name, off, err := readName(msg, off)
	if err != nil {
		return dnsRecord{}, 0, err
	}
	// Fixed part after the name: TYPE(2) CLASS(2) TTL(4) RDLENGTH(2) = 10 bytes.
	if off+10 > len(msg) {
		return dnsRecord{}, 0, errDNSTruncated
	}
	typ := binary.BigEndian.Uint16(msg[off:])
	// CLASS at off+2 is ignored: mDNS sets its top bit as a "cache flush" flag,
	// so comparing it to dnsClassIN would wrongly reject valid records.
	// TTL at off+4 is ignored: we do not cache.
	rdlen := int(binary.BigEndian.Uint16(msg[off+8:]))
	off += 10
	if off+rdlen > len(msg) {
		return dnsRecord{}, 0, errDNSTruncated
	}
	rdata := msg[off : off+rdlen]

	rec := dnsRecord{Name: name, Type: typ}
	switch typ {
	case dnsTypeA:
		if rdlen != 4 {
			return dnsRecord{}, 0, fmt.Errorf("dns: A record with %d bytes", rdlen)
		}
		rec.A = net.IPv4(rdata[0], rdata[1], rdata[2], rdata[3])
	case dnsTypePTR:
		// The pointed-to name may use compression, so decode it against the
		// whole message (msg, off), not against rdata alone.
		rec.PTR, _, err = readName(msg, off)
	case dnsTypeSRV:
		// SRV RDATA: PRIORITY(2) WEIGHT(2) PORT(2) TARGET(name)
		if rdlen < 7 {
			return dnsRecord{}, 0, fmt.Errorf("dns: SRV record with %d bytes", rdlen)
		}
		rec.SRV.Port = binary.BigEndian.Uint16(rdata[4:6])
		rec.SRV.Target, _, err = readName(msg, off+6)
	case dnsTypeTXT:
		rec.TXT, err = parseTXT(rdata)
	default:
		// Other types (AAAA, NSEC, ...) are kept with Name and Type only so
		// the debug output can still show they were there.
	}
	if err != nil {
		return dnsRecord{}, 0, err
	}
	return rec, off + rdlen, nil
}

// parseTXT splits TXT RDATA, a sequence of <length><bytes> strings.
func parseTXT(rdata []byte) ([]string, error) {
	var out []string
	for i := 0; i < len(rdata); {
		n := int(rdata[i])
		i++
		if i+n > len(rdata) {
			return nil, errDNSTruncated
		}
		out = append(out, string(rdata[i:i+n]))
		i += n
	}
	return out, nil
}

// readName decodes a possibly compressed name at off. It returns the name in
// dotted form with a trailing dot ("_hue._tcp.local.") and the offset just
// after the name *in the original stream*, which is what the caller needs to
// keep parsing. Following a pointer does not move that offset.
func readName(msg []byte, off int) (string, int, error) {
	var labels []string
	next := -1 // offset after the name in the stream; -1 = not determined yet
	jumps := 0 // guard against pointer loops in malicious or broken packets

	for {
		if off >= len(msg) {
			return "", 0, errDNSTruncated
		}
		l := int(msg[off])
		switch {
		case l == 0:
			// End of name.
			if next < 0 {
				next = off + 1
			}
			return strings.Join(labels, ".") + ".", next, nil

		case l&0xC0 == 0xC0:
			// Top two bits set: this is a 14-bit pointer to another offset.
			if off+1 >= len(msg) {
				return "", 0, errDNSTruncated
			}
			if next < 0 {
				next = off + 2
			}
			jumps++
			if jumps > 16 {
				return "", 0, errors.New("dns: too many compression pointers")
			}
			off = int(binary.BigEndian.Uint16(msg[off:]) & 0x3FFF)

		case l&0xC0 != 0:
			// 01 and 10 prefixes are reserved / extended label types. Never
			// seen in mDNS.
			return "", 0, fmt.Errorf("dns: unsupported label type 0x%02x", l)

		default:
			off++
			if off+l > len(msg) {
				return "", 0, errDNSTruncated
			}
			labels = append(labels, string(msg[off:off+l]))
			off += l
		}
	}
}
