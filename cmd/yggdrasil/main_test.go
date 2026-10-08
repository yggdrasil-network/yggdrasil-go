package main

import (
	"os"
	"testing"
)

// notifyReadiness used to close whichever descriptor it was handed and discard
// every error, so -notifyfd 1 quietly closed the process's own stdout and the
// rest of the log output went nowhere.
func TestNotifyReadinessRefusesStdio(t *testing.T) {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if err := notifyReadiness(int(f.Fd())); err == nil {
			t.Errorf("%s should not be usable as the readiness descriptor", f.Name())
		}
	}

	// Both must still be open.
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if _, err := f.Write(nil); err != nil {
			t.Errorf("%s was closed: %v", f.Name(), err)
		}
	}
}

// A descriptor that the service manager didn't actually pass down has to be
// reported, otherwise the supervisor waits for a readiness notification that
// is never coming and nothing explains why.
func TestNotifyReadinessReportsBadDescriptor(t *testing.T) {
	const unopened = 1 << 20
	if err := notifyReadiness(unopened); err == nil {
		t.Fatalf("file descriptor %d is not open and should have been reported", unopened)
	}
}
