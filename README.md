<p align="center">
<img width="50%" alt="gradient engineer" src="https://github.com/user-attachments/assets/b3e10245-205d-40e9-828c-3c8ac1163830" />
</p>

# `gradient-engineer` — 60‑Second Linux Analysis (Nix + LLM)

Run the classic [60‑second Linux Performance Analysis](https://netflixtechblog.com/linux-performance-analysis-in-60-000-milliseconds-accc10403c55) checklist in one command on Linux (or macOS). It solves the `command not found` problem on minimal systems by downloading a portable [Nix](https://nixos.org/) toolbox on the fly. Diagnostics run in parallel with a simple TUI, and an optional AI summary is shown at the end.

- **One command**: Run the entire analysis with a single line.
- **Fast**: Get a full system snapshot in about 6 seconds.
- **Just works**: No sudo, no Docker, and no permanent installation.
- **AI-powered summary**: Let an LLM explain the raw command outputs.

More details in the blog post: [60-Second Linux Analysis with Nix and LLMs](https://quesma.com/blog/60s-linux-analysis-nix-llms/).

> [!NOTE]  
> This project is an early experiment.

## Why? The `command not found` Nightmare

You SSH into a server to troubleshoot an issue, run a standard tool like `iostat`, and are greeted with `command not found`. Minimal container images and server installations often lack essential diagnostic tools. Installing them during an outage is a waste of precious time and can be blocked by firewalls, package manager issues, or immutable filesystems.

`gradient-engineer` solves this by providing a portable, on-demand toolbox with all the necessary utilities, powered by [Nix](https://nixos.org/). It runs the tools you need without requiring root access or permanent changes to the system.

## Quick Start

Run the following command in your terminal. It works on both Linux and macOS.

```bash
curl -fsSL https://gradient.engineer/60-second-linux.sh | sh
```

To enable the optional AI summary, set an API key from a supported provider _before_ running the script:

```bash
export ANTHROPIC_API_KEY="<your Anthropic API key>"   # OR
export OPENAI_API_KEY="<your OpenAI API key>"         # OR
export OPENROUTER_API_KEY="<your OpenRouter API key>"
```

**Notes:**

- If no key is set, diagnostics still run; only the AI summary is skipped.
- You can override the API base URL via `OPENAI_BASE_URL` (for OpenAI/OpenRouter).
- TUI controls: `Tab` toggles details; `q` / `Esc` / `Ctrl+C` quits.

## Demo

[![asciicast](https://asciinema.org/a/738144.svg)](https://asciinema.org/a/738144)

## Build from Source

Requires [Go 1.25 or newer](https://go.dev/).

```bash
git clone https://github.com/QuesmaOrg/gradient-engineer.git
cd gradient-engineer/app
go build -o gradient-engineer-go

# Run for your platform:
./gradient-engineer-go 60-second-linux  # for Linux
./gradient-engineer-go 60-second-darwin # for macOS
```

## Advanced

- You can override the API base URL via `OPENAI_BASE_URL` (for OpenAI/OpenRouter) if needed.

## Contributing

This is an early prototype, and we're just getting started. The repository is open-source, and we're excited to explore what's possible. Have a look at the current [playbooks](./playbook/). We started with the classic, but we bet you have your own favorite commands—feel free to contribute them!

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.

## Third-party software and licenses

This app downloads a prebuilt Linux toolbox containing third-party binaries (e.g., `coreutils`, `util-linux`, `procps`, `sysstat`, and `proot`). Some of these are licensed under GPL terms. See `THIRD_PARTY_NOTICES.md` for details and links to upstream sources. Go module dependencies each retain their own licenses; consult `go.mod` and the notices file for an overview.
