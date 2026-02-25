package tui

import (
	"os"
	"path/filepath"

	"synapse/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// ViewState represents which screen is active.
type ViewState int

const (
	ViewWelcome ViewState = iota
	ViewSetup
	ViewIndexing
	ViewChat
)

// programRef is an indirect pointer to the tea.Program so background goroutines
// can send messages. It must be set after tea.NewProgram returns but before Run.
type programRef struct {
	p *tea.Program
}

// Config holds configuration passed from the CLI layer.
type Config struct {
	DBPath    string
	OllamaURL string
	Model     string
	ChatModel string

	// program is set internally so background goroutines can send messages.
	program *programRef
}

// Model is the top-level Bubble Tea model.
type Model struct {
	state  ViewState
	config Config
	width  int
	height int

	welcome  welcomeModel
	setup    setupModel
	indexing indexingModel
	chat     chatModel
	err      error
}

// New creates a new TUI model with the given config.
func New(cfg Config) Model {
	return Model{
		state:  ViewWelcome,
		config: cfg,
	}
}

func (m Model) Init() tea.Cmd {
	return checkIndex(m.config)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.state == ViewChat {
			var c tea.Cmd
			m.chat, c = m.chat.Update(msg)
			return m, c
		}
		return m, nil

	case tea.KeyMsg:
		// Global quit.
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.state != ViewChat {
				return m, tea.Quit
			}
		}
	}

	var cmd tea.Cmd

	switch m.state {
	case ViewWelcome:
		m.welcome, cmd = m.welcome.Update(msg)
		if cmd != nil {
			return m, cmd
		}
		// Handle Enter to transition.
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter && m.welcome.ready {
			if m.welcome.status == indexReady {
				return m, m.transitionToChat()
			}
			// Need indexing — go to setup.
			m.state = ViewSetup
			return m, fetchModels(m.config.OllamaURL)
		}

	case ViewSetup:
		m.setup, cmd = m.setup.Update(msg, m.config)
		if cmd != nil {
			return m, cmd
		}
		// Handle Enter.
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter && m.setup.loaded && m.setup.err == nil && len(m.setup.models) > 0 {
			// If on embed page, advance to chat page.
			if m.setup.advancePage() {
				return m, nil
			}
			// On chat page — apply selections and start indexing.
			if sel := m.setup.selectedEmbedModel(); sel != "" {
				m.config.Model = sel
			}
			if sel := m.setup.selectedChatModel(); sel != "" {
				m.config.ChatModel = sel
			}
			if m.config.DBPath == "" {
				wd, err := os.Getwd()
				if err == nil {
					m.config.DBPath = filepath.Join(wd, ".synapse", "index.db")
				}
			}
			m.state = ViewIndexing
			m.indexing = newIndexingModel()
			return m, tea.Batch(m.indexing.spinner.Tick, runIndex(m.config))
		}

	case ViewIndexing:
		m.indexing, cmd = m.indexing.Update(msg)
		if cmd != nil {
			return m, cmd
		}
		// Handle Enter after indexing completes.
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter && m.indexing.done {
			return m, m.transitionToChat()
		}

	case ViewChat:
		m.chat, cmd = m.chat.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) transitionToChat() tea.Cmd {
	dbPath := m.config.DBPath
	if dbPath == "" {
		wd, _ := os.Getwd()
		dbPath = filepath.Join(wd, ".synapse", "index.db")
	}

	st, err := store.Open(dbPath)
	if err != nil {
		m.err = err
		return nil
	}

	// Load overview.
	var overview string
	overviewPath := filepath.Join(filepath.Dir(dbPath), "overview.md")
	if data, err := os.ReadFile(overviewPath); err == nil {
		overview = string(data)
	}

	m.chat = newChatModel(st, m.config.OllamaURL, m.config.Model, m.config.ChatModel, overview, 10)
	m.chat.initViewport(m.width, m.height)
	m.state = ViewChat

	return nil
}

func (m Model) View() string {
	if m.err != nil {
		return errorStyle.Render("Error: "+m.err.Error()) + "\n"
	}

	switch m.state {
	case ViewWelcome:
		return m.welcome.View(m.width, m.height)
	case ViewSetup:
		return m.setup.View(m.width, m.height)
	case ViewIndexing:
		return m.indexing.View(m.width, m.height)
	case ViewChat:
		return m.chat.View(m.width, m.height)
	}
	return ""
}

// Run starts the TUI program.
func Run(cfg Config) error {
	ref := &programRef{}
	cfg.program = ref
	model := New(cfg)
	p := tea.NewProgram(model, tea.WithAltScreen())
	ref.p = p
	_, err := p.Run()
	return err
}

