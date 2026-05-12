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
	sendSummaryNow      bool
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

		// --send-summary-now: build and email one digest covering the
		// configured window, then exit. Lets operators validate SMTP
		// without waiting for the next scheduled tick.
		if sendSummaryNow {
			db, err := collector.NewDB(cfg.DBPath)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer db.Close()
			subj, err := collector.SendSummaryOnce(cfg, db)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "summary sent: %s\n", subj)
			return nil
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
	collectorCmd.Flags().BoolVar(&sendSummaryNow, "send-summary-now", false, "build and email one digest covering summary_interval, then exit")

	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(collectorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
