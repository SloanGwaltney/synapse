package tui

import (
	"fmt"
	"strings"

	"synapse/internal/embedder"
	"synapse/internal/llm"
	"synapse/internal/rag"
	"synapse/internal/store"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type chatState int

const (
	chatIdle chatState = iota
	chatSearching
	chatGenerating
)

type chatModel struct {
	viewport    viewport.Model
	input       textinput.Model
	spinner     spinner.Model
	renderer    *glamour.TermRenderer
	messages    []chatMessage
	history     []llm.Message
	st          store.Store
	emb         *embedder.OllamaEmbedder
	chat        *llm.OllamaChat
	overview    string
	state       chatState
	k           int
	width       int
	height      int
	initialized bool
}

type chatMessage struct {
	role    string
	content string
}

// answerMsg is sent when a RAG query completes.
type answerMsg struct {
	answer string
	err    error
}

func newChatModel(st store.Store, ollamaURL, embedModel, chatModelName, overview string, k int) chatModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = selectedStyle

	ti := textinput.New()
	ti.Placeholder = "Ask a question about your codebase..."
	ti.CharLimit = 2000
	ti.Focus()

	return chatModel{
		spinner:  sp,
		input:    ti,
		st:       st,
		emb:      embedder.NewOllamaEmbedder(ollamaURL, embedModel),
		chat:     llm.NewOllamaChat(ollamaURL, chatModelName),
		overview: overview,
		k:        k,
		state:    chatIdle,
	}
}

func (m *chatModel) initViewport(width, height int) {
	m.width = width
	m.height = height

	// Layout: viewport + status bar (1 line) + input (1 line) + borders/gaps (1 line).
	vpHeight := height - 3
	if vpHeight < 5 {
		vpHeight = 5
	}
	m.viewport = viewport.New(width, vpHeight)
	m.viewport.SetContent(dimStyle.Render("Welcome to Synapse chat! Ask a question about your codebase.\n\nCommands: /help, /clear, /exit"))

	m.input.Width = width - 4

	// Create glamour renderer matched to current width.
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-2),
	)
	if err == nil {
		m.renderer = r
	}

	m.initialized = true
}

func askQuestion(question string, st store.Store, emb *embedder.OllamaEmbedder, chat *llm.OllamaChat, history []llm.Message, overview string, k int) tea.Cmd {
	return func() tea.Msg {
		chunks, err := rag.HybridRetrieve(question, st, emb, k)
		if err != nil {
			return answerMsg{err: fmt.Errorf("retrieval error: %w", err)}
		}

		msgs := rag.BuildMessages(chunks, history, question, overview)
		answer, err := chat.Generate(msgs)
		if err != nil {
			return answerMsg{err: fmt.Errorf("generation error: %w", err)}
		}

		return answerMsg{answer: answer}
	}
}

func (m chatModel) Update(msg tea.Msg) (chatModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.initViewport(msg.Width, msg.Height)
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, nil

	case answerMsg:
		m.state = chatIdle
		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{role: "error", content: msg.err.Error()})
		} else {
			m.messages = append(m.messages, chatMessage{role: "assistant", content: msg.answer})
			m.history = append(m.history, llm.Message{Role: "assistant", Content: msg.answer})
			if len(m.history) > 20 {
				m.history = m.history[len(m.history)-20:]
			}
		}
		m.viewport.SetContent(m.renderMessages())
		m.viewport.GotoBottom()
		return m, nil

	case spinner.TickMsg:
		if m.state != chatIdle {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			// Re-render viewport so the spinner frame updates.
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.state != chatIdle {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyEnter:
			question := strings.TrimSpace(m.input.Value())
			if question == "" {
				return m, nil
			}
			m.input.Reset()

			switch question {
			case "/exit", "/quit":
				return m, tea.Quit
			case "/clear":
				m.messages = nil
				m.history = nil
				m.viewport.SetContent(dimStyle.Render("Conversation cleared."))
				return m, nil
			case "/help":
				helpText := "Commands:\n  /clear  - clear conversation history\n  /exit   - quit\n  /help   - show this help"
				m.messages = append(m.messages, chatMessage{role: "system", content: helpText})
				m.viewport.SetContent(m.renderMessages())
				m.viewport.GotoBottom()
				return m, nil
			}

			m.messages = append(m.messages, chatMessage{role: "user", content: question})
			m.history = append(m.history, llm.Message{Role: "user", Content: question})
			m.state = chatSearching
			m.viewport.SetContent(m.renderMessages())
			m.viewport.GotoBottom()

			return m, tea.Batch(
				m.spinner.Tick,
				askQuestion(question, m.st, m.emb, m.chat, m.history[:len(m.history)-1], m.overview, m.k),
			)
		}
	}

	// Update text input.
	if m.state == chatIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update viewport (scrolling).
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m chatModel) renderMarkdown(content string) string {
	if m.renderer == nil {
		return assistantMsgStyle.Render(content)
	}
	rendered, err := m.renderer.Render(content)
	if err != nil {
		return assistantMsgStyle.Render(content)
	}
	return strings.TrimRight(rendered, "\n")
}

func (m chatModel) renderMessages() string {
	var sb strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			sb.WriteString(userMsgStyle.Render("You: ") + msg.content + "\n\n")
		case "assistant":
			sb.WriteString(m.renderMarkdown(msg.content) + "\n\n")
		case "error":
			sb.WriteString(errorStyle.Render("Error: "+msg.content) + "\n\n")
		case "system":
			sb.WriteString(dimStyle.Render(msg.content) + "\n\n")
		}
	}

	if m.state != chatIdle {
		label := "Searching..."
		if m.state == chatGenerating {
			label = "Generating..."
		}
		sb.WriteString(m.spinner.View() + " " + dimStyle.Render(label) + "\n")
	}

	return sb.String()
}

func (m chatModel) View(width, height int) string {
	if !m.initialized {
		return ""
	}

	statusText := "idle"
	switch m.state {
	case chatSearching:
		statusText = "searching..."
	case chatGenerating:
		statusText = "generating..."
	}
	statusBar := statusBarStyle.
		Width(m.width).
		Render(fmt.Sprintf(" synapse chat • %s", statusText))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		statusBar,
		m.input.View(),
	)
}
