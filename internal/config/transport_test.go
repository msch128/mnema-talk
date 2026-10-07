package config

import "testing"

func TestUDPMuxPortConfiguration(t *testing.T) {
	for _, port := range []string{"0", "50000", "50050"} {
		env := base()
		env["WEBRTC_UDP_MUX_PORT"] = port
		if _, err := FromEnv(lookup(env)); err != nil {
			t.Errorf("valid mux port %s: %v", port, err)
		}
	}
	for _, port := range []string{"49999", "50051", "65536", "-1", "invalid"} {
		env := base()
		env["WEBRTC_UDP_MUX_PORT"] = port
		if _, err := FromEnv(lookup(env)); err == nil {
			t.Errorf("invalid mux port %s accepted", port)
		}
	}
	cfg, err := FromEnv(lookup(base()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebRTCUDPMuxPort != 0 {
		t.Fatalf("mux must default off, got %d", cfg.WebRTCUDPMuxPort)
	}
}
