Third-Party Notices

This project bundles or invokes third-party software. License obligations for these components are summarized here. This file is provided for convenience and does not replace the original license texts.

Toolbox binaries (downloaded at runtime)

- Nixpkgs packages included in the toolbox archive (via `toolbox/toolbox.tar.xz` downloaded by the app):
  - coreutils (GPL-3.0-or-later)
  - util-linux (GPL-2.0-only and LGPL-2.1-or-later; mixed licensing by subcomponent)
  - procps (GPL-2.0-or-later)
  - sysstat (GPL-2.0-only)
- proot.static from Alpine APK (GPL-2.0-only)

Notes

- These binaries are redistributed as part of a prebuilt toolbox archive hosted by Quesma. Where required by GPL, the corresponding source is available from the original upstream projects (e.g., Nixpkgs, Alpine Linux, and project repositories). For convenience, you can obtain matching sources via Nixpkgs and Alpine repositories for the specific versions referenced by the toolbox. If you need assistance locating the exact sources, contact Quesma.
- The runtime downloads and executes these tools in a sandboxed environment to perform diagnostics; the Go application itself is licensed under MIT.

Go dependencies

This application depends on Go libraries via `go.mod`. Each dependency retains its own license; common licenses include MIT, Apache-2.0, BSD, and similar permissive terms. Notable direct dependencies include:

- charmbracelet libraries (`bubbletea`, `bubbles`, `lipgloss`, `glamour`)
- `github.com/ulikunitz/xz` (BSD-3-Clause)
- `gopkg.in/yaml.v3` (MIT)
- `github.com/openai/openai-go` and `github.com/anthropics/anthropic-sdk-go` (see respective repos for licenses)

For a complete, authoritative list of Go dependencies and their licenses, consult `go.mod`, `go.sum`, and the respective upstream repositories.

Requests for source

If you require assistance obtaining source code corresponding to any GPL components used in or distributed with the toolbox, please contact Quesma at support@quesma.ai.


