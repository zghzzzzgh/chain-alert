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
Chain    ChainConfig    `yaml:"chain"`
Telegram TelegramConfig `yaml:"telegram"`
Rules    []RuleConfig   `yaml:"rules"`
}

// ChainConfig describes the EVM chain and RPC endpoint to poll.
type ChainConfig struct {
Name          string `yaml:"name"`
RPCURL        string `yaml:"rpc_url"`
StartBlock    uint64 `yaml:"start_block"`
Confirmations uint64 `yaml:"confirmations"`
PollInterval  int    `yaml:"poll_interval_seconds"`
}

// TelegramConfig holds the bot credentials.
type TelegramConfig struct {
BotToken string `yaml:"bot_token"`
ChatID   string `yaml:"chat_id"`
}

// RuleConfig is one alerting rule. Only fields relevant to Type are used.
type RuleConfig struct {
Name string `yaml:"name"`
Type string `yaml:"type"`

// native_transfer
ThresholdETH float64 `yaml:"threshold_eth"`

// erc20_transfer
Token       string  `yaml:"token"`
TokenSymbol string  `yaml:"token_symbol"`
Decimals    int     `yaml:"decimals"`
Threshold   float64 `yaml:"threshold"`

// contract_event
Address        string `yaml:"address"`
EventSignature string `yaml:"event_signature"`

// address_watch
Addresses []string `yaml:"addresses"`
}

// Load reads the YAML file and applies defaults / basic validation.
func Load(path string) (*Config, error) {
raw, err := os.ReadFile(path)
if err != nil {
return nil, fmt.Errorf("read %s: %w", path, err)
}

var cfg Config
if err := yaml.Unmarshal(raw, &cfg); err != nil {
return nil, fmt.Errorf("parse %s: %w", path, err)
}

if strings.TrimSpace(cfg.Chain.RPCURL) == "" {
return nil, fmt.Errorf("chain.rpc_url is required")
}
if cfg.Chain.Name == "" {
cfg.Chain.Name = "evm"
}
if cfg.Chain.PollInterval <= 0 {
cfg.Chain.PollInterval = 12
}
if len(cfg.Rules) == 0 {
return nil, fmt.Errorf("at least one rule is required")
}

for i := range cfg.Rules {
r := &cfg.Rules[i]
if r.Name == "" {
r.Name = fmt.Sprintf("rule-%d", i+1)
}
switch r.Type {
case "native_transfer":
if r.ThresholdETH <= 0 {
return nil, fmt.Errorf("rule %q: threshold_eth must be > 0", r.Name)
}
case "erc20_transfer":
if r.Token == "" {
return nil, fmt.Errorf("rule %q: token is required", r.Name)
}
if r.Decimals < 0 || r.Decimals > 36 {
return nil, fmt.Errorf("rule %q: decimals out of range", r.Name)
}
if r.TokenSymbol == "" {
r.TokenSymbol = "TOKEN"
}
case "contract_event":
if r.Address == "" || r.EventSignature == "" {
return nil, fmt.Errorf("rule %q: address and event_signature are required", r.Name)
}
case "address_watch":
if len(r.Addresses) == 0 {
return nil, fmt.Errorf("rule %q: addresses is required", r.Name)
}
default:
return nil, fmt.Errorf("rule %q: unknown type %q", r.Name, r.Type)
}
}

return &cfg, nil
}