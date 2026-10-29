#!/usr/bin/env python3
"""Checks from the outside that a TURN relay answers over UDP and TCP.

Per transport it sends a STUN Binding request and a TURN Allocate request
without credentials. A STUN response proves the relay is reachable on that
transport: a Binding success, or for the Allocate either a success or the
expected 401 (Unauthorized) carrying REALM and NONCE.

The target comes from the environment (TURN_PROBE_HOST, optional
TURN_PROBE_PORT), never from the command line, and neither the host nor any
resolved address is ever printed. Exceptions are reported by type only.

Usage:
  TURN_PROBE_HOST=... turn-probe.py [--port N] [--attempts N] [--timeout S]
  turn-probe.py --self-test
Standard library only.
"""
import argparse
import os
import socket
import struct
import sys
import threading

MAGIC = 0x2112A442
BINDING_REQUEST, BINDING_SUCCESS = 0x0001, 0x0101
ALLOCATE_REQUEST, ALLOCATE_SUCCESS, ALLOCATE_ERROR = 0x0003, 0x0103, 0x0113
ATTR_ERROR_CODE, ATTR_REALM, ATTR_NONCE, ATTR_REQUESTED_TRANSPORT = 0x0009, 0x0014, 0x0015, 0x0019


def build(msg_type, attrs=()):
    body = b""
    for typ, val in attrs:
        body += struct.pack(">HH", typ, len(val)) + val + b"\0" * (-len(val) % 4)
    tid = os.urandom(12)
    return struct.pack(">HHI", msg_type, len(body), MAGIC) + tid + body, tid


def allocate_request():
    # REQUESTED-TRANSPORT: UDP (17), as a client asking for a UDP relay would.
    return build(ALLOCATE_REQUEST, [(ATTR_REQUESTED_TRANSPORT, bytes([17, 0, 0, 0]))])


def parse(data):
    """Returns (type, tid, attrs dict) or None when data is not a whole STUN message."""
    if len(data) < 20:
        return None
    typ, length, cookie = struct.unpack(">HHI", data[:8])
    if cookie != MAGIC or length % 4 or len(data) < 20 + length:
        return None
    attrs, pos, end = {}, 20, 20 + length
    while pos + 4 <= end:
        atyp, alen = struct.unpack(">HH", data[pos:pos + 4])
        if pos + 4 + alen > end:
            return None
        attrs[atyp] = data[pos + 4:pos + 4 + alen]
        pos += 4 + alen + (-alen % 4)
    return typ, data[8:20], attrs


def error_code(attrs):
    v = attrs.get(ATTR_ERROR_CODE)
    return None if v is None or len(v) < 4 else (v[2] & 7) * 100 + v[3]


def binding_ok(resp, tid):
    return resp is not None and resp[1] == tid and resp[0] == BINDING_SUCCESS


def allocate_ok(resp, tid):
    if resp is None or resp[1] != tid:
        return False
    typ, _, attrs = resp
    if typ == ALLOCATE_SUCCESS:
        return True
    return (typ == ALLOCATE_ERROR and error_code(attrs) == 401
            and ATTR_REALM in attrs and ATTR_NONCE in attrs)


def resolve(host, port, kind):
    infos = socket.getaddrinfo(host, port, type=kind)
    if not infos:
        raise OSError("no address")
    return infos[0]


def mask_addresses(host, port):
    """Masks resolved addresses in the Actions log. The command is not echoed."""
    if os.environ.get("GITHUB_ACTIONS") != "true":
        return
    try:
        for info in socket.getaddrinfo(host, port):
            print("::add-mask::" + info[4][0])
    except OSError:
        pass


def exchange_udp(host, port, packet, timeout):
    fam, kind, proto, _, addr = resolve(host, port, socket.SOCK_DGRAM)
    with socket.socket(fam, kind, proto) as s:
        s.settimeout(timeout)
        s.sendto(packet, addr)
        while True:
            data, _ = s.recvfrom(4096)
            if parse(data) is not None:
                return parse(data)


def read_exactly(s, n):
    buf = b""
    while len(buf) < n:
        chunk = s.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("closed")
        buf += chunk
    return buf


def exchange_tcp(host, port, packet, timeout):
    fam, kind, proto, _, addr = resolve(host, port, socket.SOCK_STREAM)
    with socket.socket(fam, kind, proto) as s:
        s.settimeout(timeout)
        s.connect(addr)
        s.sendall(packet)
        head = read_exactly(s, 20)
        length = struct.unpack(">H", head[2:4])[0]
        return parse(head + read_exactly(s, length))


EXCHANGE = {"udp": exchange_udp, "tcp": exchange_tcp}
CHECKS = (("binding", lambda: build(BINDING_REQUEST), binding_ok),
          ("allocate", allocate_request, allocate_ok))


def run_check(transport, host, port, make, verdict, attempts, timeout):
    for attempt in range(1, attempts + 1):
        packet, tid = make()
        try:
            if verdict(EXCHANGE[transport](host, port, packet, timeout), tid):
                return True, ""
            reason = "unexpected response"
        except Exception as e:  # type only: messages can carry the address
            reason = type(e).__name__
        print(f"  {transport} attempt {attempt}/{attempts}: {reason}", flush=True)
    return False, reason


