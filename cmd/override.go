package cmd

import (
	"ShareGenerationClient/config"
	"ShareGenerationClient/internal"
	"ShareGenerationClient/pkg/cosmosClient"
	"encoding/hex"
	"fmt"
	"github.com/Fairblock/fairyring/x/keyshare/types"
	"github.com/spf13/cobra"
	"log"
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// overrideCmd represents the start command
var overrideCmd = &cobra.Command{
	Use:   "override",
	Short: "Manually override current active public key",
	Long:  `Manually override current active public key`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.ReadConfigFromFile()
		if err != nil {
			fmt.Printf("Error loading config from file: %s\n", err.Error())
			return
		}

		privateKey := cfg.PrivateKey
		gRPCEndpoint := cfg.GetGRPCEndpoint()

		cClient, err := cosmosClient.NewCosmosClient(gRPCEndpoint, privateKey, cfg.FairyRingNode.ChainID)
		if err != nil {
			log.Fatalf("Couldn't create cosmos client: %s", err.Error())
		}

		masterClient := internal.ShareGeneratorClient{
			CosmosClient: cClient,
		}

		validatorsInfo, err := cClient.GetAllValidatorsPubInfos()
		if err != nil {
			log.Fatalf("Couldn't get validators info: %s", err.Error())
		}

		if len(validatorsInfo) <= 0 {
			log.Fatalln("No validators found in key share module.")
		}

		fmt.Printf("Found total %d validators in key share module\n", len(validatorsInfo))

		for i, v := range validatorsInfo {
			if err != nil {
				log.Fatalf("Couldn't get validator info from staking module: %s", err.Error())
			}
			fmt.Printf("[%d] '%s': %s\n", i, v.Description.Moniker, v.Address)
		}

		var validatorsIndexesStr string
		fmt.Print("Enter the index of the validators to be removed, separate with comma: ")
		_, err = fmt.Scan(&validatorsIndexesStr)

		splitIndexes := strings.Split(validatorsIndexesStr, ",")
		if len(splitIndexes) <= 0 || len(splitIndexes) >= len(validatorsInfo) {
			log.Fatalln("Invalid number of given validators")
		}

		indexesToRemove := make([]int, 0)
		for _, v := range splitIndexes {
			number, err := strconv.Atoi(v)
			if err != nil {
				log.Fatalf("Invalid given index, expected number, got: '%s': %s", v, err.Error())
			}
			indexesToRemove = append(indexesToRemove, number)
		}

		fmt.Println("Validator(s) to be removed:")
		var newValidatorInfo []cosmosClient.ValidatorPubInfo
		for i, v := range validatorsInfo {
			if slices.Contains(indexesToRemove, i) {
				fmt.Printf("[%d] '%s': %s\n", i, v.Description.Moniker, v.Address)
			} else {
				newValidatorInfo = append(newValidatorInfo, v)
			}
		}

		generatedResult := masterClient.Generate(newValidatorInfo)
		if generatedResult == nil {
			log.Fatal("Generate result is empty")
		}

		n := len(generatedResult.EncryptedKeyShares)

		encShares := make([]*types.EncryptedKeyShare, n)

		for _, v := range generatedResult.EncryptedKeyShares {
			indexByte, _ := hex.DecodeString(v.Index.String())
			indexInt := big.NewInt(0).SetBytes(indexByte).Uint64()
			encShares[indexInt-1] = &types.EncryptedKeyShare{
				Data:      v.EncShare,
				Validator: v.ValidatorAddress,
			}
		}

		txMsg := types.MsgOverrideLatestPubKey{
			Creator:            masterClient.CosmosClient.GetAddress(),
			PublicKey:          generatedResult.MasterPublicKey,
			Commitments:        generatedResult.Commitments,
			NumberOfValidators: uint64(n),
			EncryptedKeyShares: encShares,
		}

		err = txMsg.ValidateBasic()
		if err != nil {
			log.Fatalf("Failed to override latest pubkey, validate basic failed: %s", err.Error())
		}

		txResp, err := masterClient.CosmosClient.BroadcastTx(
			&txMsg,
			true,
		)

		if err != nil {
			log.Printf("Error broadcasting tx: %s", err.Error())
		} else {
			log.Printf("Tx Broadcasted: %s", txResp.TxHash)
		}
	},
}

func init() {
	rootCmd.AddCommand(overrideCmd)
}
