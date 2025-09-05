package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/v2/spinner"
	"github.com/charmbracelet/bubbles/v2/viewport"
	tea "github.com/charmbracelet/bubbletea/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss/v2"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	headerStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")) // foreground black; background will be per-char gradient
	pendingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	footerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
)

// commandStatus represents the execution status of a diagnostic command.
// The order is important because we rely on the natural zero value (pending)
// when the model is initialised.
type commandStatus int

const (
	statusPending commandStatus = iota
	statusRunning
	statusSuccess
	statusError
)

// resultMsg is a Bubble Tea message carrying the result of a command
// execution. It contains the index of the command in the model slice so that
// we can update the correct entry.
type resultMsg struct {
	index  int
	output string
	err    error
}

type downloadMsg struct {
	err error
}

type llmMsg struct {
	summary string
	err     error
}

type model struct {
	toolbox  *Toolbox
	commands []DiagnosticCommand

	statuses []commandStatus
	outputs  []string
	errors   []error

	vp viewport.Model

	spin spinner.Model

	startTime time.Time

	downloaded bool

	showDetails bool

	// LLM
	summarizing   bool
	summary       string // rendered ANSI summary
	summaryErr    error
	summaryNotice string

	done bool

	summarizer *Summarizer
}

// NewModel constructs a model initialised with all diagnostic commands in a
// pending state.
func NewModel(tb *Toolbox) *model {
	cmds, _ := tb.GetDiagnosticCommands()
	n := len(cmds)

	vp := viewport.New(viewport.WithWidth(0), viewport.WithHeight(0))
	vp.MouseWheelEnabled = true

	return &model{
		toolbox:  tb,
		commands: cmds,
		statuses: make([]commandStatus, n),
		outputs:  make([]string, n),
		errors:   make([]error, n),
		vp:       vp,
		spin: func() spinner.Model {
			s := spinner.New()
			s.Spinner = spinner.MiniDot
			s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
			return s
		}(),
		startTime:  time.Now(),
		summarizer: NewSummarizer(),
	}
}

// Init starts the execution of every diagnostic command in a separate
// goroutine using Bubble Tea commands. All commands are launched immediately
// so they run in parallel.
func (m *model) Init() tea.Cmd {
	// Start toolbox download first (commands will be populated after download).
	// Create command batch: start toolbox download and spinner.
	cmds := []tea.Cmd{
		downloadToolboxCmd(m.toolbox),
		m.spin.Tick,
	}
	return tea.Batch(cmds...)
}

// summarizeCmd moved to summarize.go

// downloadToolboxCmd runs the toolbox download in a goroutine.
func downloadToolboxCmd(tb *Toolbox) tea.Cmd {
	return func() tea.Msg {
		err := tb.Download()
		return downloadMsg{err: err}
	}
}

// runCommandCmd wraps the synchronous Toolbox.ExecuteDiagnosticCommand method
// in an asynchronous Bubble Tea command.
func runCommandCmd(tb *Toolbox, cmd DiagnosticCommand, idx int) tea.Cmd {
	return func() tea.Msg {
		out, err := tb.ExecuteDiagnosticCommand(cmd)
		return resultMsg{index: idx, output: out, err: err}
	}
}

