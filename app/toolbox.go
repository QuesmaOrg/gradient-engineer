package main

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"gopkg.in/yaml.v3"
)

// DiagnosticCommand represents a diagnostic command with its actual command and display name
type DiagnosticCommand struct {
	Command string            // The actual command to execute
	Display string            // Human-readable display name
	Spec    *PlaybookCommand  // Pointer to the originating playbook command spec
	Timeout time.Duration     // Timeout for the command execution
}

// Toolbox represents a downloaded and extracted toolbox
type Toolbox struct {
	URL      string            // URL to download from
	TempDir  string            // Temporary directory where toolbox is extracted
	Playbook *PlaybookConfig   // Loaded playbook configuration
}

// NewToolbox creates a new Toolbox instance
func NewToolbox(url string) *Toolbox {
	return &Toolbox{
		URL: url,
	}
}

// Download downloads and extracts the toolbox to a temporary directory
func (t *Toolbox) Download() error {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "toolbox_*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Store the temp directory in the struct
	t.TempDir = tempDir

	// Download the file
	resp, err := http.Get(t.URL)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bad status: %s", resp.Status)
	}

	// Create XZ reader
	xzReader, err := xz.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to create XZ reader: %w", err)
	}

	// Create tar reader
	tarReader := tar.NewReader(xzReader)

	// Extract files
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break // End of tar archive
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		// Construct the full path
		targetPath := filepath.Join(tempDir, header.Name)

		// Ensure the target directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			// Create directory with write permissions for the owner
			// We use 0755 to ensure we can write to the directory, regardless of original permissions
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}

		case tar.TypeReg:
			// Create regular file
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", targetPath, err)
			}

			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return fmt.Errorf("failed to copy file content: %w", err)
			}
			file.Close()

		case tar.TypeSymlink:
			// Create symbolic link
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("failed to create symlink %s -> %s: %w", targetPath, header.Linkname, err)
			}

		default:
			return fmt.Errorf("unsupported file type: %c (%d) for %s", header.Typeflag, header.Typeflag, header.Name)
		}
	}

	return nil
}

// Cleanup removes the temporary directory and all its contents
func (t *Toolbox) Cleanup() error {
	if t.TempDir == "" {
		return nil // Nothing to clean up
	}

	if err := os.RemoveAll(t.TempDir); err != nil {
		return fmt.Errorf("failed to remove temp directory: %w", err)
	}

	t.TempDir = "" // Clear the path after cleanup
	return nil
}

// PlaybookConfig is defined in playbook.go

// GetDiagnosticCommands returns the predefined diagnostic commands with actual toolbox paths
func (t *Toolbox) GetDiagnosticCommands() []DiagnosticCommand {
	if t.TempDir == "" {
		// Return empty slice if toolbox not downloaded yet
		return []DiagnosticCommand{}
	}

	cfg, err := loadDiagnosticsConfigEmbedded()
	if err != nil {
		return []DiagnosticCommand{}
	}
	// Store playbook on toolbox for later use (e.g., system prompt)
	t.Playbook = cfg

	toolboxPath := path.Join(t.TempDir, "toolbox")
	prefix := fmt.Sprintf("%s/proot -b %s/nix:/nix %s/nix/store/", toolboxPath, toolboxPath, toolboxPath)

	var result []DiagnosticCommand
	for i := range cfg.Commands {
		c := cfg.Commands[i]
		cmdStr := prefix + c.Command
		if c.Sudo {
			cmdStr = "sudo " + cmdStr
		}
		timeout := 5 * time.Second
		if c.TimeoutSeconds > 0 {
			timeout = time.Duration(c.TimeoutSeconds) * time.Second
		}
		result = append(result, DiagnosticCommand{
			Command: cmdStr,
			Display: c.Description,
			Spec:    &cfg.Commands[i],
			Timeout: timeout,
		})
	}
	return result
}

// loadDiagnosticsConfig reads diagnostics.yaml from the working directory or alongside the executable.
func loadDiagnosticsConfig() (*PlaybookConfig, error) {
	pathsToTry := []string{"diagnostics.yaml"}
	if exe, err := os.Executable(); err == nil {
		pathsToTry = append(pathsToTry, filepath.Join(filepath.Dir(exe), "diagnostics.yaml"))
	}

	var data []byte
	var readErr error
	for _, p := range pathsToTry {
		data, readErr = os.ReadFile(p)
		if readErr == nil {
			break
		}
	}
	if readErr != nil {
		return nil, readErr
	}

	var cfg PlaybookConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// loadDiagnosticsConfigEmbedded unmarshals the embedded 60-second playbook
func loadDiagnosticsConfigEmbedded() (*PlaybookConfig, error) {
	data := embeddedPlaybook()
	var cfg PlaybookConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ExecuteDiagnosticCommand executes a single diagnostic command and returns its output
func (t *Toolbox) ExecuteDiagnosticCommand(cmd DiagnosticCommand) (string, error) {
	if t.TempDir == "" {
		return "", fmt.Errorf("toolbox not downloaded yet")
	}

	// Split the command into parts for exec.Command
	parts := strings.Fields(cmd.Command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	// Create context with timeout
	timeout := cmd.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second // Default timeout if not specified
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Create the command with context
	execCmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	execCmd.WaitDelay = timeout
	execCmd.Dir = t.TempDir

	// Execute and capture output
	output, err := execCmd.CombinedOutput()
	if err != nil && ctx.Err() != context.DeadlineExceeded {
		return "", fmt.Errorf("command '%s' failed: %w\nOutput: %s", cmd.Display, err, string(output))
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) > 100 {
		lines = lines[len(lines)-100:]
	}
	return strings.Join(lines, "\n"), nil
}

// RunSpecificDiagnosticCommand runs a specific diagnostic command by its display name
func (t *Toolbox) RunSpecificDiagnosticCommand(displayName string) (string, error) {
	commands := t.GetDiagnosticCommands()

	for _, cmd := range commands {
		if cmd.Display == displayName {
			return t.ExecuteDiagnosticCommand(cmd)
		}
	}

	return "", fmt.Errorf("command '%s' not found", displayName)
}
