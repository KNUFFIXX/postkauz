//go:build !unix

package extract

func processAlive(int) bool { return false }
