// cmd/tasseograph/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tasseograph",
	Short: "dmesg anomaly detection via LLM",
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Run the host agent",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("agent not implemented")
	},
}

var collectorCmd = &cobra.Command{
	Use:   "collector",
	Short: "Run the central collector",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("collector not implemented")
	},
}

func init() {
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(collectorCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
