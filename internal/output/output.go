package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

var (
	bold   = color.New(color.Bold)
	green  = color.New(color.FgGreen, color.Bold)
	cyan   = color.New(color.FgCyan)
	faint  = color.New(color.Faint)
	yellow = color.New(color.FgYellow, color.Bold)

	isTerminal = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
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

func Warn(msg string) {
	_, _ = yellow.Printf("  %s\n", msg)
}

// ChunkProgress redraws a progress bar in place on a terminal, or prints one line per chunk in logs.
func ChunkProgress(label string, current, total int) {
	if !isTerminal {
		fmt.Printf("  %d/%d  %s\n", current, total, label)
		return
	}
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
