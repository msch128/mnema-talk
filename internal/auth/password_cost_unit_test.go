//go:build !integration

package auth

// The untagged test binary exercises the production bcrypt cost.
const expectedPasswordCost = 12
