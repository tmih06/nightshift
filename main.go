package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// nightshift: a Bubble Tea dashboard for the agent-sandbox warm pool.
// go run . opens the TUI, go run . -once prints one frame to stdout.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "-once" {
		runOnce()
		return
	}
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
