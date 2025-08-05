package playbook

type PlaybookConfig struct {
	ID      string `yaml:"id"`
	Nixpkgs struct {
		Version  string   `yaml:"version"`
		Packages []string `yaml:"packages"`
	} `yaml:"nixpkgs"`
	SystemPrompt string            `yaml:"system_prompt,omitempty"`
	Commands     []PlaybookCommand `yaml:"commands"`
}

type PlaybookCommand struct {
	Command        string `yaml:"command"`
	Description    string `yaml:"description"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}
