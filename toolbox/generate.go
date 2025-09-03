package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"gradient-engineer/playbook"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	playbookPath string
	outDir       string
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "toolbox-generator [flags]",
		Short: "Generate toolbox archives from playbook configurations",
		Long: `Toolbox Generator creates portable toolbox archives containing Nix packages
and diagnostic tools defined in playbook configurations. The generated archives
include all necessary dependencies and can be distributed and executed on
target systems.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if playbookPath == "" {
				return fmt.Errorf("playbook path is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateToolbox()
		},
	}

	// Define flags
	rootCmd.Flags().StringVarP(&playbookPath, "playbook", "p", "", "Path to playbook file (required)")
	rootCmd.Flags().StringVarP(&outDir, "out", "o", ".", "Output directory for generated archive")

	// Mark required flags
	rootCmd.MarkFlagRequired("playbook")

	// Execute the command
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func generateToolbox() error {
	cfg, err := readPlaybook(playbookPath)
	if err != nil {
		return fmt.Errorf("failed to read playbook: %w", err)
	}
	if runtime.GOOS == "linux" {
		if len(cfg.Nixpkgs.Packages) == 0 {
			return fmt.Errorf("no nixpkgs.packages listed in %s", playbookPath)
		}
	}

	workDir, err := os.MkdirTemp("", "toolbox_work_*")
	if err != nil {
		return fmt.Errorf("failed to create temporary workdir: %w", err)
	}
	defer func() {
		_ = exec.Command("chmod", "-R", "u+w", workDir).Run()
		_ = exec.Command("rm", "-rf", workDir).Run()
	}()

	toolboxDir, _ := filepath.Abs(filepath.Join(workDir, "toolbox"))

	if runtime.GOOS == "linux" {
		if err := nixCopy(toolboxDir, cfg.Nixpkgs.Version, cfg.Nixpkgs.Packages); err != nil {
			return fmt.Errorf("nix copy failed: %w", err)
		}

		if err := fetchAndInstallProot(toolboxDir); err != nil {
			return fmt.Errorf("failed to install proot: %w", err)
		}
	}

	// Include the playbook file inside the toolbox directory
	if err := copyFile(playbookPath, filepath.Join(toolboxDir, "playbook.yaml"), 0o644); err != nil {
		return fmt.Errorf("failed to copy playbook file: %w", err)
	}

	outDir, _ = filepath.Abs(outDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed to ensure output directory: %w", err)
	}
	archiveName := fmt.Sprintf("%s.%s.%s.tar.xz", cfg.ID, runtime.GOOS, runtime.GOARCH)
	outPath := filepath.Join(outDir, archiveName)
	if err := createTarXz(outPath, toolboxDir); err != nil {
		return fmt.Errorf("failed to create tar.xz: %w", err)
	}

	fmt.Printf("created %s\n", outPath)
	return nil
}

func readPlaybook(path string) (*playbook.PlaybookConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg playbook.PlaybookConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func nixCopy(destDir string, version string, pkgs []string) error {
	if _, err := exec.LookPath("nix"); err != nil {
		return fmt.Errorf("nix not found in PATH: %w", err)
	}
	args := []string{
		"--extra-experimental-features", "flakes",
		"--extra-experimental-features", "nix-command",
		"copy",
		"--to", destDir,
	}
	// Build flake reference. If a version (commit SHA) is provided, pin to that revision.
	// Otherwise, fall back to the registry alias "nixpkgs".
	flakeRef := "nixpkgs"
	if version != "" {
		// Expecting a commit SHA; use the GitHub flake URL form.
		flakeRef = "github:NixOS/nixpkgs/" + version
	}
	for _, p := range pkgs {
		args = append(args, flakeRef+"#"+p)
	}
	cmd := exec.Command("nix", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fetchAndInstallProot(destDir string) error {
	arch := runtime.GOARCH
	var url string
	switch arch {
	case "amd64":
		url = "https://web.archive.org/web/20240412082958if_/http://dl-cdn.alpinelinux.org/alpine/edge/testing/x86_64/proot-static-5.4.0-r0.apk"
	case "arm64":
		url = "https://web.archive.org/web/20240412083320if_/http://dl-cdn.alpinelinux.org/alpine/edge/testing/aarch64/proot-static-5.4.0-r0.apk"
	default:
		return fmt.Errorf("unsupported architecture %s; only amd64 and arm64 are supported", arch)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return err
	}

	if err := extractProotFromAPK(buf.Bytes(), filepath.Join(destDir, "proot")); err != nil {
		return err
	}
	return nil
}

func extractProotFromAPK(apk []byte, destPath string) error {
	workDir, err := os.MkdirTemp("", "proot_apk_*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	apkPath := filepath.Join(workDir, "proot.apk")
	if err := os.WriteFile(apkPath, apk, 0o644); err != nil {
		return err
	}

	gzPath := apkPath + ".tar.gz"
	if err := os.Rename(apkPath, gzPath); err != nil {
		return err
	}

	cmd := exec.Command("tar", "-xzf", gzPath, "-C", workDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tar extract failed: %w", err)
	}

	candidate := filepath.Join(workDir, "usr", "bin", "proot.static")
	if _, err := os.Stat(candidate); err == nil {
		return copyExecutable(candidate, destPath)
	}

	var found string
	_ = filepath.WalkDir(workDir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && filepath.Base(p) == "proot.static" {
			found = p
			return io.EOF
		}
		return nil
	})
	if found == "" {
		return fmt.Errorf("proot.static not found in APK")
	}
	return copyExecutable(found, destPath)
}

func copyExecutable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstF, srcF); err != nil {
		dstF.Close()
		return err
	}
	return dstF.Close()
}

func copyFile(srcPath, dstPath string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	srcF, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcF.Close()

	dstF, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dstF, srcF); err != nil {
		dstF.Close()
		return err
	}
	return dstF.Close()
}

func createTarXz(outPath string, dir string) error {
	parent := filepath.Dir(dir)
	base := filepath.Base(dir)

	if runtime.GOOS == "linux" {
		cmd := exec.Command("tar", "-I", "xz -e -9 -T0", "-cf", outPath, base)
		cmd.Dir = parent
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command("tar", "-cJf", outPath, "--options", "xz:compression-level=9", base)
	cmd.Dir = parent
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
