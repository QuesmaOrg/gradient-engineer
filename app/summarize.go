package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthopt "github.com/anthropics/anthropic-sdk-go/option"
	tea "github.com/charmbracelet/bubbletea"
	openai "github.com/openai/openai-go"
	openaiopt "github.com/openai/openai-go/option"
	"gradient-engineer/playbook"
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
	disabled        bool
}

// NewSummarizer constructs a Summarizer with provider selection based on env vars.
// Priority:
// - If ANTHROPIC_API_KEY is set, use Anthropic (claude-sonnet-4-0)
// - Else if OPENROUTER_API_KEY is set, use OpenRouter base and that key
// - Else if OPENAI_API_KEY starts with "sk-or-v1-", treat it as an OpenRouter key
// - Else if OPENAI_API_KEY is set, use default OpenAI base and that key
// Base URL can be overridden via OPENAI_BASE_URL for OpenAI/OpenRouter.
func NewSummarizer() *Summarizer {
	baseOverride := os.Getenv("OPENAI_BASE_URL")
	openRouterKey := os.Getenv("OPENROUTER_API_KEY")
	openAIKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	// Heuristic: detect OpenRouter key provided via OPENAI_API_KEY
	if openRouterKey == "" && strings.HasPrefix(openAIKey, "sk-or-v1-") {
		openRouterKey = openAIKey
	}

	// If no key is provided for any provider, mark summarizer as disabled.
	if strings.TrimSpace(anthropicKey) == "" && strings.TrimSpace(openRouterKey) == "" && strings.TrimSpace(openAIKey) == "" {
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

	// Determine base URL for OpenAI/OpenRouter
	baseURL := ""
	if baseOverride != "" {
		baseURL = baseOverride
	} else if usingOpenRouter {
		baseURL = "https://openrouter.ai/api/v1"
	}

	// Build OpenAI client options
	var opts []openaiopt.RequestOption
	if baseURL != "" {
		opts = append(opts, openaiopt.WithBaseURL(baseURL))
	}
	if usingOpenRouter {
		opts = append(opts, openaiopt.WithAPIKey(openRouterKey))
		// OpenRouter attribution headers
		opts = append(opts,
			openaiopt.WithHeader("X-Title", "gradient-engineer"),
			openaiopt.WithHeader("HTTP-Referer", "https://gradient.engineer"),
		)
	} else if openAIKey != "" {
		opts = append(opts, openaiopt.WithAPIKey(openAIKey))
	}

	cli := openai.NewClient(opts...)

	// Choose a model slug compatible with provider
	model := "gpt-4.1"
	if usingOpenRouter {
		model = "openai/gpt-4.1"
	}

	return &Summarizer{
		provider:     "openai",
		openaiClient: cli,
		model:        model,
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
	resp, err := s.openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: s.model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userContent),
		},
	})
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
