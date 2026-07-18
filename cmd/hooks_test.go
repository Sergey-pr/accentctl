package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHookShell(t *testing.T) {
	tests := []struct {
		goos      string
		wantShell string
		wantFlag  string
	}{
		{"windows", "cmd", "/c"},
		{"darwin", "sh", "-c"},
		{"linux", "sh", "-c"},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			shell, flag := hookShell(tt.goos)
			if shell != tt.wantShell || flag != tt.wantFlag {
				t.Errorf("hookShell(%q) = (%q, %q), want (%q, %q)",
					tt.goos, shell, flag, tt.wantShell, tt.wantFlag)
			}
		})
	}
}

// TestRunHooksExecutesOnThisPlatform runs a real hook through this platform's
// shell; `echo text> file` parses the same in sh and cmd.exe.
func TestRunHooksExecutesOnThisPlatform(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := runHooks([]string{"echo hook-ran> out.txt"}); err != nil {
		t.Fatalf("runHooks failed on %s: %v", runtime.GOOS, err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatalf("hook did not create its file on %s: %v", runtime.GOOS, err)
	}
	if got := strings.TrimSpace(string(data)); got != "hook-ran" {
		t.Errorf("hook output = %q, want %q", got, "hook-ran")
	}
}

func TestRunHooksRunsEveryHookInOrder(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := runHooks([]string{
		"echo first> a.txt",
		"echo second> b.txt",
	})
	if err != nil {
		t.Fatalf("runHooks failed: %v", err)
	}

	for _, f := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("hook did not create %s: %v", f, err)
		}
	}
}

func TestRunHooksStopsAndReportsOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// `exit 1` is spelled the same in sh and cmd.exe.
	err := runHooks([]string{
		"exit 1",
		"echo should-not-run> after.txt",
	})
	if err == nil {
		t.Fatal("runHooks succeeded on a failing hook, want an error")
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("error = %q, want it to name the failing hook", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "after.txt")); statErr == nil {
		t.Error("a hook after the failing one ran; hooks must stop at the first failure")
	}
}
