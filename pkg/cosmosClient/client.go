package cosmosClient

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"log"
	stdmath "math"
	"strings"
	"time"

	stakingv1beta1 "cosmossdk.io/api/cosmos/staking/v1beta1"
	"cosmossdk.io/math"
	"github.com/Fairblock/fairyring/api/fairyring/keyshare"
	"github.com/Fairblock/fairyring/x/pep/types"
	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cosmostypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	dcrdSecp256k1 "github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/pkg/errors"
	"github.com/skip-mev/block-sdk/v2/testutils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultGasAdjustment = 1.5
	defaultGasLimit      = 300000
	maxSequenceRetries   = 2
)

type ClientConfig struct {
	GRPCEndpoint  string
	PrivateKeyHex string
	ChainID       string
	UseTLS        bool
	GasPrice      float64
	FeeDenom      string
}

type CosmosClient struct {
	authClient          authtypes.QueryClient
	txClient            tx.ServiceClient
	grpcConn            *grpc.ClientConn
	stakingQueryClient  stakingv1beta1.QueryClient
	bankQueryClient     banktypes.QueryClient
	keyshareQueryClient keyshare.QueryClient
	pepQueryClient      types.QueryClient
	privateKey          secp256k1.PrivKey
	publicKey           cryptotypes.PubKey
	account             authtypes.BaseAccount
	accAddress          cosmostypes.AccAddress
	chainID             string
	gasPrice            float64
	feeDenom            string
}

type ValidatorPubInfo struct {
	PublicKey    *dcrdSecp256k1.PublicKey
	Description  *stakingv1beta1.Description
	AuthorizedBy string
	Authorizing  string
	Address      string
}

func (c *CosmosClient) GetValidatorDescription(val string) (*stakingv1beta1.Description, error) {
	resp, err := c.stakingQueryClient.Validator(
		context.Background(),
		&stakingv1beta1.QueryValidatorRequest{ValidatorAddr: val},
	)
	if err != nil {
		return nil, err
	}
	return resp.Validator.Description, nil
}

func (c *CosmosClient) GetAuthorizedAddrMap(keyIsValidator bool) (map[string]string, error) {
	authorizedAddrMap := make(map[string]string)
	allAuthorizedAddr, err := c.keyshareQueryClient.AuthorizedAddressAll(
		context.Background(),
		&keyshare.QueryAuthorizedAddressAllRequest{},
	)
	if err != nil {
		return nil, err
	}

	for _, v := range allAuthorizedAddr.GetAuthorizedAddress() {
		if !v.IsAuthorized {
			continue
		}
		if keyIsValidator {
			authorizedAddrMap[v.AuthorizedBy] = v.Target
		} else {
			authorizedAddrMap[v.Target] = v.AuthorizedBy
		}
	}

	return authorizedAddrMap, nil
}

func (c *CosmosClient) GetCurrentPubKeyValidatorsInfo() ([]ValidatorPubInfo, error) {
	pubKeyResp, err := c.keyshareQueryClient.Pubkey(context.Background(), &keyshare.QueryPubkeyRequest{})
	if err != nil {
		return nil, err
	}

	if pubKeyResp.ActivePubkey == nil {
		return []ValidatorPubInfo{}, nil
	}

	if len(pubKeyResp.ActivePubkey.EncryptedKeyshares) == 0 {
		return []ValidatorPubInfo{}, nil
	}

	authAddrMap, err := c.GetAuthorizedAddrMap(false)
	if err != nil {
		return nil, errors.Wrap(err, "error when getting all authorized addresses")
	}

	validatorPubKeys := make([]ValidatorPubInfo, 0)

	for _, eks := range pubKeyResp.ActivePubkey.EncryptedKeyshares {
		// .Validator here is validator / the authorized address
		// If found, then it is an authorized address
		// Not found, then it is validator
		authorizedBy, found := authAddrMap[eks.Validator]

		var targetAddr string
		if found {
			targetAddr = authorizedBy
		} else {
			targetAddr = eks.Validator
		}

		resp, err := c.authClient.Account(
			context.Background(),
			&authtypes.QueryAccountRequest{Address: targetAddr},
		)
		if err != nil {
			return nil, errors.Wrap(err, "error when querying account info")
		}

		var baseAccount authtypes.BaseAccount

		if err = baseAccount.Unmarshal(resp.Account.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling base account")
		}

		if baseAccount.PubKey == nil {
			log.Printf("Skip Validator: %s due to pubkey not found\n", targetAddr)
			continue
		}

		var secp256k1PubKey secp256k1.PubKey
		if err = secp256k1PubKey.Unmarshal(baseAccount.PubKey.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling pub key")
		}
		pubKey, err := dcrdSecp256k1.ParsePubKey(secp256k1PubKey.Key)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing pub key to dcrd pub key")
		}

		validatorDescription, err := c.GetValidatorDescription(cosmostypes.ValAddress(secp256k1PubKey.Address()).String())
		if err != nil {
			log.Printf("error getting validator description: %s\n", err)
			continue
		}
		info := ValidatorPubInfo{
			PublicKey:   pubKey,
			Address:     baseAccount.Address,
			Description: validatorDescription,
		}

		if found {
			info.Authorizing = eks.Validator
		}

		validatorPubKeys = append(validatorPubKeys, info)
	}

	return validatorPubKeys, nil
}

