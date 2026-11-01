package ws

import "github.com/msch128/mnema-talk/internal/sfu"

// ServerInfo is the payload of the server_info event every connection gets
// first. Browsers compare Version with the version their page was built as
// and offer a reload when they differ. ScreenLayers is how many layers of a
// screen share the SFU takes (absent without media, or before layers
// existed): a browser sends the lower layers only on lines the server offers
// for them.
type ServerInfo struct {
	Version      string `json:"version"`
	ScreenLayers int    `json:"screen_layers,omitempty"`
	// CameraLayers is the same for a camera (absent on a server without
	// camera layers): a browser declares and sends camera layers only then.
	CameraLayers int `json:"camera_layers,omitempty"`
}

// serverInfo reports "dev" for a hub without a version (tests, go run).
func (h *Hub) serverInfo() ServerInfo {
	info := ServerInfo{Version: h.Version}
	if info.Version == "" {
		info.Version = "dev"
	}
	if h.SFU != nil {
		info.ScreenLayers = sfu.MaxLayers
		info.CameraLayers = sfu.MaxLayers
	}
	return info
}
