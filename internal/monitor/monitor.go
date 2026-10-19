// Package monitor polls an EVM chain and evaluates alert rules against every
// new block.
package monitor

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/zghzzzzgh/chain-alert/internal/config"
	"github.com/zghzzzzgh/chain-alert/internal/notify"
)

// erc20TransferTopic is keccak256("Transfer(address,address,uint256)").
var erc20TransferTopic = crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))

// Monitor owns the RPC connection and the in-memory dedup state.
type Monitor struct {
	cfg      *config.Config
	client   *ethclient.Client
	notifier notify.Notifier
	seen     map[string]time.Time
	rules    []compiledRule
	useWS    bool
}

type compiledRule struct {
	cfg   config.RuleConfig
	addr  common.Address
	topic common.Hash
	watch map[common.Address]bool
}

// New dials the RPC endpoint and compiles the configured rules.
func New(cfg *config.Config, notifier notify.Notifier) (*Monitor, error) {
	endpoint := cfg.Chain.RPCURL
	if cfg.Chain.UseWebSocket && cfg.Chain.WSURL != "" {
		endpoint = cfg.Chain.WSURL
	}
	client, err := ethclient.Dial(endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial rpc %s: %w", endpoint, err)
	}

	m := &Monitor{cfg: cfg, client: client, notifier: notifier, seen: make(map[string]time.Time), useWS: cfg.Chain.UseWebSocket}

	for _, rc := range cfg.Rules {
		cr := compiledRule{cfg: rc}
		switch rc.Type {
		case "erc20_transfer":
			if !common.IsHexAddress(rc.Token) {
				return nil, fmt.Errorf("rule %q: invalid token address", rc.Name)
			}
			cr.addr = common.HexToAddress(rc.Token)
			cr.topic = erc20TransferTopic
		case "contract_event":
			if !common.IsHexAddress(rc.Address) {
				return nil, fmt.Errorf("rule %q: invalid address", rc.Name)
			}
			cr.addr = common.HexToAddress(rc.Address)
			cr.topic = crypto.Keccak256Hash([]byte(normalizeSignature(rc.EventSignature)))
		case "address_watch":
			cr.watch = make(map[common.Address]bool, len(rc.Addresses))
			for _, a := range rc.Addresses {
				if !common.IsHexAddress(a) {
					return nil, fmt.Errorf("rule %q: invalid address %q", rc.Name, a)
				}
				cr.watch[common.HexToAddress(a)] = true
			}
		}
		m.rules = append(m.rules, cr)
	}
	return m, nil
}

func normalizeSignature(sig string) string { return strings.ReplaceAll(sig, " ", "") }

// Run polls forever until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) error {
	if m.useWS {
		return m.runWS(ctx)
	}
	return m.runPoll(ctx)
}

// runWS subscribes to newHeads and processes each block as it is announced.
// chain.confirmations is NOT applied here - alerts fire on the head block itself.
func (m *Monitor) runWS(ctx context.Context) error {
	headers := make(chan *types.Header, 32)
	sub, err := m.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return fmt.Errorf("subscribe newHeads: %w", err)
	}
	defer sub.Unsubscribe()

	log.Printf("websocket mode: subscribed to newHeads (confirmations ignored)")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sub.Err():
			if err != nil {
				return fmt.Errorf("subscription error: %w", err)
			}
			return nil
		case h := <-headers:
			if h == nil || h.Number == nil {
				continue
			}
			m.prune()
			num := h.Number.Uint64()
			if err := m.processBlock(ctx, num); err != nil {
				log.Printf("WARN: block %d: %v", num, err)
			}
		}
	}
}

func (m *Monitor) runPoll(ctx context.Context) error {
	head, err := m.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("get block number: %w", err)
	}
	next := m.cfg.Chain.StartBlock
	if next == 0 || next > head {
		next = head
	}
	log.Printf("watching from block %d (head %d)", next, head)

	ticker := time.NewTicker(time.Duration(m.cfg.Chain.PollInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			m.prune()
			latest, err := m.client.BlockNumber(ctx)
			if err != nil {
				log.Printf("WARN: get latest block: %v", err)
				continue
			}
			target := latest
			if m.cfg.Chain.Confirmations > 0 {
				if latest <= m.cfg.Chain.Confirmations {
					continue
				}
				target = latest - m.cfg.Chain.Confirmations
			}
			for next <= target {
				if err := m.processBlock(ctx, next); err != nil {
					log.Printf("WARN: block %d: %v", next, err)
					break
				}
				next++
			}
		}
	}
}

// RunOnce scans the latest confirmed block once (smoke test).
func (m *Monitor) RunOnce(ctx context.Context) error {
	latest, err := m.client.BlockNumber(ctx)
	if err != nil {
		return fmt.Errorf("get block number: %w", err)
	}
	target := latest
	if m.cfg.Chain.Confirmations > 0 && latest > m.cfg.Chain.Confirmations {
		target = latest - m.cfg.Chain.Confirmations
	}
	log.Printf("scanning block %d once", target)
	return m.processBlock(ctx, target)
}

