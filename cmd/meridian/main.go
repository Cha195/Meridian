package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/cha195/meridian/internal/config"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "meridian",
	Short: "Edge CDN with pluggable cache eviction policies",
}

var serveCmd = &cobra.Command{
	Use:   "serve --config CONFIG_FILE",
	Short: "Start the Meridian edge node",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := cmd.Flags().GetString("config")
		if err != nil {
			return err
		}

		if configPath == "" {
			return fmt.Errorf("--config is required")
		}

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return err
		}

		if err := config.ValidateConfig(cfg); err != nil {
			return err
		}

		fmt.Printf("Loaded config: node=%s, projects=%d\n", cfg.Node.ID, len(cfg.Projects))
		for _, proj := range cfg.Projects {
			fmt.Printf("  - %s (origin: %s, cache policy: %s)\n", proj.ID, proj.Origin, proj.Cache.Policy)
		}

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("meridian version %s (commit: %s)\n", Version, Commit)
	},
}

func init() {
	serveCmd.Flags().StringP("config", "c", "", "Path to config file")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
