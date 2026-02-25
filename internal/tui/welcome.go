package tui

import (
	"fmt"
	"os"

	"synapse/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

type indexStatus int

const (
	indexNotFound indexStatus = iota
	indexReady
	indexStale
)

type welcomeModel struct {
	status       indexStatus
	staleReason  string
	ready        bool // true once the check has completed
}

// checkIndexMsg is sent after checking the index status.
type checkIndexMsg struct {
	status      indexStatus
	staleReason string
	err         error
}

func checkIndex(cfg Config) tea.Cmd {
	return func() tea.Msg {
		dbPath := cfg.DBPath
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return checkIndexMsg{status: indexNotFound}
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return checkIndexMsg{status: indexNotFound, err: err}
		}
		defer st.Close()

		lastModel, err := st.GetMeta("embedding_model")
		if err != nil || lastModel == "" {
			return checkIndexMsg{status: indexNotFound}
		}

		if lastModel != cfg.Model {
			return checkIndexMsg{
				status:      indexStale,
				staleReason: fmt.Sprintf("model changed: %s → %s", lastModel, cfg.Model),
			}
		}

		return checkIndexMsg{status: indexReady}
	}
}

func (m welcomeModel) Update(msg tea.Msg) (welcomeModel, tea.Cmd) {
	switch msg := msg.(type) {
	case checkIndexMsg:
		m.status = msg.status
		m.staleReason = msg.staleReason
		m.ready = true
	}
	return m, nil
}

func (m welcomeModel) View(width, height int) string {
	s := "\n"
	s += titleStyle.Render("  ◆ Synapse") + "\n"
	s += subtitleStyle.Render("  Local code intelligence powered by RAG") + "\n\n"

	if !m.ready {
		s += dimStyle.Render("  Checking index...") + "\n"
		return s
	}

	switch m.status {
	case indexReady:
		s += successStyle.Render("  ✓ Index ready") + "\n"
	case indexNotFound:
		s += warnStyle.Render("  ✗ No index found") + "\n"
	case indexStale:
		s += warnStyle.Render("  ⚠ Index stale") + "\n"
		s += dimStyle.Render("    "+m.staleReason) + "\n"
	}

	s += "\n"
	s += dimStyle.Render("  Press Enter to continue") + "\n"
	return s
}
