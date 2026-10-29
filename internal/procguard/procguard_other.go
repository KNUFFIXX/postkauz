//go:build !linux

package procguard

const supported = false

func denyInspection() error { return nil }

func inspectable() (bool, error) { return true, nil }
