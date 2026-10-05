package ws

// ServerInfo is the payload of the server_info event every connection gets
// first. Browsers compare Version with the version their page was built as
// and offer a reload when they differ.
type ServerInfo struct {
	Version string `json:"version"`
}

// serverInfo reports "dev" for a hub without a version (tests, go run).
func (h *Hub) serverInfo() ServerInfo {
	if h.Version == "" {
		return ServerInfo{Version: "dev"}
	}
	return ServerInfo{Version: h.Version}
}
