package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagDB        string
	flagOllama    string
	flagModel     string
	flagChatModel string
)

var rootCmd = &cobra.Command{
	Use:   "synapse",
	Short: "Local code intelligence powered by RAG",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagDB, "db", "", "database path (default <project>/.synapse/index.db)")
	rootCmd.PersistentFlags().StringVar(&flagOllama, "ollama", "http://localhost:11434", "ollama base URL")
	rootCmd.PersistentFlags().StringVar(&flagModel, "model", "nomic-embed-text", "embedding model")
	rootCmd.PersistentFlags().StringVar(&flagChatModel, "chat-model", "qwen3:8b", "generative model for chat")
}
