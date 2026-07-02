//go:build !windows

package app

import (
	"syscall"
	"testing"
	"time"
)

// Regression: the sh wrapper exits on SIGINT immediately while the actual
// server child keeps running (e.g. still saving the world). Stop must not
// report success until the whole process group is gone, or a follow-up
// restart races the old server for the game port.
func TestStopServerProcessWaitsForAbandonedChild(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	script := "#!/bin/sh\n" +
		"sh -c 'trap \"\" INT TERM; i=0; while [ $i -lt 60 ]; do sleep 1; i=$((i+1)); done' &\n" +
		"trap 'exit 0' INT\n" +
		"while :; do sleep 1; done\n"
	cmd := startFakeServerTree(t, panel, script)

	start := time.Now()
	if err := panel.stopServerProcess(2 * time.Second); err != nil {
		t.Fatalf("stopServerProcess() error = %v", err)
	}
	if err := syscall.Kill(-cmd.Process.Pid, 0); err == nil {
		t.Fatal("process group still alive after stop returned; a restart would race the old server for the port")
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("stop took %v, expected prompt tree kill after timeout", elapsed)
	}
	if panel.isServerRunning() {
		t.Fatal("server still marked running after stop")
	}
}

func TestParseProcStat(t *testing.T) {
	cases := []struct {
		name  string
		input string
		state string
		pgrp  int
		ok    bool
	}{
		{"plain", "123 (palserver) S 1 456 456 0", "S", 456, true},
		{"comm with spaces and parens", "123 (Pal Server) (x) Z 1 789 789 0", "Z", 789, true},
		{"truncated", "123 (bad", "", 0, false},
		{"non numeric pgrp", "123 (p) S 1 abc 456", "", 0, false},
	}
	for _, tc := range cases {
		state, pgrp, ok := parseProcStat(tc.input)
		if state != tc.state || pgrp != tc.pgrp || ok != tc.ok {
			t.Fatalf("%s: parseProcStat(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.name, tc.input, state, pgrp, ok, tc.state, tc.pgrp, tc.ok)
		}
	}
}