func (m *Monitor) processBlock(ctx context.Context, num uint64) error {
	block, err := m.client.BlockByNumber(ctx, big.NewInt(int64(num)))
	if err != nil {
		return fmt.Errorf("fetch block: %w", err)
	}
	for _, tx := range block.Transactions() {
		m.checkTransaction(block, tx)
	}

	addresses := m.logAddresses()
	if len(addresses) == 0 {
		return nil
	}
	logs, err := m.client.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(num)),
		ToBlock:   big.NewInt(int64(num)),
		Addresses: addresses,
	})
	if err != nil {
		return fmt.Errorf("filter logs: %w", err)
	}
	for _, lg := range logs {
		m.checkLog(block, lg)
	}
	return nil
}

func (m *Monitor) logAddresses() []common.Address {
	seen := map[common.Address]bool{}
	var out []common.Address
	for _, r := range m.rules {
		if r.cfg.Type != "erc20_transfer" && r.cfg.Type != "contract_event" {
			continue
		}
		if !seen[r.addr] {
			seen[r.addr] = true
			out = append(out, r.addr)
		}
	}
	return out
}

func (m *Monitor) checkTransaction(block *types.Block, tx *types.Transaction) {
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, tx)
	if err != nil {
		from = common.Address{}
	}
	var to common.Address
	if tx.To() != nil {
		to = *tx.To()
	}

	for _, r := range m.rules {
		switch r.cfg.Type {
		case "native_transfer":
			if tx.Value() == nil || tx.Value().Sign() == 0 {
				continue
			}
			eth := weiToFloat(tx.Value())
			if eth < r.cfg.ThresholdETH {
				continue
			}
			msg := fmt.Sprintf(
				"<b>Whale ETH transfer</b>\nRule: %s\nAmount: <b>%.4f ETH</b>\nFrom: <code>%s</code>\nTo: <code>%s</code>\nBlock: %d\nTx: %s",
				r.cfg.Name, eth, from.Hex(), to.Hex(), block.NumberU64(), txLink(tx.Hash().Hex()),
			)
			m.emit(tx.Hash().Hex(), r.cfg.Name, msg)

		case "address_watch":
			if !r.watch[from] && !r.watch[to] {
				continue
			}
			msg := fmt.Sprintf(
				"<b>Watched address activity</b>\nRule: %s\nFrom: <code>%s</code>\nTo: <code>%s</code>\nBlock: %d\nTx: %s",
				r.cfg.Name, from.Hex(), to.Hex(), block.NumberU64(), txLink(tx.Hash().Hex()),
			)
			m.emit(tx.Hash().Hex(), r.cfg.Name, msg)
		}
	}
}

func (m *Monitor) checkLog(block *types.Block, lg types.Log) {
	for _, r := range m.rules {
		if lg.Address != r.addr {
			continue
		}
		if len(lg.Topics) == 0 || lg.Topics[0] != r.topic {
			continue
		}
		switch r.cfg.Type {
		case "erc20_transfer":
			if len(lg.Topics) < 3 || len(lg.Data) < 32 {
				continue
			}
			from := common.BytesToAddress(lg.Topics[1].Bytes())
			to := common.BytesToAddress(lg.Topics[2].Bytes())
			amount := new(big.Int).SetBytes(lg.Data[:32])
			human := tokenAmount(amount, r.cfg.Decimals)
			if human < r.cfg.Threshold {
				continue
			}
			msg := fmt.Sprintf(
				"<b>%s large transfer</b>\nRule: %s\nAmount: <b>%.2f %s</b>\nFrom: <code>%s</code>\nTo: <code>%s</code>\nBlock: %d\nTx: %s",
				r.cfg.TokenSymbol, r.cfg.Name, human, r.cfg.TokenSymbol,
				from.Hex(), to.Hex(), block.NumberU64(), txLink(lg.TxHash.Hex()),
			)
			m.emit(lg.TxHash.Hex(), r.cfg.Name, msg)

		case "contract_event":
			msg := fmt.Sprintf(
				"<b>Contract event</b>\nRule: %s\nContract: <code>%s</code>\nEvent: <code>%s</code>\nBlock: %d\nTx: %s",
				r.cfg.Name, lg.Address.Hex(), r.cfg.EventSignature, block.NumberU64(), txLink(lg.TxHash.Hex()),
			)
			m.emit(lg.TxHash.Hex(), r.cfg.Name, msg)
		}
	}
}

// emit deduplicates and forwards one alert.
func (m *Monitor) emit(txHash, ruleName, text string) {
	key := txHash + "|" + ruleName
	if _, ok := m.seen[key]; ok {
		return
	}
	m.seen[key] = time.Now()
	if err := m.notifier.Send(text); err != nil {
		log.Printf("WARN: send alert (%s): %v", ruleName, err)
		delete(m.seen, key)
	}
}

// prune drops dedup entries older than 24h so the map cannot grow unbounded.
func (m *Monitor) prune() {
	cutoff := time.Now().Add(-24 * time.Hour)
	for k, t := range m.seen {
		if t.Before(cutoff) {
			delete(m.seen, k)
		}
	}
}

func weiToFloat(wei *big.Int) float64 {
	f := new(big.Float).SetInt(wei)
	f.Quo(f, big.NewFloat(math.Pow10(18)))
	out, _ := f.Float64()
	return out
}

func tokenAmount(raw *big.Int, decimals int) float64 {
	f := new(big.Float).SetInt(raw)
	f.Quo(f, big.NewFloat(math.Pow10(decimals)))
	out, _ := f.Float64()
	return out
}

// txLink builds an Etherscan link. Change the base for other chains.
func txLink(hash string) string {
	return "https://etherscan.io/tx/" + hash
}
