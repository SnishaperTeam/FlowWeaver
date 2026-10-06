package subscription

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// TestSalamanderRoundTrip verifies the obfuscation transform described in the
// Hysteria 2 extras: an 8 byte salt followed by the payload XORed with
// blake2b-256(PSK + salt).
func TestSalamanderRoundTrip(t *testing.T) {
	client, server, stop := salamanderPipe(t, "shared-secret")
	defer stop()

	payload := []byte("obfuscated datagram")
	if _, err := client.WriteTo(payload, server.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 2048)
	n, _, err := server.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf[:n], payload) {
		t.Errorf("round trip mismatch: got %q, want %q", buf[:n], payload)
	}
}

func TestSalamanderDisabledWithoutPSK(t *testing.T) {
	inner, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer inner.Close()

	// With no password the wrapper must be a pass-through.
	if got := newSalamanderConn(inner, nil); got != inner {
		t.Error("an empty psk should disable obfuscation entirely")
	}
}

func TestSalamanderIgnoresRunt(t *testing.T) {
	client, server, stop := salamanderPipe(t, "psk")
	defer stop()

	// A datagram shorter than the salt carries no key material and must be
	// skipped. It has to be injected on the raw socket, because the wrapper
	// itself always prepends a full salt.
	inner := client.(*salamanderConn).unwrap()
	if _, err := inner.WriteTo([]byte{0x01, 0x02}, server.LocalAddr()); err != nil {
		t.Fatalf("write runt: %v", err)
	}

	payload := []byte("after-runt")
	if _, err := client.WriteTo(payload, server.LocalAddr()); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, 2048)
	if err := server.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}
	n, _, err := server.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(buf[:n], payload) {
		t.Errorf("got %q, want %q", buf[:n], payload)
	}
}

func TestSalamanderKeyDependsOnSalt(t *testing.T) {
	psk := []byte("k")
	saltA := []byte("12345678")
	saltB := []byte("87654321")

	if salamanderKey(psk, saltA) == salamanderKey(psk, saltB) {
		t.Error("the derived key must depend on the salt")
	}
	if salamanderKey(psk, saltA) != salamanderKey(psk, saltA) {
		t.Error("the derivation must be deterministic")
	}
}

// salamanderPipe returns two connected packet conns that both obfuscate with
// the same pre-shared key.
func salamanderPipe(t *testing.T, psk string) (client, server net.PacketConn, stop func()) {
	t.Helper()

	first, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	second, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		first.Close()
		t.Fatalf("listen: %v", err)
	}

	client = newSalamanderConn(first, []byte(psk))
	server = newSalamanderConn(second, []byte(psk))
	return client, server, func() {
		first.Close()
		second.Close()
	}
}

func TestQUICVarintRoundTrip(t *testing.T) {
	values := []uint64{0, 1, 63, 64, 16383, 16384, 1073741823, 1073741824, 1 << 40}
	for _, v := range values {
		encoded := appendQUICVarint(nil, v)
		got, n, err := consumeQUICVarint(encoded)
		if err != nil {
			t.Fatalf("decode %d: %v", v, err)
		}
		if got != v {
			t.Errorf("round trip %d -> %d", v, got)
		}
		if n != len(encoded) {
			t.Errorf("value %d: consumed %d of %d bytes", v, n, len(encoded))
		}
	}
}

func TestQUICVarintTruncated(t *testing.T) {
	if _, _, err := consumeQUICVarint(nil); err == nil {
		t.Error("expected an error for an empty buffer")
	}
	// A two byte varint whose second byte is missing is incomplete.
	if _, _, err := consumeQUICVarint([]byte{0x40}); err == nil {
		t.Error("expected an error for a truncated 2 byte varint")
	}
}

func TestHysteriaAddrEncoding(t *testing.T) {
	// Addresses travel as a "host:port" string prefixed with a varint length,
	// which is what makes them unambiguous on the wire.
	cases := []struct {
		host string
		port int
	}{
		{"example.com", 443},
		{"1.2.3.4", 80},
		{"2001:db8::1", 53},
	}

	for _, c := range cases {
		encoded, err := encodeHysteriaAddr(c.host, c.port)
		if err != nil {
			t.Fatalf("encode %s: %v", c.host, err)
		}

		length, n, err := consumeQUICVarint(encoded)
		if err != nil {
			t.Fatalf("decode length for %s: %v", c.host, err)
		}
		if int(length) != len(encoded)-n {
			t.Errorf("%s: length prefix = %d, want %d", c.host, length, len(encoded)-n)
		}

		host, port, consumed, err := readHysteriaAddr(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", c.host, err)
		}
		if host != c.host {
			t.Errorf("host = %q, want %q", host, c.host)
		}
		if port != c.port {
			t.Errorf("port = %d, want %d", port, c.port)
		}
		if consumed != len(encoded) {
			t.Errorf("consumed = %d, want %d", consumed, len(encoded))
		}
	}

	if _, err := encodeHysteriaAddr("example.com", 0); err == nil {
		t.Error("expected error for invalid port")
	}
	if _, err := encodeHysteriaAddr("", 443); err == nil {
		t.Error("expected error for an empty host")
	}
}

