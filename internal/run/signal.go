package run

import (
	"os"
	"syscall"
	"time"
)

// Signal policy (FR-1):
//   - with a TTY, Ctrl-C is forwarded by the engine to the agent's terminal;
//     agentbox only sees SIGTERM/SIGHUP
//   - without a TTY, agentbox forwards SIGINT/SIGTERM/SIGHUP to the engine;
//     a second SIGINT (or SIGTERM/SIGHUP after grace) force-removes the
//     container and exits 128+signal
func watchSignals(
	ch <-chan os.Signal,
	proc func() *os.Process,
	forceRemove func(),
	grace time.Duration,
	done <-chan struct{},
	exit func(int),
) {
	counts := map[os.Signal]int{}
	for {
		select {
		case <-done:
			return
		case sig := <-ch:
			counts[sig]++
			if p := proc(); p != nil {
				_ = p.Signal(sig)
			}
			if sig == syscall.SIGINT {
				if counts[sig] >= 2 {
					forceRemove()
					exit(130)
				}
				continue
			}
			// SIGTERM/SIGHUP: force-remove after the grace period.
			go func(s os.Signal) {
				select {
				case <-time.After(grace):
					forceRemove()
					exit(128 + int(s.(syscall.Signal)))
				case <-done:
				}
			}(sig)
		}
	}
}
