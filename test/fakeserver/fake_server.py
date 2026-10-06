import hashlib
import http.server
import socket
import ssl
import struct
import sys
import tempfile
import threading
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.ciphers.aead import ChaCha20Poly1305
from cryptography.hazmat.primitives.kdf.hkdf import HKDF
import datetime
import os

SUB_USERINFO = "upload=1073741824; download=5368709120; total=107374182400; expire=1893456000"
TROJAN_PASS = "fake-trojan-pass"
SS_PASS = "fake-ss-pass"
UUID = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
UUID_BYTES = bytes.fromhex(UUID.replace("-", ""))


def self_signed_cert(tmpdir):
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(x509.NameOID.COMMON_NAME, "fake.test")])
    now = datetime.datetime.now(datetime.timezone.utc)
    cert = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now)
        .not_valid_after(now + datetime.timedelta(days=365))
        .sign(key, hashes.SHA256())
    )
    cert_path = os.path.join(tmpdir, "fake_server_cert.pem")
    key_path = os.path.join(tmpdir, "fake_server_key.pem")
    with open(cert_path, "wb") as f:
        f.write(cert.public_bytes(serialization.Encoding.PEM))
    with open(key_path, "wb") as f:
        f.write(
            key.private_bytes(
                serialization.Encoding.PEM,
                serialization.PrivateFormat.PKCS8,
                serialization.NoEncryption(),
            )
        )
    return cert_path, key_path


def build_subscription_yaml(ports):
    p = ports
    return (
        "proxies:\n"
        f"  - {{name: fake-trojan, type: trojan, server: 127.0.0.1, port: {p['trojan']}, "
        f"password: {TROJAN_PASS}, sni: fake.test, skip-cert-verify: true, udp: true}}\n"
        f"  - {{name: fake-ss, type: ss, server: 127.0.0.1, port: {p['ss']}, "
        f"cipher: chacha20-ietf-poly1305, password: {SS_PASS}, udp: true}}\n"
        f"  - {{name: fake-vmess, type: vmess, server: 127.0.0.1, port: {p['vmess']}, "
        f"uuid: {UUID}, cipher: auto, udp: true}}\n"
        f"  - {{name: fake-vless, type: vless, server: 127.0.0.1, port: {p['vless']}, "
        f"uuid: {UUID}, network: tcp, udp: true}}\n"
        f"  - {{name: fake-socks5, type: socks5, server: 127.0.0.1, port: {p['socks5']}, udp: true}}\n"
        f"  - {{name: fake-http, type: http, server: 127.0.0.1, port: {p['http']}}}\n"
        "proxy-groups:\n"
        "  - {name: FakeGroup, type: select, proxies: [fake-trojan, fake-ss, fake-vmess, fake-vless, fake-socks5, fake-http]}\n"
        "rules:\n"
        "  - DOMAIN-SUFFIX,example.com,FakeGroup\n"
        "  - MATCH,FakeGroup\n"
    ).encode()


class SubscriptionHTTP:
    def __init__(self, yaml_body):
        self.yaml_body = yaml_body
        outer = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                body = outer.yaml_body
                self.send_response(200)
                self.send_header("Content-Type", "text/yaml")
                self.send_header("Subscription-Userinfo", SUB_USERINFO)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *a):
                pass

        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.port = self.httpd.server_address[1]

    def start(self):
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()


def read_exact(sock, n):
    buf = b""
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("peer closed")
        buf += chunk
    return buf


def read_crlf(sock):
    data = read_exact(sock, 2)
    if data != b"\r\n":
        raise ValueError(f"expected CRLF, got {data!r}")


def read_socks_addr(sock):
    atyp = read_exact(sock, 1)[0]
    if atyp == 0x01:
        host = socket.inet_ntoa(read_exact(sock, 4))
    elif atyp == 0x03:
        n = read_exact(sock, 1)[0]
        host = read_exact(sock, n).decode()
    elif atyp == 0x04:
        host = socket.inet_ntop(socket.AF_INET6, read_exact(sock, 16))
    else:
        raise ValueError(f"bad atyp {atyp}")
    port = struct.unpack(">H", read_exact(sock, 2))[0]
    return host, port


def evp_bytes_to_key(password, key_len):
    out = b""
    prev = b""
    while len(out) < key_len:
        prev = hashlib.md5(prev + password).digest()
        out += prev
    return out[:key_len]


def hkdf_sha1(secret, salt, info, length):
    return HKDF(algorithm=hashes.SHA1(), length=length, salt=salt, info=info).derive(secret)


def ss_nonce12(counter):
    return b"\x00" * 4 + struct.pack("<Q", counter)


def ss_pay_nonce(counter):
    return ss_nonce12(counter + 1 + 0x10000)