// Update handles all incoming messages, updating the model state and returning
// any follow-up commands.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case downloadMsg:
		if msg.err != nil {
			// download failed, stop program
			fmt.Println(msg.err)
			m.done = true
			return m, tea.Quit
		}
		m.downloaded = true
		// Populate commands now that toolbox is available
		commands, err := m.toolbox.GetDiagnosticCommands()
		if err != nil {
			fmt.Println(err)
			m.done = true
			return m, tea.Quit
		}
		m.commands = commands
		n := len(m.commands)
		m.statuses = make([]commandStatus, n)
		m.outputs = make([]string, n)
		m.errors = make([]error, n)

		// start executing diagnostic commands
		var cmds []tea.Cmd
		for i, cmd := range m.commands {
			m.statuses[i] = statusRunning
			cmds = append(cmds, runCommandCmd(m.toolbox, cmd, i))
		}
		return m, tea.Batch(cmds...)

	case resultMsg:
		// Command finished.
		if msg.err != nil {
			m.statuses[msg.index] = statusError
			m.errors[msg.index] = msg.err
		} else {
			m.statuses[msg.index] = statusSuccess
			m.outputs[msg.index] = msg.output
		}

		// Check whether all commands are finished.
		allDone := true
		for _, st := range m.statuses {
			if st == statusRunning || st == statusPending {
				allDone = false
				break
			}
		}
		if allDone {
			if !m.summarizing && m.summary == "" {
				// If summarizer is disabled (no API key), skip summarization and show a notice.
				if m.summarizer == nil || m.summarizer.disabled {
					m.summaryNotice = "No API key provided; skipping AI summary.\nSet the API key with OPENAI_API_KEY, OPENROUTER_API_KEY, or ANTHROPIC_API_KEY."
					return m, nil
				}
				m.summarizing = true
				var sc []SummaryCommand
				for i := range m.commands {
					sc = append(sc, SummaryCommand{
						Description: m.commands[i].Spec,
						Output:      m.outputs[i],
					})
				}
				if m.toolbox == nil || m.toolbox.Playbook == nil || m.toolbox.Playbook.SystemPrompt == "" {
					m.summaryErr = fmt.Errorf("system_prompt is required in playbook")
					return m, nil
				}
				systemPrompt := m.toolbox.Playbook.SystemPrompt
				return m, summarizeCmd(m.summarizer, systemPrompt, sc)
			}
		}
		// No follow-up commands here.
		return m, nil

	case llmMsg:
		m.summarizing = false
		if msg.err != nil {
			m.summaryErr = msg.err
		} else {
			rendered, err := glamour.Render(msg.summary, "dark")
			if err != nil {
				m.summaryErr = err
			} else {
				m.summary = rendered
			}
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		// Keep the viewport dimensions in sync with the terminal.
		m.vp.SetWidth(msg.Width)
		// Leave a single line at the bottom for the prompt/scroll bar.
		if msg.Height > 1 {
			m.vp.SetHeight(msg.Height - 1)
		} else {
			m.vp.SetHeight(msg.Height)
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "tab":
			m.showDetails = !m.showDetails
			return m, nil
		}
		// Delegate other key events to the viewport for scrolling.
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		// Delegate mouse events (including wheel) to the viewport for scrolling.
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View produces a string representation of the current program state for the
// terminal user interface.
func (m *model) View() string {
	// Build the content string and assign it to the viewport.
	m.vp.SetContent(m.generateContent())
	return m.vp.View()
}

// generateContent builds the textual representation of the program status.
func (m *model) generateContent() string {
	const (
		iconPending = "●"
		iconSuccess = "✓"
		iconError   = "✗"
	)

	// Build the commands section
	var cmdBuf strings.Builder

	if !m.downloaded {
		// Show downloading placeholder
		cmdBuf.WriteString(runningStyle.Render(fmt.Sprintf("%s Downloading toolbox...", m.spin.View())))
		cmdBuf.WriteString("\n")
	}

	for i, cmd := range m.commands {
		icon := iconPending
		switch m.statuses[i] {
		case statusRunning:
			icon = m.spin.View()
		case statusSuccess:
			icon = iconSuccess
		case statusError:
			icon = iconError
		}

		var lineStyle lipgloss.Style
		switch m.statuses[i] {
		case statusRunning:
			lineStyle = runningStyle
		case statusSuccess:
			lineStyle = successStyle
		case statusError:
			lineStyle = errorStyle
		default:
			lineStyle = pendingStyle
		}
		cmdBuf.WriteString(lineStyle.Render(fmt.Sprintf("%s %s", icon, cmd.Display)))
		cmdBuf.WriteString("\n")

		if m.showDetails {
			switch m.statuses[i] {
			case statusSuccess:
				if m.outputs[i] != "" {
					cmdBuf.WriteString(indent(m.outputs[i], "    "))
					cmdBuf.WriteString("\n")
				}
			case statusError:
				if m.errors[i] != nil {
					cmdBuf.WriteString(indent(fmt.Sprintf("ERROR: %v", m.errors[i]), "    "))
					cmdBuf.WriteString("\n")
				}
			}
		}
	}

	// Strip final \n from cmdBuf if present
	cmdStr := cmdBuf.String()
	if strings.HasSuffix(cmdStr, "\n") {
		cmdStr = cmdStr[:len(cmdStr)-1]
	}
	cmdBuf.Reset()
	cmdBuf.WriteString(cmdStr)

	// Border the commands with a title
	sectionTitle := renderGradientHeader(" 60-second Linux analysis ", time.Since(m.startTime).Seconds())
	commandsBox := sectionTitle + "\n\n" + cmdBuf.String()

	// Assemble full UI
	var b strings.Builder
	b.WriteString(generateBanner(time.Since(m.startTime).Seconds()))
	b.WriteString("\n\n")
	b.WriteString(commandsBox)

	if m.summarizing {
		b.WriteString("\n\n")
		b.WriteString(runningStyle.Render(fmt.Sprintf("%s Summarizing results with AI…", m.spin.View())))
	}
	if m.summary != "" {
		b.WriteString("\n\n")
		b.WriteString(renderGradientHeader(" AI Summary ", time.Since(m.startTime).Seconds()))
		b.WriteString("\n")
		b.WriteString(m.summary)
	}
	if m.summaryNotice != "" {
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render(m.summaryNotice))
	}
	if m.summaryErr != nil {
		b.WriteString("\n\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("LLM error: %v", m.summaryErr)))
	}

	b.WriteString("\n\n")
	b.WriteString(footerStyle.Render("Tab: toggle details   q: quit"))
	b.WriteString("\n")

	return b.String()
}

// indent prefixes every line in text with prefix.
func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// ASCII banner lines for Gradient Engineer logo.
var bannerLines = []string{
	"                                      ░██ ░██                         ░██     ",
	"                                      ░██                             ░██     ",
	" ░████████ ░██░████  ░██████    ░████████ ░██ ░███████  ░████████  ░████████  ",
	"░██    ░██ ░███           ░██  ░██    ░██ ░██░██    ░██ ░██    ░██    ░██     ",
	"░██    ░██ ░██       ░███████  ░██    ░██ ░██░█████████ ░██    ░██    ░██     ",
	"░██   ░███ ░██      ░██   ░██  ░██   ░███ ░██░██        ░██    ░██    ░██     ",
	" ░█████░██ ░██       ░█████░██  ░█████░██ ░██ ░███████  ░██    ░██     ░████  ",
	"       ░██                                                                    ",
	" ░███████                                                                     ",
	"                                 ░██                                          ",
	"                                                                              ",
	" ░███████  ░████████   ░████████ ░██░████████   ░███████   ░███████  ░██░████ ",
	"░██    ░██ ░██    ░██ ░██    ░██ ░██░██    ░██ ░██    ░██ ░██    ░██ ░███     ",
	"░█████████ ░██    ░██ ░██    ░██ ░██░██    ░██ ░█████████ ░█████████ ░██      ",
	"░██        ░██    ░██ ░██   ░███ ░██░██    ░██ ░██        ░██        ░██      ",
	" ░███████  ░██    ░██  ░█████░██ ░██░██    ░██  ░███████   ░███████  ░██      ",
	"                             ░██                                              ",
	"                       ░███████                                               ",
}

// Convert HSV to RGB (0-360, 0-1, 0-1) output uint8 components.
func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	c := v * s
	hPrime := h / 60.0
	x := c * (1 - math.Abs(math.Mod(hPrime, 2)-1))
	var r1, g1, b1 float64
	switch int(hPrime) {
	case 0:
		r1, g1, b1 = c, x, 0
	case 1:
		r1, g1, b1 = x, c, 0
	case 2:
		r1, g1, b1 = 0, c, x
	case 3:
		r1, g1, b1 = 0, x, c
	case 4:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := v - c
	r, g, b := (r1+m)*255, (g1+m)*255, (b1+m)*255
	return uint8(r), uint8(g), uint8(b)
}

// renderGradientHeader renders text with a horizontal rainbow background.
func renderGradientHeader(text string, t float64) string {
	var b strings.Builder
	for i, ch := range text {
		progress := math.Mod(float64(i)/100+t*0.1, 1.0)
		hue := progress * 360.0
		// lower brightness for less intense colors
		r, g, c := hsvToRGB(hue, 0.8, 0.5)
		colorStr := fmt.Sprintf("#%02X%02X%02X", r, g, c)
		b.WriteString(lipgloss.NewStyle().Bold(true).
			Background(lipgloss.Color(colorStr)).
			Foreground(lipgloss.Color("255")).
			Render(string(ch)))
	}
	return b.String()
}

// generateBanner returns the rainbow animated banner based on time t (seconds).
func generateBanner(t float64) string {
	var b strings.Builder
	for _, line := range bannerLines {
		for i, ch := range line {
			progress := math.Mod(float64(i)/float64(len(line))+t*0.10+0.5, 1.0)
			hue := progress * 360.0
			r, g, cc := hsvToRGB(hue, 1.0, 1.0)
			colorStr := fmt.Sprintf("#%02X%02X%02X", r, g, cc)
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(colorStr)).Render(string(ch)))
		}
		b.WriteString("\n")
	}
	return b.String()
}
