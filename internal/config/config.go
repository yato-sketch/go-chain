package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds all node configuration.
type Config struct {
	// Network selects chain params: "mainnet", "testnet", "regtest".
	Network string `json:"network"`

	// DataDir is the root directory for all node data.
	DataDir string `json:"data_dir"`

	// ListenAddr is the TCP address to listen on for P2P connections.
	ListenAddr string `json:"listen_addr"`

	// RPCAddr is the address for the local RPC/HTTP API.
	RPCAddr string `json:"rpc_addr"`

	// SeedPeers are additional seed peer addresses (IP:port).
	SeedPeers []string `json:"seed_peers"`

	// MaxInbound is the maximum number of inbound peer connections.
	MaxInbound int `json:"max_inbound"`

	// MaxOutbound is the maximum number of outbound peer connections.
	MaxOutbound int `json:"max_outbound"`

	// MiningEnabled controls whether the built-in miner runs.
	MiningEnabled bool `json:"mining_enabled"`

	// MiningAddr is the reward address/script placeholder for mined blocks.
	MiningAddr string `json:"mining_addr"`

	// LogLevel controls logging verbosity: "debug", "info", "warn", "error".
	LogLevel string `json:"log_level"`
}

// DefaultConfig returns a config with sensible defaults for regtest.
func DefaultConfig() *Config {
	return &Config{
		Network:       "regtest",
		DataDir:       defaultDataDir(),
		ListenAddr:    "0.0.0.0:19444",
		RPCAddr:       "127.0.0.1:19445",
		SeedPeers:     []string{},
		MaxInbound:    25,
		MaxOutbound:   8,
		MiningEnabled: false,
		MiningAddr:    "",
		LogLevel:      "info",
	}
}

// LoadConfig reads a config from a JSON file, falling back to defaults for missing fields.
func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// SaveConfig writes the config to a JSON file.
func SaveConfig(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// DBPath returns the path to the block database within the data directory.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "blocks.db")
}

// PeerDBPath returns the path to the peer database.
func (c *Config) PeerDBPath() string {
	return filepath.Join(c.DataDir, "peers.db")
}

// EnsureDataDir creates the data directory if it doesn't exist.
func (c *Config) EnsureDataDir() error {
	return os.MkdirAll(c.DataDir, 0700)
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".fairchain"
	}
	return filepath.Join(home, ".fairchain")
}
