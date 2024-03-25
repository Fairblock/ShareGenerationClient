package cmd

import (
	"ShareGenerationClient/config"
	"github.com/spf13/cobra"
)

// configInitCmd represents the config init command
var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default config file for ShareGenerationClient",
	Long:  `Create default config & keys folder in your Home directory`,
	Run: func(cmd *cobra.Command, args []string) {

		cfg := config.DefaultConfig()
		cfg.ExportConfig()
	},
}
