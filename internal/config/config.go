// Package config loads and validates the YAML configuration.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the root of config.yaml.
type Config struct {
	// Chains is the multi-chain form: each entry carries its own rules.
	Chains []ChainConfig `yaml:"chains"`

	// Legacy single-chain form. Used only when Chains is empty.
	Chain ChainConfig  `yaml:"chain"`
	Rules []RuleConfig `yaml:"rules"`

	Telegram TelegramConfig `yaml:"telegram"`
	Discord  DiscordConfig  `yaml:"discord"`
}

// TelegramConfig holds the bot credentials.
type TelegramConfig struct {
	BotToken string `yaml:"bot_token"`
	ChatID   string `yaml:"chat_id"`
}

// DiscordConfig holds a Discord webhook URL (no bot token required).
type DiscordConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

// ChainConfig describes one EVM chain, its transports and its rules.
type ChainConfig struct {
	Name          string       `yaml:"name"`
	RPCURL        string       `yaml:"rpc_url"`
	WSURL         string       `yaml:"ws_url"`
	UseWebSocket  bool         `yaml:"use_websocket"`
	StartBlock    uint64       `yaml:"start_block"`
	Confirmations uint64       `yaml:"confirmations"`
	PollInterval  int          `yaml:"poll_interval_seconds"`
	Explorer      string       `yaml:"explorer"` // e.g. https://etherscan.io/tx/
	Rules         []RuleConfig `yaml:"rules"`
}

// RuleConfig is one alerting rule. Only fields relevant to Type are used.
type RuleConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`

	ThresholdETH float64 `yaml:"threshold_eth"`

	Token       string  `yaml:"token"`
	TokenSymbol string  `yaml:"token_symbol"`
	Decimals    int     `yaml:"decimals"`
	Threshold   float64 `yaml:"threshold"`

	Address        string `yaml:"address"`
	EventSignature string `yaml:"event_signature"`

	Addresses []string `yaml:"addresses"`
}

// Load reads the YAML file, normalises the single-chain form and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// legacy: top-level chain + rules
	if len(cfg.Chains) == 0 {
		legacy := cfg.Chain
		legacy.Rules = cfg.Rules
		cfg.Chains = []ChainConfig{legacy}
	}

	if len(cfg.Chains) == 0 {
		return nil, fmt.Errorf("no chain configured")
	}

	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		if strings.TrimSpace(c.RPCURL) == "" && strings.TrimSpace(c.WSURL) == "" {
			return nil, fmt.Errorf("chain %d: rpc_url is required", i+1)
		}
		if c.Name == "" {
			c.Name = fmt.Sprintf("chain-%d", i+1)
		}
		if c.PollInterval <= 0 {
			c.PollInterval = 12
		}
		if c.UseWebSocket && c.WSURL == "" && !strings.HasPrefix(c.RPCURL, "ws") {
			return nil, fmt.Errorf("chain %q: use_websocket requires ws_url", c.Name)
		}
		if c.Explorer == "" {
			c.Explorer = "https://etherscan.io/tx/"
		}
		if len(c.Rules) == 0 {
			return nil, fmt.Errorf("chain %q: at least one rule is required", c.Name)
		}
		if err := validateRules(c); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

func validateRules(c *ChainConfig) error {
	for i := range c.Rules {
		r := &c.Rules[i]
		if r.Name == "" {
			r.Name = fmt.Sprintf("%s-rule-%d", c.Name, i+1)
		}
		switch r.Type {
		case "native_transfer":
			if r.ThresholdETH <= 0 {
				return fmt.Errorf("chain %q rule %q: threshold_eth must be > 0", c.Name, r.Name)
			}
		case "erc20_transfer":
			if r.Token == "" {
				return fmt.Errorf("chain %q rule %q: token is required", c.Name, r.Name)
			}
			if r.Decimals < 0 || r.Decimals > 36 {
				return fmt.Errorf("chain %q rule %q: decimals out of range", c.Name, r.Name)
			}
			if r.TokenSymbol == "" {
				r.TokenSymbol = "TOKEN"
			}
		case "contract_event":
			if r.Address == "" || r.EventSignature == "" {
				return fmt.Errorf("chain %q rule %q: address and event_signature are required", c.Name, r.Name)
			}
		case "address_watch":
			if len(r.Addresses) == 0 {
				return fmt.Errorf("chain %q rule %q: addresses is required", c.Name, r.Name)
			}
		default:
			return fmt.Errorf("chain %q rule %q: unknown type %q", c.Name, r.Name, r.Type)
		}
	}
	return nil
}
