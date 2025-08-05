package main

import _ "embed"

//go:embed playbooks/60-second-linux.yaml
var playbook60SecondLinux []byte

func embeddedPlaybook() []byte {
	return playbook60SecondLinux
}

// PlaybookConfig mirrors the structure of the embedded playbook YAML.
type PlaybookConfig struct {
	Nixpkgs struct {
		Version  string   `yaml:"version"`
		Packages []string `yaml:"packages"`
	} `yaml:"nixpkgs"`
	SystemPrompt string            `yaml:"system_prompt,omitempty"`
	Commands     []PlaybookCommand `yaml:"commands"`
}

// PlaybookCommand describes a single command entry in a playbook.
type PlaybookCommand struct {
	Command        string `yaml:"command"`
	Description    string `yaml:"description"`
	Sudo           bool   `yaml:"sudo,omitempty"`
	TimeoutSeconds int    `yaml:"timeout_seconds,omitempty"`
}


