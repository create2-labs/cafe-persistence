package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Blockchain represents a blockchain network configuration
type Blockchain struct {
	Name             string `yaml:"name"`
	RPC              string `yaml:"rpc"`
	MoralisChainName string `yaml:"moralis_chain_name"`
	// ChainID is the EIP-155 chain identifier used when exporting wallet observations (e.g. CPM wire contract).
	ChainID int64 `yaml:"chain_id"`
}

// ChainConfig holds the blockchains section loaded from the Discovery config file.
type ChainConfig struct {
	Blockchains []Blockchain `yaml:"blockchains"`
}

// Validate checks blockchains: non-empty names, positive chain_id, no duplicate names or chain IDs.
func (c *ChainConfig) Validate() error {
	seenName := make(map[string]struct{})
	seenChain := make(map[int64]string)
	for i, b := range c.Blockchains {
		name := strings.TrimSpace(b.Name)
		if name == "" {
			return fmt.Errorf("blockchains[%d]: name is required", i)
		}
		if _, dup := seenName[name]; dup {
			return fmt.Errorf("blockchains: duplicate name %q", name)
		}
		seenName[name] = struct{}{}

		if b.ChainID <= 0 {
			return fmt.Errorf("blockchains[%d] (%s): chain_id must be a positive EIP-155 id", i, name)
		}
		if prev, dup := seenChain[b.ChainID]; dup {
			return fmt.Errorf("blockchains: duplicate chain_id %d for %q and %q", b.ChainID, prev, name)
		}
		seenChain[b.ChainID] = name
	}
	return nil
}

// ChainIDByNetwork returns name -> chain_id for wallet observation export (deterministic mapping).
func (c *ChainConfig) ChainIDByNetwork() map[string]int64 {
	m := make(map[string]int64, len(c.Blockchains))
	for _, b := range c.Blockchains {
		if b.ChainID > 0 {
			m[b.Name] = b.ChainID
		}
	}
	return m
}

// LoadChainConfig reads and parses the configuration file and validates blockchains.
func LoadChainConfig(configPath string) (*ChainConfig, error) {
	// #nosec G304 -- configPath is validated and comes from trusted source (env var or default)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg ChainConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}
