package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
	"gradient-engineer/toolbox"
)

func main() {
	// Create a new toolbox instance
	tb := toolbox.NewToolbox("https://gradient.engineer/toolbox_v0.tar.xz")
	
	defer tb.Cleanup()

	// Create and run the Bubble Tea program which will handle toolbox download and diagnostics
	p := tea.NewProgram(NewModel(tb), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running Bubble Tea program: %v", err)
	}
}

