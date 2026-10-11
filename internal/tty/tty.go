// Package tty detects whether a file descriptor is a terminal without pulling
// in external dependencies (NFR-2). The ioctl check is stricter than the
// ModeCharDevice heuristic: /dev/null is a char device but not a TTY, and CI
// often runs with stdin redirected.
package tty

import "os"

// IsTerminal reports whether fd is a terminal.
func IsTerminal(fd uintptr) bool { return isTerminal(fd) }

// StdinIsTerminal is the common call site.
func StdinIsTerminal() bool { return isTerminal(os.Stdin.Fd()) }
