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
	"github.com/cha195/meridian/internal/api"
	"github.com/cha195/meridian/internal/cache"
	"github.com/cha195/meridian/internal/config"
	"github.com/cha195/meridian/internal/events"
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

		clusterState, err := geo.NewClusterState(cfg.Node.ID, cfg.Cluster.Nodes)
		if err != nil {
			return fmt.Errorf("failed to initialize cluster state: %w", err)
		}

		emitter := events.NewEmitter(1000, events.NewStdoutOutput())
		emitter.Start()

		handler, err := proxy.NewProxyHandler(cfg, geoLocator, clusterState, emitter)
		if err != nil {
			return fmt.Errorf("failed to create proxy handler: %w", err)
		}

		caches := make(map[string]*cache.MigratingCache)
		for _, proj := range cfg.Projects {
			maxSize := int64(proj.Cache.MaxSizeMB) * 1024 * 1024
			if maxSize == 0 {
				maxSize = 512 * 1024 * 1024
			}
			policyName := proj.Cache.Policy
			if policyName == "" {
				policyName = "sieve"
			}
			policy, pErr := cache.NewPolicy(policyName, maxSize)
			if pErr != nil {
				return fmt.Errorf("project %s: %w", proj.ID, pErr)
			}
			caches[proj.ID] = cache.NewMigratingCache(policy)
		}

		// Start cluster health checks.
		clusterState.StartHealthChecks(cfg.Cluster.HealthCheck)

		// Start API server on port 9090.
		apiServer := api.NewAPIServer(cfg, emitter, clusterState, geoLocator, caches)
		apiHTTP := apiServer.Start(":9090")

		proxyServer := &http.Server{
			Addr:    cfg.Node.Listen,
			Handler: handler,
		}

		// Handle SIGHUP: reload config and update cluster node list without restart.
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		go func() {
			for range sighup {
				newCfg, err := config.LoadConfig(configPath)
				if err != nil {
					log.Printf("config reload failed: %v", err)
					continue
				}
				if err := config.ValidateConfig(newCfg); err != nil {
					log.Printf("config reload validation failed: %v", err)
					continue
				}
				clusterState.UpdateNodes(newCfg.Cluster.Nodes)
				log.Printf("config reloaded: cluster updated with %d nodes", len(newCfg.Cluster.Nodes))
			}
		}()

		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

		go func() {
			log.Printf("Meridian edge node [%s] listening on %s", cfg.Node.ID, cfg.Node.Listen)
			if err := proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("proxy server error: %v", err)
			}
		}()

		<-stop
		log.Println("shutting down...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		clusterState.StopHealthChecks()
		if err := apiHTTP.Shutdown(ctx); err != nil {
			log.Printf("API server shutdown error: %v", err)
		}
		if err := proxyServer.Shutdown(ctx); err != nil {
			return err
		}
		emitter.Shutdown()
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
