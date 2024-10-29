package internal

import (
	"ShareGenerationClient/config"
	"ShareGenerationClient/pkg/cosmosClient"
	"context"
	"encoding/hex"
	"fmt"
	"github.com/Fairblock/fairyring/x/keyshare/types"
	tmclient "github.com/cometbft/cometbft/rpc/client/http"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"math"
	"math/big"
	"net/http"
	"strings"
	"time"
)

var (
	failedShareGenerated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sharegenerationclient_failed_share_generated",
		Help: "The total number of invalid key share generated",
	})

	validShareGenerated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sharegenerationclient_valid_share_generated",
		Help: "The total number of valid key share generated",
	})
)

func ShareGenerationClient(cfg *config.Config) {

	privateKey := cfg.PrivateKey
	gRPCEndpoint := cfg.GetGRPCEndpoint()
	checkInterval := cfg.CheckInterval

	cClient, err := cosmosClient.NewCosmosClient(gRPCEndpoint, privateKey, cfg.FairyRingNode.ChainID)
	if err != nil {
		log.Fatalf("Couldn't create cosmos client: %s", err.Error())
	}

	masterClient := ShareGeneratorClient{
		CosmosClient: cClient,
	}

	client, err := tmclient.New(
		cfg.GetFairyRingNodeURI(),
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

	http.Handle("/metrics", promhttp.Handler())
	log.Printf("MetricsPort: %d\n", cfg.MetricsPort)
	go http.ListenAndServe(fmt.Sprintf(":%d", cfg.MetricsPort), nil)

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

			if res == nil || (len(res.QueuedPubkey.PublicKey) == 0 && len(res.QueuedPubkey.Creator) == 0) {
				log.Println("Queued Pub Key Not found, sending setup request...")
				validatorsPubInfos, err := masterClient.CosmosClient.GetAllValidatorsPubInfos()
				if err != nil {
					log.Fatalf("error getting all validators public infos: %s\n", err.Error())
				}
				generatedResult := masterClient.Generate(validatorsPubInfos)
				if generatedResult == nil {
					log.Fatal("Generate result is empty")
				}

				n := len(generatedResult.EncryptedKeyShares)

				encShares := make([]*types.EncryptedKeyshare, n)

				for _, v := range generatedResult.EncryptedKeyShares {
					indexByte, _ := hex.DecodeString(v.Index.String())
					indexInt := big.NewInt(0).SetBytes(indexByte).Uint64()
					encShares[indexInt-1] = &types.EncryptedKeyshare{
						Data:      v.EncShare,
						Validator: v.ValidatorAddress,
					}
				}

				txMsg := types.MsgCreateLatestPubkey{
					Creator:            masterClient.CosmosClient.GetAddress(),
					PublicKey:          generatedResult.MasterPublicKey,
					Commitments:        generatedResult.Commitments,
					NumberOfValidators: uint64(n),
					EncryptedKeyshares: encShares,
				}

				if err = txMsg.ValidateBasic(); err != nil {
					log.Fatalf("Failed to submit latest pubkey, validate basic failed: %s", err.Error())
				}

				if err = masterClient.CosmosClient.UpdateClientAccountInfo(); err != nil {
					log.Printf("Unable to update client account info: %s", err.Error())
				}

				txResp, err := masterClient.CosmosClient.BroadcastTx(
					&txMsg,
					true,
				)
				if err != nil {
					log.Printf("Error broadcasting tx: %s", err.Error())
					failedShareGenerated.Inc()
					break
				} else {
					log.Printf("Tx Broadcasted: %s", txResp.TxHash)
				}

				finalTxResp, err := masterClient.CosmosClient.WaitForTx(txResp.TxHash, time.Second)
				if err != nil {
					log.Printf("Create latest pubkey tx failed: %s\n", err.Error())
					break
				}

				if finalTxResp.TxResponse.Code != 0 {
					log.Printf("Create latest pubkey tx failed: %s\n", finalTxResp.TxResponse.RawLog)
					failedShareGenerated.Inc()
					break
				}
				validShareGenerated.Inc()
			} else {
				log.Println("Pub Keys Found !")
				log.Printf("Active Pub Key: %s | Expries at: %d\n", res.ActivePubkey.PublicKey, res.ActivePubkey.Expiry)
				log.Printf("Queued Pub Key: %s | Expries at: %d\n", res.QueuedPubkey.PublicKey, res.QueuedPubkey.Expiry)
			}
		}
	}
}