class SSStream:
    def __init__(self, password, key_size=32):
        self.master = evp_bytes_to_key(password.encode(), key_size)

    def derive_session(self, salt):
        return hkdf_sha1(self.master, salt, b"ss-subkey", 32)

    @staticmethod
    def chunk_key(session, nonce12):
        return hkdf_sha1(session, nonce12[:8], b"ss-chunk", 32)

    @staticmethod
    def mask(session, nonce12):
        return hkdf_sha1(session, nonce12[:8], b"ss-mask", 2)

    def open_chunk(self, session, counter, reader):
        nonce = ss_nonce12(counter)
        aead = ChaCha20Poly1305(self.chunk_key(session, nonce))
        header = reader(2 + 16)
        plain = aead.decrypt(nonce, header, None)
        mask = self.mask(session, nonce)
        length = (plain[0] ^ mask[0]) << 8 | (plain[1] ^ mask[1])
        pay_nonce = ss_pay_nonce(counter)
        pay_aead = ChaCha20Poly1305(self.chunk_key(session, pay_nonce))
        body = reader(length + 16)
        body_plain = pay_aead.decrypt(pay_nonce, body, None)
        return bytes(b ^ mask[i % 2] for i, b in enumerate(body_plain))

    def seal_chunk(self, session, counter, payload):
        nonce = ss_nonce12(counter)
        aead = ChaCha20Poly1305(self.chunk_key(session, nonce))
        mask = self.mask(session, nonce)
        length = len(payload)
        len_plain = bytes([((length >> 8) & 0xFF) ^ mask[0], (length & 0xFF) ^ mask[1]])
        len_sealed = aead.encrypt(nonce, len_plain, None)
        pay_nonce = ss_pay_nonce(counter)
        pay_aead = ChaCha20Poly1305(self.chunk_key(session, pay_nonce))
        pay_plain = bytes(b ^ mask[i % 2] for i, b in enumerate(payload))
        pay_sealed = pay_aead.encrypt(pay_nonce, pay_plain, None)
        return len_sealed + pay_sealed


def echo_raw(sock):
    sock.settimeout(15)
    while True:
        data = sock.recv(65535)
        if not data:
            return
        sock.sendall(data)


def echo_ss(sock, stream, session, counter):
    sock.settimeout(15)
    buf = b""

    def reader(n):
        nonlocal buf
        while len(buf) < n:
            chunk = sock.recv(n - len(buf))
            if not chunk:
                raise ConnectionError("closed")
            buf += chunk
        out, buf = buf[:n], buf[n:]
        return out

    rx = counter
    while True:
        try:
            payload = stream.open_chunk(session, rx, reader)
        except Exception:
            return
        rx += 1
        out = stream.seal_chunk(session, rx, payload)
        sock.sendall(out)


def trojan_handler(sock):
    key = read_exact(sock, 56).decode()
    expected = hashlib.sha224(TROJAN_PASS.encode()).hexdigest()
    if key != expected:
        raise ValueError("trojan auth mismatch")
    read_crlf(sock)
    cmd = read_exact(sock, 1)[0]
    read_socks_addr(sock)
    read_crlf(sock)
    if cmd == 0x03:
        dest = read_socks_addr(sock)
        length = struct.unpack(">H", read_exact(sock, 2))[0]
        body = read_exact(sock, length)
        payload = body[2:]
        out = struct.pack(">H", len(payload) + 2) + b"\r\n" + payload
        sock.sendall(out)
        return
    echo_raw(sock)


def vmess_handler(sock):
    length = struct.unpack(">H", read_exact(sock, 2))[0]
    auth_id = read_exact(sock, 16)
    sealed = read_exact(sock, length)
    nonce = read_exact(sock, 12)
    key = UUID_BYTES + b"\x00" * 16
    body = ChaCha20Poly1305(key).decrypt(nonce, sealed, None)
    if body[0] != 0x01 or body[1:17] != auth_id:
        raise ValueError("vmess body malformed")
    sec = body[17]
    sni_len = body[18]
    pos = 19 + sni_len
    pad_len = body[pos]
    pos += 1 + pad_len
    if body[pos : pos + 16] != auth_id:
        raise ValueError("vmess auth mismatch")
    sock.sendall(bytes([UUID_BYTES[0], 0, 0, 0]))
    echo_raw(sock)


def read_vless_addr(sock):
    atyp = read_exact(sock, 1)[0]
    if atyp == 0x01:
        read_exact(sock, 4)
    elif atyp == 0x02:
        n = read_exact(sock, 1)[0]
        read_exact(sock, n)
    elif atyp == 0x03:
        read_exact(sock, 16)
    else:
        raise ValueError(f"bad vless atyp {atyp}")


def vless_handler(sock):
    version = read_exact(sock, 1)[0]
    read_exact(sock, 16)
    addons_len = read_exact(sock, 1)[0]
    if addons_len:
        read_exact(sock, addons_len)
    cmd = read_exact(sock, 1)[0]
    read_exact(sock, 2)
    if version != 0x00 or cmd != 0x01:
        raise ValueError("vless header malformed")
    read_vless_addr(sock)
    sock.sendall(b"\x00\x00")
    echo_raw(sock)


