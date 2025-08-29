package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"gopkg.in/yaml.v3"
)

type playbookConfig struct {
	Nixpkgs struct {
		Version  string   `yaml:"version"`
		Packages []string `yaml:"packages"`
	} `yaml:"nixpkgs"`
}

func main() {
	var yamlPath string
	var outPath string

	flag.StringVar(&yamlPath, "yaml", "", "Path to YAML with nixpkgs.packages")
	flag.StringVar(&outPath, "out", "toolbox.tar.xz", "Output archive path")
	flag.Parse()

	if yamlPath == "" {
		fmt.Fprintln(os.Stderr, "error: -yaml path is required")
		os.Exit(2)
	}
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "error: this utility must run on Linux")
		os.Exit(2)
	}

	cfg, err := readYAML(yamlPath)
	if err != nil {
		fatalf("failed to read YAML: %v", err)
	}
	if len(cfg.Nixpkgs.Packages) == 0 {
		fatalf("no nixpkgs.packages listed in %s", yamlPath)
	}

	workDir, err := os.MkdirTemp("", "toolbox_work_*")
	if err != nil {
		fatalf("failed to create temporary workdir: %v", err)
	}
	defer func() {
		_ = exec.Command("chmod", "-R", "u+w", workDir).Run()
		_ = exec.Command("rm", "-rf", workDir).Run()
	}()

	toolboxDir, _ := filepath.Abs(filepath.Join(workDir, "toolbox"))
	if err := nixCopy(toolboxDir, cfg.Nixpkgs.Version, cfg.Nixpkgs.Packages); err != nil {
		fatalf("nix copy failed: %v", err)
	}

	if err := fetchAndInstallProot(toolboxDir); err != nil {
		fatalf("failed to install proot: %v", err)
	}

	// Include the playbook YAML inside the toolbox directory
	if err := copyFile(yamlPath, filepath.Join(toolboxDir, "playbook.yaml"), 0o644); err != nil {
		fatalf("failed to copy playbook YAML: %v", err)
	}

	outPath, _ = filepath.Abs(outPath)
	if err := createTarXz(outPath, toolboxDir); err != nil {
		fatalf("failed to create tar.xz: %v", err)
	}

	fmt.Printf("created %s\n", outPath)
}

func readYAML(path string) (*playbookConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg playbookConfig
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
	// Values provided by user; nix base32 style digests
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

	cmd := exec.Command("tar", "-I", "xz -e -9 -T0", "-cf", outPath, base)
	cmd.Dir = parent
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}



