// cmd/tasseograph/main.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/signalnine/tasseograph/internal/agent"
	"github.com/signalnine/tasseograph/internal/collector"
	"github.com/signalnine/tasseograph/internal/config"
	"github.com/spf13/cobra"
)

var (
	agentConfigPath     string
	collectorConfigPath string
)

var rootCmd = &cobra.Command{
	Use:   "tasseograph",
	Short: "dmesg anomaly detection via LLM",
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the host agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadAgentConfig(agentConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		a := agent.New(cfg)

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		return a.Run(ctx)
	},
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the central collector",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadCollectorConfig(collectorConfigPath)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		srv, err := collector.NewServer(cfg)
		if err != nil {
			return err
		}

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		return srv.Run(ctx)
	},
}

func init() {
	agentCmd.Flags().StringVarP(&agentConfigPath, "config", "c", "/etc/tasseograph/agent.yaml", "path to config file")
	collectorCmd.Flags().StringVarP(&collectorConfigPath, "config", "c", "/etc/tasseograph/collector.yaml", "path to config file")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(collectorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