def probe(host, port, attempts, timeout):
    """Returns {transport: bool}; prints one line per check."""
    results = {}
    for transport in ("udp", "tcp"):
        ok_all = True
        for name, make, verdict in CHECKS:
            ok, _ = run_check(transport, host, port, make, verdict, attempts, timeout)
            print(f"{transport} {name}: {'ok' if ok else 'FAIL'}", flush=True)
            ok_all = ok_all and ok
        results[transport] = ok_all
    return results


def respond(data):
    """The answer a relay gives to the two unauthenticated requests."""
    msg = parse(data)
    if msg is None:
        return None
    typ, tid, _ = msg
    head = lambda t, body: struct.pack(">HHI", t, len(body), MAGIC) + tid + body
    attr = lambda t, v: struct.pack(">HH", t, len(v)) + v + b"\0" * (-len(v) % 4)
    if typ == BINDING_REQUEST:
        xor_addr = struct.pack(">BBHI", 0, 1, 4242 ^ (MAGIC >> 16), 0x7F000001 ^ MAGIC)
        return head(BINDING_SUCCESS, attr(0x0020, xor_addr))
    if typ == ALLOCATE_REQUEST:
        err = attr(ATTR_ERROR_CODE, bytes([0, 0, 4, 1]) + b"Unauthorized")
        return head(ALLOCATE_ERROR, err + attr(ATTR_REALM, b"probe") + attr(ATTR_NONCE, b"n0nce"))
    return None


def start_responder():
    """Local UDP+TCP STUN responder on one loopback port. Returns (port, stop)."""
    for _ in range(50):
        tcp = socket.socket()
        tcp.bind(("127.0.0.1", 0))
        port = tcp.getsockname()[1]
        udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        try:
            udp.bind(("127.0.0.1", port))
            break
        except OSError:
            tcp.close()
            udp.close()
    else:
        raise RuntimeError("no free port")
    tcp.listen(8)
    stop = threading.Event()

    def serve_udp():
        udp.settimeout(0.1)
        while not stop.is_set():
            try:
                data, peer = udp.recvfrom(4096)
            except socket.timeout:
                continue
            out = respond(data)
            if out:
                udp.sendto(out, peer)

    def serve_conn(conn):
        with conn:
            conn.settimeout(2)
            try:
                head = read_exactly(conn, 20)
                data = head + read_exactly(conn, struct.unpack(">H", head[2:4])[0])
                out = respond(data)
                if out:
                    conn.sendall(out)
            except (OSError, ConnectionError):
                pass

    def serve_tcp():
        tcp.settimeout(0.1)
        while not stop.is_set():
            try:
                conn, _ = tcp.accept()
            except socket.timeout:
                continue
            threading.Thread(target=serve_conn, args=(conn,), daemon=True).start()

    threads = [threading.Thread(target=f, daemon=True) for f in (serve_udp, serve_tcp)]
    for t in threads:
        t.start()

    def shut():
        stop.set()
        for t in threads:
            t.join()
        tcp.close()
        udp.close()
    return port, shut


def self_test():
    failures = []

    def check(name, cond):
        print(f"self-test {name}: {'ok' if cond else 'FAIL'}")
        if not cond:
            failures.append(name)

    port, shut = start_responder()
    try:
        res = probe("127.0.0.1", port, 2, 1)
        check("responder answers udp", res["udp"])
        check("responder answers tcp", res["tcp"])
    finally:
        shut()
    # Nothing listens any more: both transports must be reported as failed.
    res = probe("127.0.0.1", port, 2, 0.3)
    check("closed port fails udp", not res["udp"])
    check("closed port fails tcp", not res["tcp"])
    # A response for another transaction must not count.
    packet, tid = build(BINDING_REQUEST)
    other = respond(build(BINDING_REQUEST)[0])
    check("foreign transaction id rejected", not binding_ok(parse(other), tid))
    # A bare 401 without REALM and NONCE is not a relay answer.
    bare = struct.pack(">HHI", ALLOCATE_ERROR, 8, MAGIC) + tid + struct.pack(">HH", ATTR_ERROR_CODE, 4) + bytes([0, 0, 4, 1])
    check("401 without realm and nonce rejected", not allocate_ok(parse(bare), tid))
    return not failures


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--port", type=int, default=int(os.environ.get("TURN_PROBE_PORT") or 3479))
    ap.add_argument("--attempts", type=int, default=4)
    ap.add_argument("--timeout", type=float, default=3.0)
    ap.add_argument("--self-test", action="store_true")
    args = ap.parse_args()
    if args.self_test:
        return 0 if self_test() else 1
    host = os.environ.get("TURN_PROBE_HOST", "").strip()
    if not host:
        print("TURN_PROBE_HOST is not set", file=sys.stderr)
        return 2
    mask_addresses(host, args.port)
    results = probe(host, args.port, args.attempts, args.timeout)
    for transport, ok in results.items():
        print(f"RESULT {transport}: {'PASS' if ok else 'FAIL'}")
    return 0 if all(results.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