func (c *CosmosClient) GetAllValidatorsPubInfos() ([]ValidatorPubInfo, error) {
	validatorsResp, err := c.keyshareQueryClient.ValidatorSetAll(
		context.Background(),
		&keyshare.QueryValidatorSetAllRequest{},
	)

	if err != nil {
		return nil, errors.Wrap(err, "error when getting validator set in keyshare module")
	}

	if len(validatorsResp.ValidatorSet) == 0 {
		return nil, errors.New("validator set in key share module is empty")
	}

	validatorPubKeys := make([]ValidatorPubInfo, 0)

	authAddrMap, err := c.GetAuthorizedAddrMap(true)
	if err != nil {
		return nil, errors.Wrap(err, "error when getting all authorized addresses")
	}

	for _, addr := range validatorsResp.ValidatorSet {
		targetAddr := addr.Validator

		authorizedTo, found := authAddrMap[targetAddr]
		if found {
			targetAddr = authorizedTo
		}

		resp, err := c.authClient.Account(
			context.Background(),
			&authtypes.QueryAccountRequest{Address: targetAddr},
		)
		if err != nil {
			return nil, errors.Wrap(err, "error when querying account info")
		}

		var baseAccount authtypes.BaseAccount

		if err = baseAccount.Unmarshal(resp.Account.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling base account")
		}

		if baseAccount.PubKey == nil {
			log.Printf("Skip Validator: %s due to pubkey not found\n", targetAddr)
			continue
		}

		var secp256k1PubKey secp256k1.PubKey
		if err = secp256k1PubKey.Unmarshal(baseAccount.PubKey.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling pub key")
		}
		pubKey, err := dcrdSecp256k1.ParsePubKey(secp256k1PubKey.Key)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing pub key to dcrd pub key")
		}

		if !found {
			validatorDescription, err := c.GetValidatorDescription(cosmostypes.ValAddress(secp256k1PubKey.Address()).String())
			if err != nil {
				return nil, errors.Wrap(err, "error getting validator description")
			}
			validatorPubKeys = append(validatorPubKeys, ValidatorPubInfo{
				PublicKey:   pubKey,
				Address:     baseAccount.Address,
				Description: validatorDescription,
			})
		} else {
			validatorPubKeys = append(validatorPubKeys, ValidatorPubInfo{
				PublicKey:    pubKey,
				Address:      baseAccount.Address,
				Description:  nil,
				AuthorizedBy: addr.Validator,
			})
		}
	}
	return validatorPubKeys, nil
}

