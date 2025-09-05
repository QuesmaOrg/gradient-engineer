package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"

	"gradient-engineer/playbook"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthopt "github.com/anthropics/anthropic-sdk-go/option"
	tea "github.com/charmbracelet/bubbletea/v2"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
)

// SummaryCommand represents a command's description and its captured output
// used for generating an LLM summary.
type SummaryCommand struct {
	Description *playbook.PlaybookCommand
	Output      string
}

// Summarizer encapsulates LLM client configuration used for summarization.
type Summarizer struct {
	provider        string
	openaiClient    openai.Client
	anthropicClient anthropic.Client
	model           string
	models          []string // fallback models
	disabled        bool
}

// NewSummarizer constructs a Summarizer with provider selection based on env vars.
// Priority:
// - If ANTHROPIC_API_KEY is set, use Anthropic (claude-sonnet-4-0)
// - Else if OPENROUTER_API_KEY is set, use OpenRouter base and that key
// - Else if OPENAI_API_KEY starts with "sk-or-v1-", treat it as an OpenRouter key
// - Else if OPENAI_API_KEY is set, use default OpenAI base and that key
// - Else fallback to fk
// Base URL can be overridden via OPENAI_BASE_URL for OpenAI/OpenRouter.
func NewSummarizer() *Summarizer {
	baseOverride := os.Getenv("OPENAI_BASE_URL")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	fk := getFK()
	openAIKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	// Heuristic: detect OpenRouter key provided via OPENAI_API_KEY
	if openRouterKey == "" && strings.HasPrefix(openAIKey, "sk-or-v1-") {
		openRouterKey = openAIKey
	}

	// If no key is provided for any provider, mark summarizer as disabled.
	if strings.TrimSpace(anthropicKey) == "" && strings.TrimSpace(openRouterKey) == "" && strings.TrimSpace(openAIKey) == "" && strings.TrimSpace(fk) == "" {
		return &Summarizer{
			provider: "none",
			model:    "",
			disabled: true,
		}
	}

	// Anthropic has highest priority if explicitly provided
	if strings.TrimSpace(anthropicKey) != "" {
		cli := anthropic.NewClient(anthopt.WithAPIKey(anthropicKey))
		return &Summarizer{
			provider:        "anthropic",
			anthropicClient: cli,
			model:           "claude-sonnet-4-0",
			disabled:        false,
		}
	}

	usingOpenRouter := openRouterKey != ""
	usingFK := openRouterKey == "" && openAIKey == ""

	// Determine base URL for OpenAI/OpenRouter
	baseURL := ""
	if baseOverride != "" {
		baseURL = baseOverride
	} else if usingOpenRouter || usingFK {
		baseURL = "https://openrouter.ai/api/v1"
	}

	// Build OpenAI client options
	var opts []openaiopt.RequestOption
	if baseURL != "" {
		opts = append(opts, openaiopt.WithBaseURL(baseURL))
	}
	if usingOpenRouter || usingFK {
		if usingOpenRouter {
			opts = append(opts, openaiopt.WithAPIKey(openRouterKey))
		} else {
			opts = append(opts, openaiopt.WithAPIKey(fk))
		}
		// OpenRouter attribution headers
		opts = append(opts,
			openaiopt.WithHeader("X-Title", "gradient-engineer"),
			openaiopt.WithHeader("HTTP-Referer", "https://gradient.engineer"),
		)
	} else if openAIKey != "" {
		opts = append(opts, openaiopt.WithAPIKey(openAIKey))
	}

	// Choose a model slug compatible with provider
	model := "gpt-4.1"
	models := []string{}
	if usingOpenRouter {
		model = "openai/gpt-4.1"
	} else if usingFK {
		model = "deepseek/deepseek-chat-v3.1:free"
		models = []string{"deepseek/deepseek-chat-v3-0324:free", "moonshotai/kimi-k2:free", "meta-llama/llama-3.3-70b-instruct:free"}
	}

	cli := openai.NewClient(opts...)

	return &Summarizer{
		provider:     "openai",
		openaiClient: cli,
		model:        model,
		models:       models,
		disabled:     false,
	}
}

// Summarize generates a summary given a system prompt and a list of command
// descriptions paired with their outputs. The systemPrompt is passed as a
// system message, and the concatenated command outputs are passed as a user
// message.
func (s *Summarizer) Summarize(systemPrompt string, commands []SummaryCommand) (string, error) {
	ctx := context.Background()

	var b strings.Builder
	for i, c := range commands {
		if strings.TrimSpace(c.Output) == "" {
			continue
		}
		desc := ""
		if c.Description != nil {
			desc = c.Description.Description
		}
		b.WriteString(fmt.Sprintf("Command %d: %s\n", i+1, desc))
		b.WriteString(c.Output)
		b.WriteString("\n\n")
	}
	userContent := b.String()

	if s.provider == "anthropic" {
		// Anthropic Messages API
		msg, err := s.anthropicClient.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(s.model),
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(userContent)),
			},
		})
		if err != nil {
			return "", err
		}
		// Concatenate text blocks
		var out strings.Builder
		for _, c := range msg.Content {
			if c.Type == "text" {
				out.WriteString(c.Text)
			}
		}
		return out.String(), nil
	}

	// OpenAI/OpenRouter path
	params := openai.ChatCompletionNewParams{
		Model: s.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userContent),
		},
	}
	if len(s.models) > 0 {
		params.SetExtraFields(map[string]interface{}{
			"models": s.models,
		})
	}
	resp, err := s.openaiClient.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices from LLM")
	}
	return resp.Choices[0].Message.Content, nil
}

// summarizeCmd wraps the Summarizer.Summarize call into a Bubble Tea command
// that returns an llmMsg for the UI state machine.
func summarizeCmd(s *Summarizer, systemPrompt string, commands []SummaryCommand) tea.Cmd {
	return func() tea.Msg {
		summary, err := s.Summarize(systemPrompt, commands)
		if err != nil {
			return llmMsg{err: err}
		}
		return llmMsg{summary: summary}
	}
}

//go:embed .fk*.txt
var fk embed.FS

func getFK() string {
	fk1, err1 := fk.ReadFile(".fk1.txt")
	fk2, err2 := fk.ReadFile(".fk2.txt")
	if err1 != nil || err2 != nil {
		return ""
	}
	fk3 := make([]byte, len(fk1))
	for i := 0; i < len(fk1); i++ {
		fk3[i] = byte((int(fk1[i]^fk2[i]) + 256 - i - 42) ^ 0xFF)
	}
	return string(fk3)
}
