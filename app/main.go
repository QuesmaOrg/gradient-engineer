package main

import (
	"log"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Create a new toolbox instance
	toolbox := NewToolbox("https://gradient.engineer/toolbox_v0.tar.xz")
	defer toolbox.Cleanup()

	// Create and run the Bubble Tea program which will handle toolbox download and diagnostics
	p := tea.NewProgram(NewModel(toolbox), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running Bubble Tea program: %v", err)
	}
}

