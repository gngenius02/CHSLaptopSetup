package main

import "sync"

var (
	cleanupMu    sync.Mutex
	cleanupFns   []func()
	cleanupRun   bool
)

func registerCleanup(fn func()) {
	if fn == nil {
		return
	}
	cleanupMu.Lock()
	defer cleanupMu.Unlock()
	cleanupFns = append(cleanupFns, fn)
}

func runCleanup() {
	cleanupMu.Lock()
	if cleanupRun {
		cleanupMu.Unlock()
		return
	}
	cleanupRun = true
	fns := make([]func(), 0, len(cleanupFns))
	for i := len(cleanupFns) - 1; i >= 0; i-- {
		fns = append(fns, cleanupFns[i])
	}
	cleanupMu.Unlock()

	for _, fn := range fns {
		fn()
	}
}
