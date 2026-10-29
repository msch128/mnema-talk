#!/usr/bin/env bash
# Sourced by the docker drills: picks host ports that nothing else can hold.
#
# The Linux ephemeral range (32768-60999) is where the kernel hands out source
# ports for outbound connections, so a fixed port in it can be taken by any
# connection the runner happens to have open. These helpers pick a random port
# below that range (20000-32000) and check that it is free, so neither a fixed
# default nor a parallel drill can collide with it. Docker still binds the port
# later; the window in between is tiny and the range large.

FREE_PORT_LOW="${FREE_PORT_LOW:-20000}"
FREE_PORT_HIGH="${FREE_PORT_HIGH:-32000}"

# free_port_range COUNT: prints the first port of COUNT consecutive ports that
# are free for both TCP and UDP on 127.0.0.1 and 0.0.0.0.
free_port_range() {
  python3 - "$1" "$FREE_PORT_LOW" "$FREE_PORT_HIGH" <<'PY'
import random, socket, sys
count, low, high = (int(a) for a in sys.argv[1:4])

def free(port):
    for kind in (socket.SOCK_STREAM, socket.SOCK_DGRAM):
        for host in ("127.0.0.1", "0.0.0.0"):
            s = socket.socket(socket.AF_INET, kind)
            try:
                s.bind((host, port))
            except OSError:
                return False
            finally:
                s.close()
    return True

for _ in range(200):
    first = random.randint(low, high - count)
    if all(free(p) for p in range(first, first + count)):
        print(first)
        sys.exit(0)
sys.exit("no free port range of %d in %d-%d" % (count, low, high))
PY
}

# free_port: one free port.
free_port() { free_port_range 1; }
