package httpx

import (
	"testing"
)

func TestNameSkeletonFoldsHomoglyphs(t *testing.T) {
	for _, in := range []string{"еveryone", "everyone", "EVERYONE", "evеryоне"[:0] + "εveryοne"} {
		if NameSkeleton(in) != "everyone" {
			t.Fatalf("%q -> %q", in, NameSkeleton(in))
		}
	}
	if NameSkeleton("Straße") != "strasse" {
		t.Fatal("sharp s not folded")
	}
}
