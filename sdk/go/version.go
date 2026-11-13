package mnema

import (
	"runtime/debug"
	"sync"
)

const modulePath = "github.com/msch128/mnema-talk/sdk/go"

// Version returns the SDK's module version as the Go toolchain recorded it
// (a tag such as v0.1.0 or a pseudo-version), or "devel" when it is unknown,
// for example in a build from a work tree.
var Version = sync.OnceValue(func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "devel"
	}
	return versionFrom(info)
})

func versionFrom(info *debug.BuildInfo) string {
	mod := &info.Main
	if mod.Path != modulePath {
		mod = nil
		for _, d := range info.Deps {
			if d.Path == modulePath {
				mod = d
				break
			}
		}
	}
	if mod == nil {
		return "devel"
	}
	if mod.Replace != nil && mod.Replace.Version != "" {
		return mod.Replace.Version
	}
	if mod.Version == "" || mod.Version == "(devel)" {
		return "devel"
	}
	return mod.Version
}

// userAgent identifies the SDK to the server; it carries no personal data.
func userAgent() string { return "mnema-talk-sdk-go/" + Version() }
