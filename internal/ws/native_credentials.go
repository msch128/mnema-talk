package ws

import (
	"net"

	"github.com/google/uuid"
)

// DisconnectNativeCredentials seals every connection for a credential change,
// including browser sockets and native pending admission. Native packet gates
// close together under the Hub lock. All underlying transports close before
// asynchronous voice cleanup: a blocked voiceMu or TLS write cannot delay the
// new password response or leave another device's native media authorized.
// Presence cleanup may finish later; this method does not create MLS removal.
func (h *Hub) DisconnectNativeCredentials(id uuid.UUID) {
	h.mu.Lock()
	selected := make([]*Client, 0)
	for c := range h.clients {
		if c.User.ID == id {
			sealForCredentialChange(c)
			selected = append(selected, c)
		}
	}
	for c := range h.pending {
		if c.User.ID == id {
			delete(h.pending, c)
			sealForCredentialChange(c)
			selected = append(selected, c)
		}
	}
	h.mu.Unlock()
	for _, c := range selected {
		closeTransport(c)
		c.close()
	}
	go func() {
		for _, c := range selected {
			h.leaveCurrentVoice(c)
		}
	}()
}

// sealForCredentialChange stops c's native packet gate and its sends. Callers
// hold h.mu.
func sealForCredentialChange(c *Client) {
	if c.native != nil {
		c.native.securityClosed.Store(true)
	}
	c.shutdown()
}

// closeTransport closes the raw network connection beneath c's websocket (and
// TLS), so a blocked write cannot keep the socket open.
func closeTransport(c *Client) {
	if c.conn == nil {
		return
	}
	transport := c.conn.UnderlyingConn()
	if tlsTransport, ok := transport.(interface{ NetConn() net.Conn }); ok {
		transport = tlsTransport.NetConn()
	}
	_ = transport.Close()
}
