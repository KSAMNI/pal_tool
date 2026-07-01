package app

import "testing"

func TestCurrentSystemSummaryReturnsLocalResourceShape(t *testing.T) {
	panel, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer panel.Close()

	summary := panel.currentSystemSummary(settingsPayload{})
	if summary.CollectedAt == "" {
		t.Fatalf("CollectedAt is empty")
	}
	if summary.CPU.Cores <= 0 {
		t.Fatalf("CPU cores = %d", summary.CPU.Cores)
	}
	if summary.Disk.Path == "" {
		t.Fatalf("disk path is empty")
	}
	if summary.Disk.TotalBytes == 0 && len(summary.Errors) == 0 {
		t.Fatalf("disk total is zero without an error: %#v", summary)
	}
}
