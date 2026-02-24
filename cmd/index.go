package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"synapse/internal/index"

	"github.com/spf13/cobra"
)

var (
	flagWorkers      int
	flagOverviewModel string
)

var indexCmd = &cobra.Command{
	Use:   "index <path>",
	Short: "Index a codebase for search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		// Default DB path is <project>/.synapse/index.db.
		dbPath := flagDB
		if dbPath == "" {
			dbPath = filepath.Join(root, ".synapse", "index.db")
		}

		// Ensure the database directory exists.
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return fmt.Errorf("create db directory: %w", err)
		}

		overviewModel := flagOverviewModel
		if overviewModel == "" {
			overviewModel = flagChatModel
		}

		idx, err := index.New(index.Config{
			DBPath:        dbPath,
			OllamaURL:     flagOllama,
			Model:         flagModel,
			Workers:       flagWorkers,
			OverviewModel: overviewModel,
		})
		if err != nil {
			return err
		}
		defer idx.Close()

		fmt.Printf("Indexing %s...\n", root)
		start := time.Now()

		stats, err := idx.Index(root)
		elapsed := time.Since(start)

		if stats != nil {
			fmt.Printf("\nDone in %s\n", elapsed.Round(time.Millisecond))
			fmt.Printf("  Files:   %d total, %d indexed, %d skipped\n",
				stats.FilesTotal, stats.FilesIndexed, stats.FilesSkipped)
			fmt.Printf("  Chunks:  %d\n", stats.ChunksTotal)
		}

		return err
	},
}

func init() {
	indexCmd.Flags().IntVar(&flagWorkers, "workers", runtime.NumCPU(), "parallel workers")
	indexCmd.Flags().StringVar(&flagOverviewModel, "overview-model", "", "model for overview generation (default: same as --chat-model)")
	rootCmd.AddCommand(indexCmd)
}
