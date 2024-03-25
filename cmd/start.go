package cmd

import (
	"ShareGenerationClient/config"
	"ShareGenerationClient/internal"
	"fmt"
	"github.com/spf13/cobra"
)

// startCmd represents the start command
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the client",
	Long:  `Start the client`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.ReadConfigFromFile()
		if err != nil {
			fmt.Printf("Error loading config from file: %s\n", err.Error())
			return
		}
		internal.ShareGenerationClient(cfg)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
