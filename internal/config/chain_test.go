package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestChainConfig_Validate_ok(t *testing.T) {
	t.Parallel()
	c := &ChainConfig{
		Blockchains: []Blockchain{
			{Name: "ethereum-mainnet", ChainID: 1},
			{Name: "base", ChainID: 8453},
		},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestChainConfig_Validate_duplicateName(t *testing.T) {
	t.Parallel()
	c := &ChainConfig{
		Blockchains: []Blockchain{
			{Name: "base", ChainID: 1},
			{Name: "base", ChainID: 2},
		},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `duplicate name`) {
		t.Fatalf("got %v", err)
	}
}

func TestChainConfig_Validate_duplicateChainID(t *testing.T) {
	t.Parallel()
	c := &ChainConfig{
		Blockchains: []Blockchain{
			{Name: "net-a", ChainID: 1},
			{Name: "net-b", ChainID: 1},
		},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `duplicate chain_id`) {
		t.Fatalf("got %v", err)
	}
}

func TestChainConfig_Validate_badChainID(t *testing.T) {
	t.Parallel()
	c := &ChainConfig{
		Blockchains: []Blockchain{
			{Name: "x", ChainID: 0},
		},
	}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), `chain_id`) {
		t.Fatalf("got %v", err)
	}
}

func TestLoadChainConfig_repoConfigYAML(t *testing.T) {
	t.Parallel()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to resolve test file path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	cfgPath := filepath.Join(repoRoot, "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		cfgPath = filepath.Join(repoRoot, "config.local-example.yaml")
	}

	cfg, err := LoadChainConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Blockchains) < 1 {
		t.Fatal("expected blockchains in config.yaml")
	}
	if cfg.ChainIDByNetwork()["ethereum-mainnet"] != 1 {
		t.Fatalf("unexpected mapping: %#v", cfg.ChainIDByNetwork())
	}
}

func TestChainConfig_ChainIDByNetwork(t *testing.T) {
	t.Parallel()
	c := &ChainConfig{
		Blockchains: []Blockchain{
			{Name: "base", ChainID: 8453},
		},
	}
	m := c.ChainIDByNetwork()
	if m["base"] != 8453 || len(m) != 1 {
		t.Fatalf("got %#v", m)
	}
}
