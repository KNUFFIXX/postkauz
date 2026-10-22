//go:build !unix

package hook

func processAlive(int) bool { return false }
