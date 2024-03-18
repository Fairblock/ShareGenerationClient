package main

import (
	"ApiSetupClient/cosmosClient"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	distIBE "github.com/FairBlock/DistributedIBE"
	dcrdSecp256k1 "github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/drand/kyber"
	bls "github.com/drand/kyber-bls12381"
	"math"
	"math/big"
)

type ShareGeneratorClient struct {
	CosmosClient *cosmosClient.CosmosClient
}

type EncryptedShare struct {
	EncShare         string
	Index            kyber.Scalar
	PK               *dcrdSecp256k1.PublicKey
	ValidatorAddress string
}

type GenerateResult struct {
	EncryptedKeyShares []*EncryptedShare
	Commitments        []string
	MasterPublicKey    string
}

func (sgc *ShareGeneratorClient) Generate() *GenerateResult {
	validatorsPubInfos, err := sgc.CosmosClient.GetAllValidatorsPubInfos()
	if err != nil {
		fmt.Printf("error getting all validators public infos %s\n", err.Error())
		return nil
	}

	n := len(validatorsPubInfos)
	t := int(math.Ceil(float64(n) * (2.0 / 3.0)))

	shares, mpk, _, err := distIBE.GenerateShares(uint32(n), uint32(t))
	if err != nil {
		fmt.Printf("error while generating shares: %s\n", err.Error())
		return nil
	}

	masterPublicKeyByte, err := mpk.MarshalBinary()
	if err != nil {
		fmt.Printf("error while marshaling master public key to binary: %s\n", err.Error())
		return nil
	}

	var result GenerateResult
	result.MasterPublicKey = hex.EncodeToString(masterPublicKeyByte)

	suite := bls.NewBLS12381Suite()
	keyShareCommitments := make([]string, n)
	sharesList := make([]*EncryptedShare, n)

	for i, s := range shares {
		indexByte, _ := hex.DecodeString(s.Index.String())
		indexInt := big.NewInt(0).SetBytes(indexByte).Uint64()

		commitmentPoints := suite.G1().Point().Mul(s.Value, suite.G1().Point().Base())
		commitmentPointsBinary, _ := commitmentPoints.MarshalBinary()

		keyShareCommitments[indexInt] = hex.EncodeToString(commitmentPointsBinary)

		sb, _ := s.Value.MarshalBinary()

		res, err := dcrdSecp256k1.Encrypt(validatorsPubInfos[i].PublicKey, sb)
		if err != nil {
			fmt.Printf("Error encrypting share: %s\n", err.Error())
			return nil
		}

		share := EncryptedShare{
			base64.StdEncoding.EncodeToString(res),
			s.Index,
			validatorsPubInfos[i].PublicKey,
			validatorsPubInfos[i].Address,
		}

		sharesList[indexInt] = &share
	}

	result.EncryptedKeyShares = sharesList
	result.Commitments = keyShareCommitments

	return &result
}
