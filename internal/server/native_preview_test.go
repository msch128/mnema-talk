package server

import "testing"

func TestNativePreviewOptionsAreServerOwnedAndBounded(t *testing.T) {
	valid := NativePreviewOptions{CommunityID: "safe-community_1", InstanceOrigin: "https://community.example.invalid", Compatibility: NativeSupported}
	if validateNativePreviewOptions(valid) != nil {
		t.Fatal("valid preview options denied")
	}
	cases := []NativePreviewOptions{
		{CommunityID: "", InstanceOrigin: valid.InstanceOrigin, Compatibility: NativeSupported},
		{CommunityID: "bad community", InstanceOrigin: valid.InstanceOrigin, Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: "http://community.example.invalid", Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: "https://user@community.example.invalid", Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: "https://community.example.invalid/", Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: "https://community.example.invalid?", Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: "https://community.example.invalid#fragment", Compatibility: NativeSupported},
		{CommunityID: valid.CommunityID, InstanceOrigin: valid.InstanceOrigin, Compatibility: "unknown"},
	}
	for i, c := range cases {
		if validateNativePreviewOptions(c) == nil {
			t.Fatalf("invalid option case %d admitted", i)
		}
	}
	if _, err := NewNativePreviewRouter(Deps{}, valid); err == nil {
		t.Fatal("missing actual dependencies admitted")
	}
	if _, ok := NativePreviewInstanceFrom(t.Context()); ok {
		t.Fatal("absent native context supplied authority")
	}
}
func TestNativePreviewRegistryIsClosedAndCopied(t *testing.T) {
	routes := NativeMetadataRoutes()
	if len(routes) != 27 {
		t.Fatal("metadata registry unexpectedly changed")
	}
	keys := map[string]bool{}
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if keys[key] || route.Method == "" || route.Path == "" {
			t.Fatal("invalid metadata registry")
		}
		keys[key] = true
	}
	routes[0].Path = "/arbitrary"
	if NativeMetadataRoutes()[0].Path == "/arbitrary" {
		t.Fatal("caller mutated registry")
	}
}
