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
	"runtime"
	"strings"
	"time"

	"github.com/ulikunitz/xz"
	"gopkg.in/yaml.v3"
	"gradient-engineer/playbook"
)

// DiagnosticCommand represents a diagnostic command with its actual command and display name
type DiagnosticCommand struct {
	Command string                    // The actual command to execute
	Display string                    // Human-readable display name
	Spec    *playbook.PlaybookCommand // Pointer to the originating playbook command spec
	Timeout time.Duration             // Timeout for the command execution
}

// Toolbox represents a downloaded and extracted toolbox
type Toolbox struct {
	URL      string                   // URL to download from
	TempDir  string                   // Temporary directory where toolbox is extracted
	Playbook *playbook.PlaybookConfig // Loaded playbook configuration
}

// NewToolbox creates a new Toolbox instance
func NewToolbox(toolboxRepo, playbookName string) *Toolbox {
	// Construct the toolbox URL using the specified format
	url := fmt.Sprintf("%s%s.%s.%s.tar.xz", toolboxRepo, playbookName, runtime.GOOS, runtime.GOARCH)
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
	var rc io.ReadCloser
	if strings.HasPrefix(t.URL, "file://") {
		localPath := strings.TrimPrefix(t.URL, "file://")
		file, err := os.Open(localPath)
		if err != nil {
			return fmt.Errorf("failed to open local file: %w", err)
		}
		rc = file
	} else {
		resp, err := http.Get(t.URL)
		if err != nil {
			return fmt.Errorf("failed to download file: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("bad status: %s", resp.Status)
		}
		rc = resp.Body
	}
	defer rc.Close()

	// Create XZ reader
	xzReader, err := xz.NewReader(rc)
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

// PlaybookConfig is defined in playbook package

// GetDiagnosticCommands returns the predefined diagnostic commands with actual toolbox paths
func (t *Toolbox) GetDiagnosticCommands() ([]DiagnosticCommand, error) {
	if t.TempDir == "" {
		// Return empty slice if toolbox not downloaded yet
		return []DiagnosticCommand{}, nil
	}

	// Load playbook from the extracted toolbox archive only
	playbookPath := filepath.Join(t.TempDir, "toolbox", "playbook.yaml")
	data, err := os.ReadFile(playbookPath)
	if err != nil {
		return []DiagnosticCommand{}, fmt.Errorf("failed to read playbook.yaml from toolbox: %w", err)
	}
	var cfg playbook.PlaybookConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return []DiagnosticCommand{}, fmt.Errorf("failed to parse playbook.yaml: %w", err)
	}
	// Store playbook on toolbox for later use (e.g., system prompt)
	t.Playbook = &cfg

	toolboxPath := path.Join(t.TempDir, "toolbox")
	storeDir := filepath.Join(toolboxPath, "nix", "store")
	prootPath := filepath.Join(toolboxPath, "proot")
	prootPrefix := fmt.Sprintf("%s -b %s/nix:/nix", prootPath, toolboxPath)

	var result []DiagnosticCommand
	for i := range cfg.Commands {
		c := cfg.Commands[i]
		parts := strings.Fields(c.Command)
		if len(parts) == 0 {
			return nil, fmt.Errorf("command '%s' is empty", c.Command)
		}
		binName := parts[0]
		args := parts[1:]

		// Try to locate the binary inside toolbox nix store under */bin or */sbin
		resolved := ""
		if entries, err := os.ReadDir(storeDir); err == nil {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				candidate := filepath.Join(storeDir, e.Name(), "bin", binName)
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() && (st.Mode()&0o111 != 0) {
					resolved = candidate
					break
				}
				candidate = filepath.Join(storeDir, e.Name(), "sbin", binName)
				if st, err := os.Stat(candidate); err == nil && !st.IsDir() && (st.Mode()&0o111 != 0) {
					resolved = candidate
					break
				}
			}
		}

		var cmdStr string
		if resolved != "" {
			if len(args) > 0 {
				cmdStr = prootPrefix + " " + resolved + " " + strings.Join(args, " ")
			} else {
				cmdStr = prootPrefix + " " + resolved
			}
		} else {
			return nil, fmt.Errorf("binary for command '%s' not found in toolbox nix store", binName)
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
	return result, nil
}

// ExecuteDiagnosticCommand executes a single diagnostic command and returns its output
func (t *Toolbox) ExecuteDiagnosticCommand(cmd DiagnosticCommand) (string, error) {
	if t.TempDir == "" {
		return "", fmt.Errorf("toolbox not downloaded yet")
	}

	// If command resolution failed earlier, return a descriptive error now.
	if strings.TrimSpace(cmd.Command) == "" {
		if cmd.Spec != nil && cmd.Spec.Command != "" {
			return "", fmt.Errorf("binary for command '%s' not found in toolbox nix store", cmd.Spec.Command)
		}
		return "", fmt.Errorf("command binary not found in toolbox nix store")
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
	commands, err := t.GetDiagnosticCommands()
	if err != nil {
		return "", err
	}

	for _, cmd := range commands {
		if cmd.Display == displayName {
			return t.ExecuteDiagnosticCommand(cmd)
		}
	}

	return "", fmt.Errorf("command '%s' not found", displayName)
}
