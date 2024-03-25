package cmd

import (
	"ShareGenerationClient/config"
	"fmt"
	"github.com/spf13/cobra"
)

// overrideCmd represents the start command
var overrideCmd = &cobra.Command{
	Use:   "override",
	Short: "Manually override current active public key",
	Long:  `Manually override current active public key`,
	Run: func(cmd *cobra.Command, args []string) {
		_, err := config.ReadConfigFromFile()
		if err != nil {
			fmt.Printf("Error loading config from file: %s\n", err.Error())
			return
		}

	},
}

func init() {
	rootCmd.AddCommand(overrideCmd)
}