func NewCosmosClient(cfg ClientConfig) (*CosmosClient, error) {
	var transportCreds credentials.TransportCredentials
	if cfg.UseTLS {
		transportCreds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	} else {
		transportCreds = insecure.NewCredentials()
	}

	grpcConn, err := grpc.NewClient(
		cfg.GRPCEndpoint,
		grpc.WithTransportCredentials(transportCreds),
	)
	if err != nil {
		return nil, err
	}

	authClient := authtypes.NewQueryClient(grpcConn)
	bankClient := banktypes.NewQueryClient(grpcConn)
	pepeClient := types.NewQueryClient(grpcConn)
	keyshareClient := keyshare.NewQueryClient(grpcConn)
	stakingQueryClient := stakingv1beta1.NewQueryClient(grpcConn)

	keyBytes, err := hex.DecodeString(cfg.PrivateKeyHex)
	if err != nil {
		return nil, err
	}

	privateKey := secp256k1.PrivKey{Key: keyBytes}
	pubKey := privateKey.PubKey()
	address := pubKey.Address()

	bech32Cfg := cosmostypes.GetConfig()
	bech32Cfg.SetBech32PrefixForAccount("fairy", "fairypub")
	bech32Cfg.SetBech32PrefixForValidator("fairyvaloper", "fairyvaloperpub")
	bech32Cfg.SetBech32PrefixForConsensusNode("fairyvalcons", "fairyrvalconspub")

	accAddr := cosmostypes.AccAddress(address)
	addr := accAddr.String()

	var baseAccount authtypes.BaseAccount

	resp, err := authClient.Account(
		context.Background(),
		&authtypes.QueryAccountRequest{Address: addr},
	)

	if err != nil {
		log.Println(cosmostypes.AccAddress(address).String())
		return nil, err
	}

	err = baseAccount.Unmarshal(resp.Account.Value)
	if err != nil {
		return nil, err
	}

	return &CosmosClient{
		bankQueryClient:     bankClient,
		authClient:          authClient,
		txClient:            tx.NewServiceClient(grpcConn),
		pepQueryClient:      pepeClient,
		keyshareQueryClient: keyshareClient,
		stakingQueryClient:  stakingQueryClient,
		grpcConn:            grpcConn,
		privateKey:          privateKey,
		account:             baseAccount,
		accAddress:          accAddr,
		publicKey:           pubKey,
		chainID:             cfg.ChainID,
		gasPrice:            cfg.GasPrice,
		feeDenom:            cfg.FeeDenom,
	}, nil
}

func (c *CosmosClient) UpdateClientAccountInfo() error {
	var baseAccount authtypes.BaseAccount

	resp, err := c.authClient.Account(
		context.Background(),
		&authtypes.QueryAccountRequest{Address: c.GetAddress()},
	)

	if err != nil {
		return err
	}

	err = baseAccount.Unmarshal(resp.Account.Value)
	if err != nil {
		return err
	}

	c.account = baseAccount

	return nil
}

func (c *CosmosClient) GetActivePubKey() (*types.QueryPubkeyResponse, error) {
	resp, err := c.pepQueryClient.Pubkey(
		context.Background(),
		&types.QueryPubkeyRequest{},
	)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *CosmosClient) GetLatestHeight() (uint64, error) {
	resp, err := c.pepQueryClient.LatestHeight(
		context.Background(),
		&types.QueryLatestHeightRequest{},
	)
	if err != nil {
		return 0, err
	}
	return resp.Height, nil
}

func (c *CosmosClient) GetBalance(denom string) (*math.Int, error) {
	resp, err := c.bankQueryClient.Balance(
		context.Background(),
		&banktypes.QueryBalanceRequest{
			Address: c.GetAddress(),
			Denom:   denom,
		},
	)
	if err != nil {
		return nil, err
	}
	return &resp.Balance.Amount, nil
}

func (c *CosmosClient) SendToken(target, denom string, amount math.Int, adjustGas bool) (*cosmostypes.TxResponse, error) {
	resp, err := c.BroadcastTx(&banktypes.MsgSend{
		FromAddress: c.GetAddress(),
		ToAddress:   target,
		Amount:      cosmostypes.NewCoins(cosmostypes.NewCoin(denom, amount)),
	}, adjustGas)
	return resp, err
}

func (c *CosmosClient) MultiSend(denom string, totalAmount, eachAmt math.Int, targets []cosmostypes.AccAddress, adjustGas bool) (*cosmostypes.TxResponse, error) {
	outputs := make([]banktypes.Output, len(targets))
	for i, each := range targets {
		outputs[i] = banktypes.NewOutput(each, cosmostypes.NewCoins(cosmostypes.NewCoin(denom, eachAmt)))
	}
	resp, err := c.BroadcastTx(&banktypes.MsgMultiSend{
		Inputs:  []banktypes.Input{banktypes.NewInput(c.accAddress, cosmostypes.NewCoins(cosmostypes.NewCoin(denom, totalAmount)))},
		Outputs: outputs,
	}, adjustGas)

	return resp, err
}

func (c *CosmosClient) GetAddress() string {
	return c.account.Address
}

func (c *CosmosClient) GetAccAddress() cosmostypes.AccAddress {
	return c.accAddress
}

func (c *CosmosClient) handleBroadcastResult(resp *cosmostypes.TxResponse, err error) error {
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return errors.New("make sure that your account has enough balance")
		}
		return err
	}

	if resp.Code > 0 {
		return errors.Errorf("error code: '%d' msg: '%s'", resp.Code, resp.RawLog)
	}
	return nil
}

