//go:build !windows

package ui

// Every other terminal we support interprets escapes without being asked.
func enableVT() error { return nil }
