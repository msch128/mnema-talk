package config

import "testing"

func TestWebhookAllowPrivateTargets(t *testing.T) {
	cfg, err := FromEnv(lookup(base()))
	if err != nil || len(cfg.WebhookAllowPrivateTargets) != 0 {
		t.Fatalf("default must allow nothing: %v %v", cfg.WebhookAllowPrivateTargets, err)
	}
	env := base()
	env["WEBHOOK_ALLOW_PRIVATE_TARGETS"] = "192.168.1.20, 10.0.0.0/24"
	cfg, err = FromEnv(lookup(env))
	if err != nil || len(cfg.WebhookAllowPrivateTargets) != 2 {
		t.Fatalf("%v %v", cfg.WebhookAllowPrivateTargets, err)
	}
	env["WEBHOOK_ALLOW_PRIVATE_TARGETS"] = "internal.example"
	if _, err := FromEnv(lookup(env)); err == nil {
		t.Fatal("a host name is not an address or range")
	}
}
