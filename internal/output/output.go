package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

var (
	bold  = color.New(color.Bold)
	green = color.New(color.FgGreen, color.Bold)
	cyan  = color.New(color.FgCyan)
	faint = color.New(color.Faint)
)

func Section(title string) {
	fmt.Println()
	_, _ = bold.Printf("==> %s\n", title)
}

func FileSync(path string) {
	_, _ = green.Print("  sync  ")
	fmt.Println(path)
}

func FilePull(path string) {
	_, _ = cyan.Print("  pull  ")
	fmt.Println(path)
}

func FileAddTranslations(path string) {
	_, _ = green.Print("  add  ")
	fmt.Println(path)
}

func Info(msg string) {
	_, _ = faint.Printf("  %s\n", msg)
}

// ChunkProgress renders an in-place progress bar.
// Call with current=1..total; the line is finalised (newline printed) when
// current == total.
func ChunkProgress(label string, current, total int) {
	const width = 25
	filled := 0
	if total > 0 {
		filled = width * current / total
	}
	bar := green.Sprint(strings.Repeat("█", filled)) + faint.Sprint(strings.Repeat("░", width-filled))
	fmt.Printf("\r  [%s]  %d/%d  %s", bar, current, total, label)
	if current >= total {
		fmt.Println()
	}
}

func Hook(cmd string) {
	_, _ = faint.Printf("  $ %s\n", cmd)
}