def socks5_handler(sock):
    ver = read_exact(sock, 1)[0]
    if ver != 0x05:
        raise ValueError(f"bad socks version {ver}")
    n_methods = read_exact(sock, 1)[0]
    methods = read_exact(sock, n_methods)
    if 0x00 not in methods:
        sock.sendall(b"\x05\xff")
        raise ValueError("no acceptable auth method")
    sock.sendall(b"\x05\x00")
    ver_cmd = read_exact(sock, 3)
    if ver_cmd[0] != 0x05 or ver_cmd[1] != 0x01:
        raise ValueError("bad connect request")
    read_socks_addr(sock)
    sock.sendall(b"\x05\x00\x00\x01" + b"\x00" * 6)
    echo_raw(sock)


def http_connect_handler(sock):
    sock.settimeout(15)
    buf = b""
    while b"\r\n\r\n" not in buf:
        chunk = sock.recv(65535)
        if not chunk:
            return
        buf += chunk
    first_line = buf.split(b"\r\n", 1)[0].decode()
    if not first_line.startswith("CONNECT "):
        raise ValueError("not a CONNECT request")
    sock.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
    echo_raw(sock)


class TCPServerBase:
    def __init__(self, handler, tls_ctx=None):
        self.handler = handler
        self.tls_ctx = tls_ctx
        self.sock = socket.socket()
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(("127.0.0.1", 0))
        self.sock.listen(16)
        self.port = self.sock.getsockname()[1]

    def start(self):
        def loop():
            while True:
                try:
                    conn, _ = self.sock.accept()
                except OSError:
                    return
                threading.Thread(target=self.serve, args=(conn,), daemon=True).start()

        threading.Thread(target=loop, daemon=True).start()

    def serve(self, conn):
        try:
            if self.tls_ctx is not None:
                conn = self.tls_ctx.wrap_socket(conn, server_side=True)
            self.handler(conn)
        except Exception as exc:
            print(f"[fakeserver] handler error: {type(exc).__name__}: {exc}", file=sys.stderr, flush=True)
        finally:
            try:
                conn.close()
            except Exception:
                pass


class SSUDPServer:
    def __init__(self, stream):
        self.stream = stream
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.bind(("127.0.0.1", 0))
        self.port = self.sock.getsockname()[1]

    def start(self):
        def loop():
            while True:
                try:
                    data, addr = self.sock.recvfrom(65535)
                except OSError:
                    return
                try:
                    salt, sealed = data[:32], data[32:]
                    session = self.stream.derive_session(salt)
                    plain = ChaCha20Poly1305(self.stream.chunk_key(session, b"\x00" * 12)).decrypt(
                        b"\x00" * 12, sealed, None
                    )
                    atyp = plain[0]
                    if atyp == 0x03:
                        consumed = 2 + plain[1] + 2
                    elif atyp == 0x01:
                        consumed = 7
                    else:
                        consumed = 19
                    out = ChaCha20Poly1305(self.stream.chunk_key(session, b"\x00" * 12)).encrypt(
                        b"\x00" * 12, plain, None
                    )
                    self.sock.sendto(salt + out, addr)
                except Exception:
                    continue

        threading.Thread(target=loop, daemon=True).start()


class FakeServer:
    def __init__(self, tmpdir):
        self.tmpdir = tmpdir
        cert_path, key_path = self_signed_cert(tmpdir)
        self.tls_ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        self.tls_ctx.load_cert_chain(cert_path, key_path)

        self.stream = SSStream(SS_PASS)

        self.sub = SubscriptionHTTP(None)
        self.trojan = TCPServerBase(trojan_handler, self.tls_ctx)
        self.ss = TCPServerBase(self.ss_tcp_handler)
        self.ss_udp = SSUDPServer(self.stream)
        self.vmess = TCPServerBase(vmess_handler)
        self.vless = TCPServerBase(vless_handler)
        self.socks5 = TCPServerBase(socks5_handler)
        self.http_proxy = TCPServerBase(http_connect_handler)

        self.ports = {
            "sub": self.sub.port,
            "trojan": self.trojan.port,
            "ss": self.ss.port,
            "ss_udp": self.ss_udp.port,
            "vmess": self.vmess.port,
            "vless": self.vless.port,
            "socks5": self.socks5.port,
            "http": self.http_proxy.port,
        }
        self.sub.yaml_body = build_subscription_yaml(self.ports)

    def ss_tcp_handler(self, sock):
        salt = read_exact(sock, 32)
        session = self.stream.derive_session(salt)
        counter = 0
        first = self.stream.open_chunk(session, counter, lambda n: read_exact(sock, n))
        counter += 1
        if len(first) < 7:
            raise ValueError("ss first chunk too short")
        echo_ss(sock, self.stream, session, counter)

    def start(self):
        self.sub.start()
        self.trojan.start()
        self.ss.start()
        self.ss_udp.start()
        self.vmess.start()
        self.vless.start()
        self.socks5.start()
        self.http_proxy.start()

    def stop(self):
        self.sub.httpd.shutdown()
        for s in (self.trojan, self.ss, self.vmess, self.vless, self.socks5, self.http_proxy):
            s.sock.close()


if __name__ == "__main__":
    import json
    import time

    tmpdir = tempfile.mkdtemp(prefix="fakeserver-")
    srv = FakeServer(tmpdir)
    srv.start()
    print(json.dumps(srv.ports), flush=True)
    time.sleep(3600)
