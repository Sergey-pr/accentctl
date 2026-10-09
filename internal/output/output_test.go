package output

import (
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	stdout := os.Stdout
	os.Stdout = w
	func() {
		defer func() { os.Stdout = stdout }()
		fn()
	}()
	_ = w.Close()
	return <-done
}

func setTerminal(t *testing.T, on bool) {
	t.Helper()
	was := isTerminal
	isTerminal = on
	t.Cleanup(func() { isTerminal = was })
}

func TestChunkProgressPrintsPlainLinesOffTerminal(t *testing.T) {
	setTerminal(t, false)
	out := captureStdout(t, func() {
		for i := 1; i <= 3; i++ {
			ChunkProgress("app.json", i, 3)
		}
	})

	want := "  1/3  app.json\n  2/3  app.json\n  3/3  app.json\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

func TestChunkProgressRedrawsBarOnTerminal(t *testing.T) {
	setTerminal(t, true)
	out := captureStdout(t, func() {
		for i := 1; i <= 3; i++ {
			ChunkProgress("app.json", i, 3)
		}
	})

	if got := strings.Count(out, "\r"); got != 3 {
		t.Errorf("output has %d redraws, want 3: %q", got, out)
	}
	if got := strings.Count(out, "\n"); got != 1 || !strings.HasSuffix(out, "3/3  app.json\n") {
		t.Errorf("output = %q, want one line ending after the last chunk", out)
	}
	if !strings.Contains(out, "█") {
		t.Errorf("output = %q, want the bar", out)
	}
}