func isSequenceMismatch(resp *cosmostypes.TxResponse, err error) bool {
	var msg string
	if err != nil {
		msg = err.Error()
	} else if resp != nil {
		msg = resp.RawLog
	}
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "account sequence mismatch") ||
		strings.Contains(msg, "incorrect account sequence")
}

func (c *CosmosClient) BroadcastTx(msg cosmostypes.Msg, adjustGas bool) (*cosmostypes.TxResponse, error) {
	var lastResp *cosmostypes.TxResponse
	var lastErr error

	for attempt := 0; attempt <= maxSequenceRetries; attempt++ {
		if attempt > 0 {
			if err := c.UpdateClientAccountInfo(); err != nil {
				return nil, err
			}
		}

		txBytes, err := c.signTxMsg(msg, adjustGas)
		if err != nil {
			return nil, err
		}

		resp, err := c.txClient.BroadcastTx(
			context.Background(),
			&tx.BroadcastTxRequest{
				TxBytes: txBytes,
				Mode:    tx.BroadcastMode_BROADCAST_MODE_SYNC,
			},
		)
		if err != nil {
			if isSequenceMismatch(nil, err) && attempt < maxSequenceRetries {
				lastErr = err
				continue
			}
			return nil, err
		}

		lastResp = resp.TxResponse
		if resp.TxResponse.Code == 0 {
			c.account.Sequence++
			return resp.TxResponse, c.handleBroadcastResult(resp.TxResponse, nil)
		}

		if isSequenceMismatch(resp.TxResponse, nil) && attempt < maxSequenceRetries {
			lastErr = c.handleBroadcastResult(resp.TxResponse, nil)
			continue
		}

		return resp.TxResponse, c.handleBroadcastResult(resp.TxResponse, nil)
	}

	if lastResp != nil {
		return lastResp, c.handleBroadcastResult(lastResp, nil)
	}
	return nil, lastErr
}

func (c *CosmosClient) WaitForTx(hash string, rate, timeout time.Duration) (*tx.GetTxResponse, error) {
	deadline := time.Now().Add(timeout)
	for {
		resp, err := c.txClient.GetTx(context.Background(), &tx.GetTxRequest{Hash: hash})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				if time.Now().After(deadline) {
					return nil, errors.Errorf("timed out waiting for tx %s to be included in a block", hash)
				}
				time.Sleep(rate)
				continue
			}
			return nil, err
		}
		return resp, err
	}
}

func (c *CosmosClient) signTxMsg(msg cosmostypes.Msg, adjustGas bool) ([]byte, error) {
	encodingCfg := testutils.CreateTestEncodingConfig()
	txBuilder := encodingCfg.TxConfig.NewTxBuilder()

	err := txBuilder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	var newGasLimit uint64 = defaultGasLimit
	if adjustGas {
		txf := clienttx.Factory{}.
			WithGas(defaultGasLimit).
			WithSignMode(1).
			WithTxConfig(encodingCfg.TxConfig).
			WithChainID(c.chainID).
			WithAccountNumber(c.account.AccountNumber).
			WithSequence(c.account.Sequence).
			WithGasAdjustment(defaultGasAdjustment)

		_, newGasLimit, err = clienttx.CalculateGas(c.grpcConn, txf, msg)
		if err != nil {
			return nil, err
		}
	}

	txBuilder.SetGasLimit(newGasLimit)

	if c.gasPrice > 0 {
		feeAmount := int64(stdmath.Ceil(float64(newGasLimit) * c.gasPrice))
		txBuilder.SetFeeAmount(cosmostypes.NewCoins(
			cosmostypes.NewCoin(c.feeDenom, math.NewInt(feeAmount)),
		))
	}

	signerData := authsigning.SignerData{
		ChainID:       c.chainID,
		AccountNumber: c.account.AccountNumber,
		Sequence:      c.account.Sequence,
		PubKey:        c.publicKey,
		Address:       c.account.Address,
	}

	sigData := signing.SingleSignatureData{
		SignMode:  1,
		Signature: nil,
	}
	sig := signing.SignatureV2{
		PubKey:   c.publicKey,
		Data:     &sigData,
		Sequence: c.account.Sequence,
	}

	if err := txBuilder.SetSignatures(sig); err != nil {
		return nil, err
	}

	sigV2, err := clienttx.SignWithPrivKey(
		context.Background(), 1, signerData, txBuilder, &c.privateKey,
		encodingCfg.TxConfig, c.account.Sequence,
	)

	err = txBuilder.SetSignatures(sigV2)
	if err != nil {
		return nil, err
	}

	txBytes, err := encodingCfg.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, err
	}

	return txBytes, nil
}
