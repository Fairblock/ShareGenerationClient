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

	checkInterval := cfg.CheckInterval

	cClient, err := cosmosClient.NewCosmosClient(cosmosClient.ClientConfig{
		GRPCEndpoint:  cfg.GetGRPCEndpoint(),
		PrivateKeyHex: cfg.PrivateKey,
		ChainID:       cfg.FairyRingNode.ChainID,
		UseTLS:        cfg.FairyRingNode.GRPCTLS,
		GasPrice:      cfg.FairyRingNode.GasPrice,
		FeeDenom:      cfg.FairyRingNode.Denom,
	})
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
	if err != nil {
		log.Fatalf("Couldn't create tendermint rpc client: %s", err.Error())
	}

	if err = client.Start(); err != nil {
		log.Fatal(err)
	}

	out, err := client.Subscribe(context.Background(), "share-generation-client", "tm.event = 'NewBlockHeader'", 256)
	if err != nil {
		log.Fatal(err)
	}

	defer client.Stop()
	var blockPassed uint64 = math.MaxUint64

	log.Printf("Client Started, checking pub key status every %d block...\n", checkInterval)

	http.Handle("/metrics", promhttp.Handler())
	log.Printf("MetricsPort: %d\n", cfg.MetricsPort)
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.MetricsPort), nil); err != nil {
			log.Printf("Metrics server stopped: %s", err.Error())
		}
	}()

	for {
		select {
		case result, ok := <-out:
			if !ok {
				log.Fatal("Block subscription closed, exiting so the process can be restarted")
			}

			newBlockHeader, ok := result.Data.(tmtypes.EventDataNewBlockHeader)
			if !ok {
				continue
			}

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
				log.Printf("Error while querying pub key: %s", err.Error())
				break
			}

			if res == nil || (len(res.QueuedPubkey.PublicKey) == 0 && len(res.QueuedPubkey.Creator) == 0) {
				log.Println("Queued Pub Key Not found, sending setup request...")
				validatorsPubInfos, err := masterClient.CosmosClient.GetAllValidatorsPubInfos()
				if err != nil {
					log.Printf("Error getting all validators public infos: %s", err.Error())
					break
				}
				generatedResult := masterClient.Generate(validatorsPubInfos)
				if generatedResult == nil {
					log.Println("Generate result is empty")
					break
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
					log.Printf("Failed to submit latest pubkey, validate basic failed: %s", err.Error())
					break
				}

				if err = masterClient.CosmosClient.UpdateClientAccountInfo(); err != nil {
					log.Printf("Unable to update client account info: %s", err.Error())
					break
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

				finalTxResp, err := masterClient.CosmosClient.WaitForTx(txResp.TxHash, time.Second, 60*time.Second)
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
