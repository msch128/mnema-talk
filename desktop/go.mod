// Module boundary: prevent Go server ./... checks from visiting npm's
// third-party Go fixtures inside the independently built desktop client.
module github.com/msch128/mnema-talk/desktop

go 1.26.0
