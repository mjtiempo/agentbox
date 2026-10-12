package run

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// watchSignals drives real signal handling; these tests feed the channel
// directly and use fake proc/forceRemove/exit so no real signals or processes
// are touched.

func TestWatchSignalsSecondSIGINTForceRemoves(t *testing.T) {
	ch := make(chan os.Signal, 4)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	removed := make(chan struct{}, 1)
	exits := make(chan int, 1)
	go watchSignals(ch,
		func() *os.Process { return nil },
		func() { removed <- struct{}{} },
		time.Second, done,
		func(code int) { exits <- code })

	ch <- syscall.SIGINT
	ch <- syscall.SIGINT
	select {
	case <-removed:
	case <-time.After(2 * time.Second):
		t.Fatal("forceRemove was not called after two SIGINTs")
	}
	select {
	case code := <-exits:
		if code != 130 {
			t.Errorf("exit code = %d, want 130", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exit was not called after two SIGINTs")
	}
}

func TestWatchSignalsSIGTERMGraceForceRemoves(t *testing.T) {
	ch := make(chan os.Signal, 4)
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	removed := make(chan struct{}, 1)
	exits := make(chan int, 1)
	go watchSignals(ch,
		func() *os.Process { return nil },
		func() { removed <- struct{}{} },
		20*time.Millisecond, done,
		func(code int) { exits <- code })

	ch <- syscall.SIGTERM
	select {
	case <-removed:
	case <-time.After(2 * time.Second):
		t.Fatal("forceRemove was not called after the SIGTERM grace period")
	}
	select {
	case code := <-exits:
		if code != 143 {
			t.Errorf("exit code = %d, want 143 (128+SIGTERM)", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exit was not called after the SIGTERM grace period")
	}
}

func TestWatchSignalsDoneEndsGoroutine(t *testing.T) {
	ch := make(chan os.Signal, 1)
	done := make(chan struct{})
	returned := make(chan struct{})
	go func() {
		watchSignals(ch,
			func() *os.Process { return nil },
			func() {}, time.Second, done,
			func(int) {})
		close(returned)
	}()

	close(done)
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("watchSignals did not return after done closed")
	}
}
