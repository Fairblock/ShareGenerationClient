package cosmosClient

import (
	"context"
	"cosmossdk.io/math"
	"encoding/hex"
	"github.com/Fairblock/fairyring/api/fairyring/keyshare"
	"github.com/Fairblock/fairyring/app"
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
	"google.golang.org/grpc"
	"log"
	"strings"
	"time"
)

const (
	defaultGasAdjustment = 1.5
	defaultGasLimit      = 300000
)

type CosmosClient struct {
	authClient          authtypes.QueryClient
	txClient            tx.ServiceClient
	grpcConn            *grpc.ClientConn
	bankQueryClient     banktypes.QueryClient
	keyshareQueryClient keyshare.QueryClient
	pepQueryClient      types.QueryClient
	privateKey          secp256k1.PrivKey
	publicKey           cryptotypes.PubKey
	account             authtypes.BaseAccount
	accAddress          cosmostypes.AccAddress
	chainID             string
}

type ValidatorPubInfo struct {
	PublicKey *dcrdSecp256k1.PublicKey
	Address   string
}

func (c *CosmosClient) GetAllValidatorsPubInfos() ([]ValidatorPubInfo, error) {
	resp, err := c.keyshareQueryClient.ValidatorSetAll(
		context.Background(),
		&keyshare.QueryAllValidatorSetRequest{},
	)

	if err != nil {
		return nil, err
	}

	if len(resp.ValidatorSet) == 0 {
		return nil, errors.New("validator set in key share module is empty")
	}

	validatorPubKeys := make([]ValidatorPubInfo, len(resp.ValidatorSet))

	for i, addr := range resp.ValidatorSet {
		resp, err := c.authClient.Account(
			context.Background(),
			&authtypes.QueryAccountRequest{Address: addr.Validator},
		)
		if err != nil {
			return nil, err
		}

		var baseAccount authtypes.BaseAccount
		if err = baseAccount.Unmarshal(resp.Account.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling base account")
		}

		var secp256k1PubKey secp256k1.PubKey
		if err = secp256k1PubKey.Unmarshal(baseAccount.PubKey.Value); err != nil {
			return nil, errors.Wrap(err, "error when unmarshalling pub key")
		}
		pubKey, err := dcrdSecp256k1.ParsePubKey(secp256k1PubKey.Key)
		if err != nil {
			return nil, errors.Wrap(err, "error parsing pub key to dcrd pub key")
		}
		validatorPubKeys[i] = ValidatorPubInfo{
			PublicKey: pubKey,
			Address:   baseAccount.Address,
		}
	}
	return validatorPubKeys, nil
}

func PrivateKeyToAccAddress(privateKeyHex string) (cosmostypes.AccAddress, error) {
	keyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}

	privateKey := secp256k1.PrivKey{Key: keyBytes}

	return cosmostypes.AccAddress(privateKey.PubKey().Address()), nil
}

func NewCosmosClient(
	endpoint string,
	privateKeyHex string,
	chainID string,
) (*CosmosClient, error) {
	grpcConn, err := grpc.Dial(
		endpoint,
		grpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	authClient := authtypes.NewQueryClient(grpcConn)
	bankClient := banktypes.NewQueryClient(grpcConn)
	pepeClient := types.NewQueryClient(grpcConn)
	keyshareClient := keyshare.NewQueryClient(grpcConn)

	keyBytes, err := hex.DecodeString(privateKeyHex)
	if err != nil {
		return nil, err
	}

	privateKey := secp256k1.PrivKey{Key: keyBytes}
	pubKey := privateKey.PubKey()
	address := pubKey.Address()

	cfg := cosmostypes.GetConfig()
	cfg.SetBech32PrefixForAccount("fairy", "fairypub")
	cfg.SetBech32PrefixForValidator("fairyvaloper", "fairyvaloperpub")
	cfg.SetBech32PrefixForConsensusNode("fairyvalcons", "fairyrvalconspub")

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
		grpcConn:            grpcConn,
		privateKey:          privateKey,
		account:             baseAccount,
		accAddress:          accAddr,
		publicKey:           pubKey,
		chainID:             chainID,
	}, nil
}

func (c *CosmosClient) GetActivePubKey() (*types.QueryPubKeyResponse, error) {
	resp, err := c.pepQueryClient.PubKey(
		context.Background(),
		&types.QueryPubKeyRequest{},
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

func (c *CosmosClient) BroadcastTx(msg cosmostypes.Msg, adjustGas bool) (*cosmostypes.TxResponse, error) {
	txBytes, err := c.signTxMsg(msg, adjustGas)
	if err != nil {
		return nil, err
	}

	c.account.Sequence++

	resp, err := c.txClient.BroadcastTx(
		context.Background(),
		&tx.BroadcastTxRequest{
			TxBytes: txBytes,
			Mode:    tx.BroadcastMode_BROADCAST_MODE_SYNC,
		},
	)
	if err != nil {
		return nil, err
	}

	return resp.TxResponse, c.handleBroadcastResult(resp.TxResponse, err)
}

func (c *CosmosClient) WaitForTx(hash string, rate time.Duration) (*tx.GetTxResponse, error) {
	for {
		resp, err := c.txClient.GetTx(context.Background(), &tx.GetTxRequest{Hash: hash})
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				time.Sleep(rate)
				continue
			}
			return nil, err
		}
		return resp, err
	}
}

func (c *CosmosClient) signTxMsg(msg cosmostypes.Msg, adjustGas bool) ([]byte, error) {
	encodingCfg := app.MakeEncodingConfig()
	txBuilder := encodingCfg.TxConfig.NewTxBuilder()
	signMode := encodingCfg.TxConfig.SignModeHandler().DefaultMode()

	err := txBuilder.SetMsgs(msg)
	if err != nil {
		return nil, err
	}

	var newGasLimit uint64 = defaultGasLimit
	if adjustGas {
		txf := clienttx.Factory{}.
			WithGas(defaultGasLimit).
			WithSignMode(signMode).
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

	signerData := authsigning.SignerData{
		ChainID:       c.chainID,
		AccountNumber: c.account.AccountNumber,
		Sequence:      c.account.Sequence,
		PubKey:        c.publicKey,
		Address:       c.account.Address,
	}

	sigData := signing.SingleSignatureData{
		SignMode:  signMode,
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
		signMode, signerData, txBuilder, &c.privateKey,
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
