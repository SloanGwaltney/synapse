package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type setupPage int

const (
	setupPageEmbed setupPage = iota
	setupPageChat
)

type setupModel struct {
	models      []OllamaModel
	embedModels []OllamaModel
	chatModels  []OllamaModel
	embedCursor int
	chatCursor  int
	page        setupPage
	loaded      bool
	err         error
}

// fetchModelsMsg is sent when models have been fetched from Ollama.
type fetchModelsMsg struct {
	models []OllamaModel
	err    error
}

func fetchModels(baseURL string) tea.Cmd {
	return func() tea.Msg {
		models, err := ListModels(baseURL)
		return fetchModelsMsg{models: models, err: err}
	}
}

func (m setupModel) Update(msg tea.Msg, cfg Config) (setupModel, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchModelsMsg:
		if msg.err != nil {
			m.err = msg.err
			m.loaded = true
			return m, nil
		}
		m.models = msg.models
		m.loaded = true

		// Split into embedding and chat model lists.
		for _, model := range msg.models {
			nameLower := strings.ToLower(model.Name)
			isEmbed := strings.Contains(nameLower, "embed") || strings.Contains(nameLower, "nomic")
			if isEmbed {
				m.embedModels = append(m.embedModels, model)
			} else {
				m.chatModels = append(m.chatModels, model)
			}
		}

		// Fallback: if no embedding models found, show all.
		if len(m.embedModels) == 0 {
			m.embedModels = msg.models
		}
		if len(m.chatModels) == 0 {
			m.chatModels = msg.models
		}

		// Find cursor positions for defaults.
		for i, model := range m.embedModels {
			if model.Name == cfg.Model {
				m.embedCursor = i
				break
			}
		}
		for i, model := range m.chatModels {
			if model.Name == cfg.ChatModel {
				m.chatCursor = i
				break
			}
		}

	case tea.KeyMsg:
		if !m.loaded || m.err != nil {
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.page == setupPageEmbed && m.embedCursor > 0 {
				m.embedCursor--
			} else if m.page == setupPageChat && m.chatCursor > 0 {
				m.chatCursor--
			}
		case "down", "j":
			if m.page == setupPageEmbed && m.embedCursor < len(m.embedModels)-1 {
				m.embedCursor++
			} else if m.page == setupPageChat && m.chatCursor < len(m.chatModels)-1 {
				m.chatCursor++
			}
		}
	}
	return m, nil
}

// confirmed returns true when the user presses Enter on the chat page.
func (m setupModel) confirmed() bool {
	return m.page == setupPageChat
}

// advancePage moves from embed page to chat page. Returns true if it advanced.
func (m *setupModel) advancePage() bool {
	if m.page == setupPageEmbed {
		m.page = setupPageChat
		return true
	}
	return false
}

func (m setupModel) View(width, height int) string {
	s := "\n"

	if !m.loaded {
		s += titleStyle.Render("  Model Selection") + "\n\n"
		s += dimStyle.Render("  Fetching models from Ollama...") + "\n"
		return s
	}

	if m.err != nil {
		s += titleStyle.Render("  Model Selection") + "\n\n"
		s += errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)) + "\n\n"
		s += dimStyle.Render("  Make sure Ollama is running and try again.") + "\n"
		s += dimStyle.Render("  Press q to quit.") + "\n"
		return s
	}

	if len(m.models) == 0 {
		s += titleStyle.Render("  Model Selection") + "\n\n"
		s += warnStyle.Render("  No models found in Ollama.") + "\n"
		s += dimStyle.Render("  Pull a model first: ollama pull nomic-embed-text") + "\n"
		return s
	}

	if m.page == setupPageEmbed {
		s += titleStyle.Render("  Select Embedding Model") + "\n"
		s += dimStyle.Render("  Used to generate vector embeddings for code chunks") + "\n\n"
		for i, model := range m.embedModels {
			cursor := "  "
			style := listItemStyle
			if i == m.embedCursor {
				cursor = "▸ "
				style = selectedStyle
			}
			s += fmt.Sprintf("  %s%s\n", cursor, style.Render(fmt.Sprintf("%s (%s)", model.Name, formatSize(model.Size))))
		}
		s += "\n"
		s += helpStyle.Render("  ↑/↓ navigate • Enter select") + "\n"
	} else {
		s += titleStyle.Render("  Select Chat Model") + "\n"
		s += dimStyle.Render("  Used for answering questions and generating summaries") + "\n\n"
		for i, model := range m.chatModels {
			cursor := "  "
			style := listItemStyle
			if i == m.chatCursor {
				cursor = "▸ "
				style = selectedStyle
			}
			s += fmt.Sprintf("  %s%s\n", cursor, style.Render(fmt.Sprintf("%s (%s)", model.Name, formatSize(model.Size))))
		}
		s += "\n"
		s += helpStyle.Render("  ↑/↓ navigate • Enter confirm") + "\n"
	}

	return s
}

func (m setupModel) selectedEmbedModel() string {
	if len(m.embedModels) > 0 && m.embedCursor < len(m.embedModels) {
		return m.embedModels[m.embedCursor].Name
	}
	return ""
}

func (m setupModel) selectedChatModel() string {
	if len(m.chatModels) > 0 && m.chatCursor < len(m.chatModels) {
		return m.chatModels[m.chatCursor].Name
	}
	return ""
}
