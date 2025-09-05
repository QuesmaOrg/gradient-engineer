package main

import (
	"log"
	"runtime"

	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/spf13/cobra"
)

var (
	toolboxRepo string
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "gradient-engineer [flags] [PLAYBOOK_NAME]",
		Short: "Run diagnostic playbooks using gradient engineer toolbox",
		Long: `Gradient Engineer runs diagnostic playbooks by downloading and executing
toolbox commands. The toolbox is automatically downloaded from the specified
repository based on your platform (OS and architecture).`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var playbookName string
			if len(args) > 0 {
				playbookName = args[0]
			} else {
				if runtime.GOOS == "linux" {
					playbookName = "60-second-linux"
				} else {
					playbookName = "60-second-darwin"
				}
			}

			// Create a new toolbox instance
			tb := NewToolbox(toolboxRepo, playbookName)
			defer tb.Cleanup()

			// Create and run the Bubble Tea program which will handle toolbox download and diagnostics
			p := tea.NewProgram(NewModel(tb), tea.WithMouseCellMotion())
			if _, err := p.Run(); err != nil {
				log.Fatalf("Error running Bubble Tea program: %v", err)
			}
		},
	}

	// Define flags
	rootCmd.Flags().StringVar(&toolboxRepo, "toolbox-repo", "https://gradient.engineer/toolbox/",
		"Toolbox repository URL or path (e.g., file:///home/user/mytoolboxes/)")

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
