package cosmosClient

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/skip-mev/block-sdk/v2/testutils"
	"github.com/stretchr/testify/require"
)

func TestSignTxMsgIncludesConfiguredFee(t *testing.T) {
	privateKey := secp256k1.PrivKey{Key: bytes.Repeat([]byte{1}, 32)}
	publicKey := privateKey.PubKey()
	from := sdk.AccAddress(publicKey.Address())
	to := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	gasPrice, err := math.LegacyNewDecFromStr("0.025")
	require.NoError(t, err)

	client := &CosmosClient{
		privateKey: privateKey,
		publicKey:  publicKey,
		account: authtypes.BaseAccount{
			Address:       from.String(),
			AccountNumber: 7,
			Sequence:      3,
		},
		accAddress: from,
		chainID:    "fee-test-chain",
		feeDenom:   "ufair",
		gasPrice:   gasPrice,
	}

	msg := banktypes.NewMsgSend(
		from,
		to,
		sdk.NewCoins(sdk.NewInt64Coin("ufair", 1)),
	)

	txBytes, err := client.signTxMsg(msg, false)
	require.NoError(t, err)

	encodingCfg := testutils.CreateTestEncodingConfig()
	decodedTx, err := encodingCfg.TxConfig.TxDecoder()(txBytes)
	require.NoError(t, err)

	feeTx, ok := decodedTx.(sdk.FeeTx)
	require.True(t, ok, "decoded transaction must implement sdk.FeeTx")
	require.Equal(t, uint64(defaultGasLimit), feeTx.GetGas())
	require.Equal(t, sdk.NewCoins(sdk.NewInt64Coin("ufair", 7500)), feeTx.GetFee())
}
