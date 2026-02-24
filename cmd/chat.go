package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"synapse/internal/embedder"
	"synapse/internal/llm"
	"synapse/internal/rag"
	"synapse/internal/store"

	"github.com/spf13/cobra"
)

var flagK int

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Ask questions about your indexed codebase",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve DB path.
		dbPath := flagDB
		if dbPath == "" {
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			dbPath = filepath.Join(wd, ".synapse", "index.db")
		}
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			return fmt.Errorf("index not found at %s\nRun 'synapse index <path>' first to build the index", dbPath)
		}

		st, err := store.Open(dbPath)
		if err != nil {
			return fmt.Errorf("open index: %w", err)
		}
		defer st.Close()

		emb := embedder.NewOllamaEmbedder(flagOllama, flagModel)
		chat := llm.NewOllamaChat(flagOllama, flagChatModel)

		// Load project overview if available.
		var overview string
		overviewPath := filepath.Join(filepath.Dir(dbPath), "overview.md")
		if data, err := os.ReadFile(overviewPath); err == nil {
			overview = string(data)
		}

		var history []llm.Message
		scanner := bufio.NewScanner(os.Stdin)

		fmt.Println("synapse chat (type /help for commands, /exit to quit)")
		fmt.Println()

		for {
			fmt.Print("> ")
			if !scanner.Scan() {
				break
			}
			question := strings.TrimSpace(scanner.Text())
			if question == "" {
				continue
			}

			switch question {
			case "/exit", "/quit":
				fmt.Println("Goodbye.")
				return nil
			case "/clear":
				history = nil
				fmt.Println("Conversation cleared.")
				continue
			case "/help":
				fmt.Println("Commands:")
				fmt.Println("  /clear  - clear conversation history")
				fmt.Println("  /exit   - quit chat")
				fmt.Println("  /help   - show this help")
				continue
			}

			fmt.Println("[Searching...]")

			chunks, err := rag.HybridRetrieve(question, st, emb, flagK)
			if err != nil {
				fmt.Fprintf(os.Stderr, "retrieval error: %v\n", err)
				continue
			}

			msgs := rag.BuildMessages(chunks, history, question, overview)
			answer, err := chat.Generate(msgs)
			if err != nil {
				fmt.Fprintf(os.Stderr, "llm error: %v\n", err)
				continue
			}

			fmt.Println()
			fmt.Println(answer)
			fmt.Println()

			// Keep last 10 turns of history.
			history = append(history, llm.Message{Role: "user", Content: question})
			history = append(history, llm.Message{Role: "assistant", Content: answer})
			if len(history) > 20 {
				history = history[len(history)-20:]
			}
		}

		if err := scanner.Err(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	chatCmd.Flags().IntVar(&flagK, "k", 10, "number of chunks to retrieve per question")
	rootCmd.AddCommand(chatCmd)
}
