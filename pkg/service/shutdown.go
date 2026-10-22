package service

import "os"

// WaitForShutdown blocks until a signal arrives on sc, then runs shutdown and
// returns only once shutdown has finished. Callers return from main after this,
// so the process cannot exit while shutdown is still draining or flushing.
func WaitForShutdown(sc <-chan os.Signal, shutdown func(sig os.Signal)) {
	sig := <-sc
	shutdown(sig)
}
