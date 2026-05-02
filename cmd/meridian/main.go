package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/geo"
	"github.com/cha195/meridian/internal/proxy"
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

		var geoLocator *geo.GeoLocator
		if cfg.Node.GeoIPDB != "" {
			geoLocator, err = geo.NewGeoLocator(cfg.Node.GeoIPDB)
			if err != nil {
				log.Printf("warning: GeoIP unavailable: %v", err)
			} else {
				defer geoLocator.Close()
			}
		} else {
			log.Printf("warning: geoip_db not configured, geo routing disabled")
		}

		handler, err := proxy.NewProxyHandler(cfg, geoLocator)
		if err != nil {
			return fmt.Errorf("failed to create proxy handler: %w", err)
		}

		server := &http.Server{
			Addr:    cfg.Node.Listen,
			Handler: handler,
		}

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

		go func() {
			log.Printf("Meridian edge node [%s] listening on %s", cfg.Node.ID, cfg.Node.Listen)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		<-stop
		log.Println("shutting down...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return server.Shutdown(ctx)
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
