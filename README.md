<p align="center">
<img width="50%" alt="gradient engineer" src="https://github.com/user-attachments/assets/b3e10245-205d-40e9-828c-3c8ac1163830" />
</p>

# `gradient-engineer` — 60‑Second Linux Analysis (Nix + LLM)

Run the classic [“60‑second Linux Performance Analysis”](https://netflixtechblog.com/linux-performance-analysis-in-60-000-milliseconds-accc10403c55) checklist in one command. A portable Nix toolbox is downloaded on the fly, diagnostics run in parallel with a simple TUI, and an optional AI summary is shown at the end.

- One command to run it all
- Fast and portable
- No Docker, no system‑wide installs
- Optional AI summary

> [!NOTE]  
> This project is an early experiment.

## Quick Start

```bash
# Any provider works; set one of these env vars to enable AI summary
export ANTHROPIC_API_KEY="<your Anthropic API key>"   # Anthropic
export OPENAI_API_KEY="<your OpenAI API key>"         # OpenAI
export OPENROUTER_API_KEY="<your OpenRouter API key>" # OpenRouter

curl -fsSL https://gradient.engineer/60-second-linux.sh | sh
```

Notes:

- If no key is set, diagnostics still run; only the AI summary is skipped.
- TUI controls: Tab toggles details; q / Esc / Ctrl+C quits.

## Demo - See It in Action!

[![asciicast](https://asciinema.org/a/738144.svg)](https://asciinema.org/a/738144)

## Build from Source

```bash
cd app
go build -o gradient-engineer-go
./gradient-engineer-go 60-second-linux
```

## Advanced

- You can override the API base URL via `OPENAI_BASE_URL` (for OpenAI/OpenRouter) if needed.

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.

## Third-party software and licenses

This app downloads a prebuilt Linux toolbox containing third-party binaries (e.g., `coreutils`, `util-linux`, `procps`, `sysstat`, and `proot`). Some of these are licensed under GPL terms. See `THIRD_PARTY_NOTICES.md` for details and links to upstream sources. Go module dependencies each retain their own licenses; consult `go.mod` and the notices file for an overview.
