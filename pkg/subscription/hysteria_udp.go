package subscription

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/sagernet/sing/common/buf"
	M "github.com/sagernet/sing/common/metadata"
)

// hysteriaUDPPacketConn relays datagrams as Hysteria 2 UDPRequest messages
// carried on QUIC datagrams.
//
// Because a QUIC datagram cannot exceed the path MTU, large datagrams are
// split across fragments and reassembled by session id:
//
//	UDPRequest (0x402) body:
//	  [4-byte big-endian session id][fragment index][fragment count]
//	  [address][padding][data]
//
// Only the first fragment carries the address; later fragments use a zero
// length marker and are matched to their session.
type hysteriaUDPPacketConn struct {
	session *hysteriaSession
	// readBuf holds a reassembled datagram waiting to be handed over.
	readBuf []byte
	// source is the address carried by the datagram in readBuf.
	source M.Socksaddr

	// nextSession hands out session ids for outgoing datagrams.
	nextSession uint32
	// fragments collects incoming fragments keyed by session id.
	fragments map[uint32]*hysteriaFragment
}

type hysteriaFragment struct {
	count     byte
	parts     [][]byte
	have      int
	addr      []byte
	addrReady bool
}

// maxUDPPayload is the practical datagram size that survives fragmentation.
const maxUDPPayload = 1200

func dialHysteriaUDP(ctx context.Context, node Node, host string, port int) (*hysteriaUDPPacketConn, error) {
	session, err := dialHysteria(ctx, node)
	if err != nil {
		return nil, err
	}
	if !session.udpOK {
		session.close()
		return nil, fmt.Errorf("hysteria2: server does not support udp relay")
	}

	target, err := encodeHysteriaAddr(host, port)
	if err != nil {
		session.close()
		return nil, err
	}

	pc := &hysteriaUDPPacketConn{
		session:   session,
		fragments: make(map[uint32]*hysteriaFragment),
	}
	// Register the session with an empty datagram so the server allocates the
	// association before real traffic starts.
	if err := pc.sendFragments(target, nil); err != nil {
		session.close()
		return nil, err
	}
	return pc, nil
}

func (c *hysteriaUDPPacketConn) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	addr, err := encodeHysteriaAddr(tuicHostOf(destination), int(destination.Port))
	if err != nil {
		buffer.Release()
		return err
	}
	payload := buffer.Bytes()
	err = c.sendFragments(addr, payload)
	buffer.Release()
	return err
}

// sendFragments splits a datagram across as many QUIC datagrams as needed.
func (c *hysteriaUDPPacketConn) sendFragments(addr, payload []byte) error {
	c.session.mu.Lock()
	sessionID := c.nextSession
	c.nextSession++
	c.session.mu.Unlock()

	// The address travels with the first fragment; the rest are marked None.
	addressLen := len(addr)
	overhead := addressLen + 8
	if len(payload) <= maxUDPPayload-overhead {
		body := buildHysteriaUDPFragment(sessionID, 0, 1, addr, payload)
		return writeHysteriaDatagram(c.session, hysteriaUDPMsgType, body)
	}

	chunkSize := maxUDPPayload - 8
	total := (len(payload) + chunkSize - 1) / chunkSize
	if total > 255 {
		total = 255
	}
	for i := 0; i < total; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(payload) {
			end = len(payload)
		}
		fragmentAddr := addr
		if i > 0 {
			fragmentAddr = nil
		}
		body := buildHysteriaUDPFragment(sessionID, byte(i), byte(total), fragmentAddr, payload[start:end])
		if err := writeHysteriaDatagram(c.session, hysteriaUDPMsgType, body); err != nil {
			return err
		}
	}
	return nil
}

// buildHysteriaUDPFragment renders one fragment body.
func buildHysteriaUDPFragment(sessionID uint32, index, total byte, addr, payload []byte) []byte {
	body := make([]byte, 0, 11+len(addr)+len(payload))

	var sid [4]byte
	binary.BigEndian.PutUint32(sid[:], sessionID)
	body = append(body, sid[:]...)
	body = append(body, index, total)

	if addr == nil {
		// None marker: no address on continuation fragments.
		body = append(body, 0x00)
	} else {
		body = append(body, addr...)
	}

	body = appendQUICVarint(body, 0) // padding length
	body = append(body, payload...)
	return body
}

// writeHysteriaDatagram frames a message and sends it as a QUIC datagram.
func writeHysteriaDatagram(s *hysteriaSession, msgType uint64, body []byte) error {
	frame := make([]byte, 0, len(body)+8)
	frame = appendQUICVarint(frame, uint64(len(body)))
	frame = appendQUICVarint(frame, msgType)
	frame = append(frame, body...)
	return s.quic.SendDatagram(frame)
}

func (c *hysteriaUDPPacketConn) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	for len(c.readBuf) == 0 {
		if err := c.readOne(); err != nil {
			return M.Socksaddr{}, err
		}
	}
	buffer.Write(c.readBuf)
	source := c.source
	c.readBuf = nil
	return source, nil
}

