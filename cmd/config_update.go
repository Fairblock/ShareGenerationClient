package cmd

import (
	"ShareGenerationClient/config"
	"fmt"
	"github.com/spf13/cobra"
)

// configUpdateCmd represents the config update command
var configUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update ShareGenerationClient config file",
	Long:  `Update ShareGenerationClient config file`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.ReadConfigFromFile()
		if err != nil {
			fmt.Printf("Error loading config from file: %s\n", err.Error())
			return
		}

		chainID, _ := cmd.Flags().GetString("chain-id")
		chainDenom, _ := cmd.Flags().GetString("denom")
		chainIP, _ := cmd.Flags().GetString("ip")
		chainProtocol, _ := cmd.Flags().GetString("protocol")
		chainGrpcPort, _ := cmd.Flags().GetUint64("grpc-port")
		chainPort, _ := cmd.Flags().GetUint64("port")
		checkInterval, _ := cmd.Flags().GetUint64("check-interval")
		privateKey, _ := cmd.Flags().GetString("private-key")
		metricsPort, _ := cmd.Flags().GetUint64("metrics-port")

		cfg.FairyRingNode = config.Node{
			Protocol: chainProtocol,
			IP:       chainIP,
			Port:     chainPort,
			GRPCPort: chainGrpcPort,
			Denom:    chainDenom,
			ChainID:  chainID,
		}

		cfg.CheckInterval = checkInterval
		cfg.PrivateKey = privateKey
		cfg.MetricsPort = metricsPort

		if err = cfg.SaveConfig(); err != nil {
			fmt.Printf("Error saving updated config to system: %s\n", err.Error())
			return
		}

		fmt.Println("Successfully Updated config!")
	},
}

func init() {
	cfg, err := config.ReadConfigFromFile()
	if err != nil {
		fmt.Printf("Error loading config from file: %s\n", err.Error())
		return
	}

	configUpdateCmd.Flags().String("chain-id", cfg.FairyRingNode.ChainID, "Update config chain id")
	configUpdateCmd.Flags().String("denom", cfg.FairyRingNode.Denom, "Update config denom")
	configUpdateCmd.Flags().Uint64("grpc-port", cfg.FairyRingNode.GRPCPort, "Update config grpc-port")
	configUpdateCmd.Flags().String("ip", cfg.FairyRingNode.IP, "Update config node ip address")
	configUpdateCmd.Flags().Uint64("port", cfg.FairyRingNode.Port, "Update config node port")
	configUpdateCmd.Flags().String("protocol", cfg.FairyRingNode.Protocol, "Update config node protocol")
	configUpdateCmd.Flags().Uint64("check-interval", cfg.CheckInterval, "How often the client check for pub key status in blocks")
	configUpdateCmd.Flags().String("private-key", cfg.PrivateKey, "Private key for the trusted address")
	configUpdateCmd.Flags().Uint64("metrics-port", cfg.MetricsPort, "Update config metrics port")
}
