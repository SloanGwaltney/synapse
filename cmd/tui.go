package cmd

import (
	"os"
	"path/filepath"

	"synapse/internal/tui"
)

func runTUI() error {
	dbPath := flagDB
	if dbPath == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dbPath = filepath.Join(wd, ".synapse", "index.db")
	}

	return tui.Run(tui.Config{
		DBPath:    dbPath,
		OllamaURL: flagOllama,
		Model:     flagModel,
		ChatModel: flagChatModel,
	})
}