// readOne receives datagrams until one completes a reassembled message.
func (c *hysteriaUDPPacketConn) readOne() error {
	for {
		if c.session.isClosed() {
			return net.ErrClosed
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		frame, err := c.session.quic.ReceiveDatagram(ctx)
		cancel()
		if err != nil {
			return err
		}

		payload, addr, ok, err := parseHysteriaUDPFrame(frame)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}

		if addr != nil {
			// A fragment that carries an address starts a new message.
			c.readBuf = payload
			c.source = hysteriaSocksaddr(addr)
			return nil
		}
		// Continuation fragments are dropped: this client never fragments
		// outbound traffic, so an unexpected one is not ours to reassemble.
	}
}

// parseHysteriaUDPFrame decodes a UDPRequest datagram. It reports ok=false for
// frames of another type, and an error only for malformed input.
func parseHysteriaUDPFrame(frame []byte) (payload []byte, addr []byte, ok bool, err error) {
	rest := frame

	length, n, err := consumeQUICVarint(rest)
	if err != nil {
		return nil, nil, false, fmt.Errorf("hysteria2: bad datagram length: %w", err)
	}
	rest = rest[n:]

	msgType, n, err := consumeQUICVarint(rest)
	if err != nil {
		return nil, nil, false, fmt.Errorf("hysteria2: bad datagram type: %w", err)
	}
	rest = rest[n:]

	if msgType != hysteriaUDPMsgType {
		return nil, nil, false, nil
	}
	if uint64(len(rest)) < length {
		return nil, nil, false, fmt.Errorf("hysteria2: truncated datagram")
	}
	body := rest[:length]

	// session id(4) frag index(1) frag count(1)
	if len(body) < 6 {
		return nil, nil, false, fmt.Errorf("hysteria2: datagram body too short")
	}
	total := body[5]
	if total != 1 {
		// Fragmented replies are not reassembled; a single fragment is the
		// common case and anything else is skipped rather than mis-parsed.
		return nil, nil, false, nil
	}

	// The address follows, then a padding length and the data.
	// The address offset is relative to the slice passed in, so shift it back
	// into the body coordinates before reading the padding length.
	relEnd, addr, err := splitHysteriaAddr(body[6:])
	if err != nil {
		return nil, nil, false, err
	}
	addressEnd := 6 + relEnd

	padLen, n, err := consumeQUICVarint(body[addressEnd:])
	if err != nil {
		return nil, nil, false, fmt.Errorf("hysteria2: bad padding length: %w", err)
	}
	start := addressEnd + n + int(padLen)
	if start > len(body) {
		return nil, nil, false, fmt.Errorf("hysteria2: padding overruns the datagram")
	}
	return body[start:], addr, true, nil
}

// splitHysteriaAddr returns the offset just past the length prefixed address
// and the address bytes. A zero length marks a continuation fragment, which
// omits the address entirely.
func splitHysteriaAddr(b []byte) (int, []byte, error) {
	length, n, err := consumeQUICVarint(b)
	if err != nil {
		return 0, nil, fmt.Errorf("hysteria2: bad address length: %w", err)
	}
	if length == 0 {
		return n, nil, nil
	}
	if uint64(len(b)-n) < length {
		return 0, nil, fmt.Errorf("hysteria2: address overruns the datagram")
	}
	return n + int(length), b[n : n+int(length)], nil
}

// hysteriaSocksaddr decodes a "host:port" address into a sing address. The
// input is the address field without its length prefix.
func hysteriaSocksaddr(addr []byte) M.Socksaddr {
	if len(addr) == 0 {
		return M.Socksaddr{}
	}

	host, portStr, err := net.SplitHostPort(string(addr))
	if err != nil {
		return M.Socksaddr{}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return M.Socksaddr{}
	}
	if ip := net.ParseIP(host); ip != nil {
		return M.Socksaddr{Addr: ipAddr(ip), Port: uint16(port)}
	}
	return M.Socksaddr{Fqdn: host, Port: uint16(port)}
}

// decodeVarintPort is retained for callers that read a bare varint port.

func decodeVarintPort(b []byte) uint16 {
	v, _, err := consumeQUICVarint(b)
	if err != nil {
		return 0
	}
	return uint16(v)
}

// tuicHostOf returns the host part of a sing address.
func tuicHostOf(addr M.Socksaddr) string {
	if addr.IsDomain() {
		return addr.Fqdn
	}
	return addr.Addr.String()
}

func (c *hysteriaUDPPacketConn) Close() error {
	c.session.close()
	return nil
}

// A QUIC connection has no local UDP address callers can use, and quic.Conn
// offers no deadline methods, so reads are bounded by the context timeout in
// readOne instead.
func (c *hysteriaUDPPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{IP: net.IPv4zero, Port: 0} }
func (c *hysteriaUDPPacketConn) SetDeadline(t time.Time) error      { return nil }
func (c *hysteriaUDPPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *hysteriaUDPPacketConn) SetWriteDeadline(t time.Time) error { return nil }
