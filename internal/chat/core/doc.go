// Package core is the lowest layer of internal/chat: the shared model (Message,
// Channel, Category, Poll, ForumTag and friends), the channel and message
// lookups every domain needs, and the member list. It has no HTTP handlers and
// imports no other chat package, so every domain package can depend on it
// without a cycle (internal/repo/boundaries_test.go enforces that).
package core
