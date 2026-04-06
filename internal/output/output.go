package output

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

var (
	bold   = color.New(color.Bold)
	green  = color.New(color.FgGreen, color.Bold)
	yellow = color.New(color.FgYellow)
	red    = color.New(color.FgRed)
	cyan   = color.New(color.FgCyan)
	faint  = color.New(color.Faint)
)

func Section(title string) {
	fmt.Println()
	bold.Printf("==> %s\n", title)
}

func FileSync(path string) {
	green.Print("  sync  ")
	fmt.Println(path)
}

func FileDryRun(path string, newCount, updatedCount, removedCount int) {
	yellow.Print("  peek  ")
	fmt.Printf("%s", path)
	faint.Printf("  (+%d /%d -%d)\n", newCount, updatedCount, removedCount)
}

func FilePull(path string) {
	cyan.Print("  pull  ")
	fmt.Println(path)
}

func FileAddTranslations(path string) {
	green.Print("  add  ")
	fmt.Println(path)
}

func Info(msg string) {
	faint.Printf("  %s\n", msg)
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
	faint.Printf("  $ %s\n", cmd)
}

func Error(msg string) {
	red.Fprintf(color.Error, "error: %s\n", msg)
}

func Fatal(msg string) {
	red.Fprintf(color.Error, "error: %s\n", msg)
}
