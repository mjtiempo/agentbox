//go:build !linux && !darwin

package tty

func isTerminal(fd uintptr) bool { return false }
