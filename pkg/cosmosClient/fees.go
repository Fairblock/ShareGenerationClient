package cosmosClient

import "cosmossdk.io/math"

func calculateFeeAmount(gasLimit uint64, gasPrice math.LegacyDec) math.Int {
	return gasPrice.MulInt(math.NewIntFromUint64(gasLimit)).Ceil().TruncateInt()
}