func TestHysteriaRequiresPassword(t *testing.T) {
	node := Node{Name: "hy", Type: "hysteria2", Server: "127.0.0.1", Port: 1, Options: map[string]string{}}
	if _, err := node.Dial("a.test", 443); err == nil {
		t.Error("expected an error when the password is missing")
	}
}

func TestHysteriaRejectsUnknownObfs(t *testing.T) {
	node := Node{
		Name: "hy", Type: "hysteria2", Server: "127.0.0.1", Port: 1,
		Options: map[string]string{
			"password":      "pw",
			"obfs":          "xplus",
			"obfs-password": "obfspw",
		},
	}
	if _, err := node.Dial("a.test", 443); err == nil {
		t.Error("expected an error for an unsupported obfs type")
	}
}

func TestHysteriaSupported(t *testing.T) {
	for _, typ := range []string{"hysteria2", "Hysteria2", "hy2"} {
		if !(Node{Type: typ}).Supported() {
			t.Errorf("%q should be supported", typ)
		}
	}
}

// hysteriaServer runs a QUIC endpoint that answers the /auth request with the
// expected status, following the protocol document rather than the client
// code. Relay frames are exercised separately by TestHysteriaTCPRelayFrames
// because a stock http3 server owns the bidirectional streams on the
// connection and would contend with a relay loop for them.
func hysteriaServer(t *testing.T) (addr string, stop func()) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Hysteria-Auth") != "hunter2" {
			// A real server would behave like an ordinary web server.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Hysteria-UDP", "true")
		w.WriteHeader(hysteriaAuthOK)
	})

	server := &http3.Server{
		Handler:    mux,
		QUICConfig: &quic.Config{EnableDatagrams: true},
	}
	conn, err := quic.ListenAddrEarly("127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert(t)},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"h3"},
	}, server.QUICConfig)
	if err != nil {
		t.Fatalf("quic listen: %v", err)
	}

	go func() {
		_ = server.ServeListener(conn)
	}()

	return conn.Addr().String(), func() { conn.Close() }
}

// serveHysteriaTCP handles relayed streams: it reads the request frame, answers
// with a TCPResponse, then echoes what the client sends.
func serveHysteriaTCP(session *quic.Conn) {
	for {
		stream, err := session.AcceptStream(context.Background())
		if err != nil {
			return
		}
		go func(s *quic.Stream) {
			defer s.Close()

			// Request frame: varint length, varint type, body. The length
			// covers the body only, not the type field.
			length, _, err := consumeQUICVarintFrom(s)
			if err != nil {
				return
			}
			msgType, _, err := consumeQUICVarintFrom(s)
			if err != nil {
				return
			}
			if msgType != hysteriaTCPMsgType {
				return
			}

			body := make([]byte, length)
			if _, err := io.ReadFull(s, body); err != nil {
				return
			}

			// Answer with a success TCPResponse: status, empty message, no
			// padding.
			response := []byte{hysteriaStatusOK, 0x00}
			_ = writeHysteriaFrame(s, hysteriaTCPMsgType, response)

			line, err := readStreamLine(s)
			if err != nil && line == "" {
				return
			}
			s.Write([]byte(toUpper(trimLine(line)) + " "))
		}(stream)
	}
}

// TestHysteria2Authentication checks the HTTP/3 leg in isolation: a session is
// only returned when the server answers /auth with status 233, so both the
// success and the rejection paths are observable.
func TestHysteria2Authentication(t *testing.T) {
	addr, stop := hysteriaServer(t)
	defer stop()

	host, port := SplitHostPort(addr, 0)
	base := Node{
		Name: "hy2-node", Type: "hysteria2",
		Server: host, Port: port,
		Options: map[string]string{
			"skip-cert-verify": "true",
			"servername":       "example.com",
		},
	}

	// Correct password: the session must be established.
	good := base
	good.Options["password"] = "hunter2"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := dialHysteria(ctx, good)
	if err != nil {
		t.Fatalf("authentication with the right password failed: %v", err)
	}
	if !session.udpOK {
		t.Error("the server advertised UDP support but the session did not record it")
	}
	session.close()

	// Wrong password: the server answers 404 and the dial must fail.
	bad := base
	bad.Options = map[string]string{
		"password":         "wrong",
		"skip-cert-verify": "true",
		"servername":       "example.com",
	}
	if _, err := dialHysteria(ctx, bad); err == nil {
		t.Error("expected authentication to fail with the wrong password")
	}
}

