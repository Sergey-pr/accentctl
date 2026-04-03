package output

import (
	"fmt"

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

func FileExport(path string) {
	cyan.Print("  export  ")
	fmt.Println(path)
}

func FileAddTranslations(path string) {
	green.Print("  add  ")
	fmt.Println(path)
}

func Info(msg string) {
	faint.Printf("  %s\n", msg)
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
