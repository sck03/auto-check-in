//go:build !windows && !linux

package systemtime

// Other platforms retain normal task timers and startup recovery.
func Watch(func()) (func(), error) { return func() {}, nil }
