package cmd

import (
	"ShareGenerationClient/config"
	"fmt"
	"github.com/spf13/cobra"
)

// configShowCmd represents the config show command
var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the current config",
	Long:  `Show the current config`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.ReadConfigFromFile()
		if err != nil {
			fmt.Printf("Error loading config from file: %s\n", err.Error())
			return
		}

		showPKey, _ := cmd.Flags().GetBool("show-private-key")

		fmt.Printf(`GRPC Endpoint: %s
GRPC TLS: %t
FairyRing Node Endpoint: %s
Chain ID: %s
Chain Denom: %s
Gas Price: %g
CheckInterval: %d
MetricsPort: %d
`, cfg.GetGRPCEndpoint(), cfg.FairyRingNode.GRPCTLS, cfg.GetFairyRingNodeURI(), cfg.FairyRingNode.ChainID, cfg.FairyRingNode.Denom, cfg.FairyRingNode.GasPrice, cfg.CheckInterval, cfg.MetricsPort)

		if showPKey {
			fmt.Printf("Private Key: %s\n", cfg.PrivateKey)
		}
	},
}

func init() {
	configShowCmd.Flags().Bool("show-private-key", false, "Show private key")
}
