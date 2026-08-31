package config

import "testing"

func TestDefaultConfigIncludesGasPrice(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.FairyRingNode.Denom != DefaultDenom {
		t.Fatalf("unexpected default denom: got %q want %q", cfg.FairyRingNode.Denom, DefaultDenom)
	}
	if cfg.FairyRingNode.GasPrice != DefaultGasPrice {
		t.Fatalf("unexpected default gas price: got %q want %q", cfg.FairyRingNode.GasPrice, DefaultGasPrice)
	}
}