// TestHysteriaTCPRequestFrame checks the TCPRequest wire format end to end
// through a pipe: the client writes a request frame, a server reads it back,
// answers with a TCPResponse, and the client accepts it.
//
// The frame layer is exercised directly rather than over a live QUIC session
// because a stock http3 server already owns the bidirectional streams on that
// connection and would contend with a relay loop for them.
func TestHysteriaTCPRequestFrame(t *testing.T) {
	clientSide, serverSide := pipePair(t)

	done := make(chan error, 1)
	go func() {
		// Server: read the request frame.
		length, _, err := consumeQUICVarintFrom(serverSide)
		if err != nil {
			done <- err
			return
		}
		msgType, _, err := consumeQUICVarintFrom(serverSide)
		if err != nil {
			done <- err
			return
		}
		if msgType != hysteriaTCPMsgType {
			done <- fmt.Errorf("msg type = 0x%x, want 0x%x", msgType, hysteriaTCPMsgType)
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(serverSide, body); err != nil {
			done <- err
			return
		}
		if len(body) == 0 {
			done <- fmt.Errorf("empty request body")
			return
		}
		// Answer with a success response: status plus an empty message.
		if err := writeHysteriaFrame(serverSide, hysteriaTCPMsgType, []byte{hysteriaStatusOK, 0x00}); err != nil {
			done <- err
			return
		}
		done <- nil
	}()

	payload, err := buildHysteriaTCPPayload("target.test", 443)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	if err := writeHysteriaFrame(clientSide, hysteriaTCPMsgType, payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	if err := readHysteriaResponse(clientSide); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("server: %v", err)
	}
}

func TestHysteriaResponseRejection(t *testing.T) {
	clientSide, serverSide := pipePair(t)

	go func() {
		// An error response: body is status 0x01, the message length and the
		// message text, wrapped in the usual frame header.
		body := append([]byte{hysteriaStatusError, 0x06}, []byte("denied")...)
		_ = writeHysteriaFrame(serverSide, hysteriaTCPMsgType, body)
	}()

	err := readHysteriaResponse(clientSide)
	if err == nil {
		t.Fatal("expected an error when the server rejects the relay")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("error should carry the server message, got %v", err)
	}
}

// pipePair returns two connected TCP conns. A buffered stream is required
// because the frame tests write a request and then read a response on the same
// side, which would deadlock on a synchronous net.Pipe.
func pipePair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type accepted struct {
		conn net.Conn
		err  error
	}
	ch := make(chan accepted, 1)
	go func() {
		c, err := ln.Accept()
		ch <- accepted{conn: c, err: err}
	}()

	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	result := <-ch
	if result.err != nil {
		client.Close()
		t.Fatalf("accept: %v", result.err)
	}

	t.Cleanup(func() {
		client.Close()
		result.conn.Close()
	})
	return client, result.conn
}

// TestHysteriaUDPFrameRoundTrip checks the UDPRequest body layout by decoding a
// frame the client produced and comparing it with the original payload.
func TestHysteriaUDPFrameRoundTrip(t *testing.T) {
	addr, err := encodeHysteriaAddr("target.test", 443)
	if err != nil {
		t.Fatalf("encode address: %v", err)
	}
	payload := []byte("udp-datagram")

	body := buildHysteriaUDPFragment(42, 0, 1, addr, payload)

	// Wrap it the way it travels on the wire.
	frame := appendQUICVarint(nil, uint64(len(body)))
	frame = appendQUICVarint(frame, hysteriaUDPMsgType)
	frame = append(frame, body...)

	got, gotAddr, ok, err := parseHysteriaUDPFrame(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ok {
		t.Fatal("a well-formed UDPRequest was rejected")
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("payload = %q, want %q", got, payload)
	}
	if len(gotAddr) == 0 {
		t.Error("the address should be reported for a first fragment")
	}

	// The decoded address must name the original destination.
	decoded := hysteriaSocksaddr(gotAddr)
	if decoded.Fqdn != "target.test" || decoded.Port != 443 {
		t.Errorf("address = %s, want target.test:443", decoded)
	}
}

func TestHysteriaUDPFrameIgnoresOtherTypes(t *testing.T) {
	// A heartbeat or any other message type must be skipped, not mis-parsed.
	frame := appendQUICVarint(nil, 0)
	frame = appendQUICVarint(frame, hysteriaTCPMsgType)

	if _, _, ok, err := parseHysteriaUDPFrame(frame); ok || err != nil {
		t.Errorf("a non-UDP message must be ignored, got ok=%v err=%v", ok, err)
	}
}

func TestHysteriaUDPFragmentCarriesAddressOnlyFirst(t *testing.T) {
	addr, err := encodeHysteriaAddr("target.test", 443)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	first := buildHysteriaUDPFragment(7, 0, 2, addr, []byte("chunk-a"))
	// Continuation fragments use the None marker and carry no address.
	second := buildHysteriaUDPFragment(7, 1, 2, nil, []byte("chunk-b"))

	if !bytes.Contains(first, addr) {
		t.Error("the first fragment must carry the address")
	}
	if bytes.Contains(second, addr) {
		t.Error("continuation fragments must not repeat the address")
	}
}

func TestHysteriaUDPRejectsTruncatedFrame(t *testing.T) {
	if _, _, _, err := parseHysteriaUDPFrame(nil); err == nil {
		t.Error("expected an error for an empty frame")
	}

	// A length that exceeds the buffer must be reported rather than accepted.
	frame := appendQUICVarint(nil, 64)
	frame = appendQUICVarint(frame, hysteriaUDPMsgType)
	frame = append(frame, 0, 0, 0, 0, 0, 1)

	if _, _, _, err := parseHysteriaUDPFrame(frame); err == nil {
		t.Error("expected an error for a truncated datagram")
	}
}
