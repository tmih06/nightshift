package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tmih06/nightshift/internal/dashboard"
)

// nightshift: a Bubble Tea dashboard for the agent-sandbox warm pool.
// go run ./cmd/nightshift opens the TUI (two panes, mouse enabled),
// go run ./cmd/nightshift -once prints one frame to stdout.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "-once" {
		dashboard.RunOnce()
		return
	}
	p := tea.NewProgram(dashboard.New(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
