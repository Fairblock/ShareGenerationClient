package main

import (
	"ShareGenerationClient/cosmosClient"
	"context"
	"encoding/hex"
	"fmt"
	"github.com/Fairblock/fairyring/x/keyshare/types"
	tmclient "github.com/cometbft/cometbft/rpc/client/http"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/joho/godotenv"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
)

const DefaultChainID = "fairyring_devnet"

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	privateKey := os.Getenv("PRIVATE_KEY")
	gRPCEndpoint := fmt.Sprintf(
		"%s:%s",
		os.Getenv("GRPC_IP_ADDRESS"),
		os.Getenv("GRPC_PORT"),
	)
	checkIntervalStr := os.Getenv("CHECK_INTERVAL")

	checkInterval, err := strconv.ParseUint(checkIntervalStr, 10, 64)
	if err != nil {
		log.Fatalf("Invalid CHECK_INTERVAL in .env: %s", err.Error())
	}

	cClient, err := cosmosClient.NewCosmosClient(gRPCEndpoint, privateKey, DefaultChainID)
	if err != nil {
		log.Fatalf("Couldn't create cosmos client: %s", err.Error())
	}

	masterClient := ShareGeneratorClient{
		CosmosClient: cClient,
	}

	client, err := tmclient.New(
		fmt.Sprintf(
			"%s:%s",
			os.Getenv("NODE_IP_ADDRESS"),
			os.Getenv("NODE_PORT"),
		),
		"/websocket",
	)

	if err = client.Start(); err != nil {
		log.Fatal(err)
	}

	out, err := client.Subscribe(context.Background(), "", "tm.event = 'NewBlockHeader'")
	if err != nil {
		log.Fatal(err)
	}

	defer client.Stop()
	var blockPassed uint64 = math.MaxUint64

	log.Printf("Client Started, checking pub key status every %d block...\n", checkInterval)

	for {
		select {
		case result := <-out:
			newBlockHeader := result.Data.(tmtypes.EventDataNewBlockHeader)

			height := newBlockHeader.Header.Height

			if blockPassed != math.MaxUint64 {
				blockPassed++
				if blockPassed < checkInterval {
					continue
				}
				blockPassed = 0
			} else {
				blockPassed = 0
			}

			fmt.Println("")
			log.Printf("Latest Block Height: %d | Checking Pub Key status...\n", height)

			res, err := masterClient.CosmosClient.GetActivePubKey()
			if err != nil && !strings.Contains(err.Error(), "Active Public Key does not exists") {
				log.Fatal("Error while querying pub key:", err)
			}

			if res == nil || (len(res.QueuedPubKey.PublicKey) == 0 && len(res.QueuedPubKey.Creator) == 0) {
				log.Println("Queued Pub Key Not found, sending setup request...")
				generatedResult := masterClient.Generate()
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

				txMsg := types.MsgCreateLatestPubKey{
					Creator:            masterClient.CosmosClient.GetAddress(),
					PublicKey:          generatedResult.MasterPublicKey,
					Commitments:        generatedResult.Commitments,
					NumberOfValidators: uint64(n),
					EncryptedKeyShares: encShares,
				}

				err = txMsg.ValidateBasic()
				if err != nil {
					log.Fatalf("Failed to submit latest pubkey, validate basic failed: %s", err.Error())
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
			} else {
				log.Println("Pub Keys Found !")
				log.Printf("Active Pub Key: %s | Expries at: %d\n", res.ActivePubKey.PublicKey, res.ActivePubKey.Expiry)
				log.Printf("Queued Pub Key: %s | Expries at: %d\n", res.QueuedPubKey.PublicKey, res.QueuedPubKey.Expiry)
			}
		}
	}
}
