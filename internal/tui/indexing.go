package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"synapse/internal/index"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

type indexingModel struct {
	spinner        spinner.Model
	phase          string
	filesProcessed int
	filesTotal     int
	done           bool
	stats          *index.Stats
	err            error
}

func newIndexingModel() indexingModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = selectedStyle
	return indexingModel{
		spinner: sp,
		phase:   "Indexing files...",
	}
}

// indexDoneMsg is sent when indexing completes.
type indexDoneMsg struct {
	stats *index.Stats
	err   error
}

// indexProgressMsg is sent periodically during indexing.
type indexProgressMsg struct {
	phase          string
	filesProcessed int
	filesTotal     int
}

func runIndex(cfg Config) tea.Cmd {
	return func() tea.Msg {
		wd, err := os.Getwd()
		if err != nil {
			return indexDoneMsg{err: err}
		}

		dbPath := cfg.DBPath
		if dbPath == "" {
			dbPath = filepath.Join(wd, ".synapse", "index.db")
		}

		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return indexDoneMsg{err: fmt.Errorf("create db directory: %w", err)}
		}

		// Redirect stdout to suppress indexer's fmt.Printf output.
		origStdout := os.Stdout
		devNull, err := os.Open(os.DevNull)
		if err == nil {
			os.Stdout = devNull
		}

		// Progress channel — the callback sends updates, we drain after indexing.
		// We use cfg.program (set by the TUI) to send messages to the tea program.
		idx, err := index.New(index.Config{
			DBPath:        dbPath,
			OllamaURL:     cfg.OllamaURL,
			Model:         cfg.Model,
			Workers:       runtime.NumCPU(),
			OverviewModel: cfg.ChatModel,
			OnProgress: func(phase string, processed, total int) {
				if cfg.program != nil && cfg.program.p != nil {
					cfg.program.p.Send(indexProgressMsg{
						phase:          phase,
						filesProcessed: processed,
						filesTotal:     total,
					})
				}
			},
		})
		if err != nil {
			os.Stdout = origStdout
			if devNull != nil {
				devNull.Close()
			}
			return indexDoneMsg{err: err}
		}

		stats, indexErr := idx.Index(wd)

		// Restore stdout.
		os.Stdout = origStdout
		if devNull != nil {
			devNull.Close()
		}

		if indexErr != nil {
			idx.Close()
			return indexDoneMsg{stats: stats, err: indexErr}
		}

		return indexDoneMsg{stats: stats}
	}
}

func (m indexingModel) Update(msg tea.Msg) (indexingModel, tea.Cmd) {
	switch msg := msg.(type) {
	case indexDoneMsg:
		m.done = true
		m.stats = msg.stats
		m.err = msg.err
		return m, nil
	case indexProgressMsg:
		m.phase = msg.phase
		m.filesProcessed = msg.filesProcessed
		m.filesTotal = msg.filesTotal
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m indexingModel) View(width, height int) string {
	s := "\n"
	s += titleStyle.Render("  Indexing") + "\n\n"

	if m.done {
		if m.err != nil {
			s += errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)) + "\n\n"
			s += dimStyle.Render("  Press Enter to continue to chat anyway, or q to quit.") + "\n"
			return s
		}
		s += successStyle.Render("  ✓ Indexing complete!") + "\n\n"
		if m.stats != nil {
			s += fmt.Sprintf("  Files: %d total, %d indexed, %d skipped\n",
				m.stats.FilesTotal, m.stats.FilesIndexed, m.stats.FilesSkipped)
			s += fmt.Sprintf("  Chunks: %d\n", m.stats.ChunksTotal)
		}
		s += "\n"
		s += dimStyle.Render("  Press Enter to start chatting") + "\n"
		return s
	}

	s += fmt.Sprintf("  %s %s\n", m.spinner.View(), m.phase)
	if m.filesTotal > 0 {
		s += fmt.Sprintf("  %d / %d files processed\n", m.filesProcessed, m.filesTotal)
	}
	s += "\n"
	s += dimStyle.Render("  This may take a while for large codebases...") + "\n"
	return s
}
