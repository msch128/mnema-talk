# Sourced by e2e/run.sh. Needs free_port (a kernel-chosen free TCP port) in scope.

# port_in_range PORT MIN MAX: true when MIN <= PORT <= MAX.
port_in_range() { [ "$1" -ge "$2" ] && [ "$1" -le "$3" ]; }

# pick_turn_port UDP_MIN UDP_MAX prints a port for the built-in TURN relay: free
# for TCP and UDP and outside the WebRTC UDP range, which the server rejects
# (WEBRTC_BUILTIN_TURN_PORT must lie outside the WebRTC UDP port range). The
# kernel hands out ephemeral ports from the same pool as the range, so a bare
# free port lands inside it now and then.
pick_turn_port() {
  local min="$1" max="$2" candidate
  for _ in $(seq 1 40); do
    candidate="$(free_port tcp)"
    if port_in_range "$candidate" "$min" "$max"; then continue; fi
    if python3 -c "import socket,sys; s=socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind(('127.0.0.1',int(sys.argv[1])))" "$candidate" 2>/dev/null; then
      echo "$candidate"
      return 0
    fi
  done
  return 1
}
