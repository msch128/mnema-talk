// Package payload holds the realtime events whose fields are plain values:
// IDs, text, numbers and times. An event that carries a domain type lives in
// the package that owns that type (auth.UserUpdate, core.MessageCreate).
//
// The JSON of every type here is public API (doc/api-stability.md). A type
// that replaced a map keeps the map's alphabetical key order, so its fields
// are declared in that order; never reorder or rename them.
package payload
