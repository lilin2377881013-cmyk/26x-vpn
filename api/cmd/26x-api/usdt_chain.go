package main

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const transferTopic = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

type tronReceipt struct {
	ID        string `json:"id"`
	Block     int64  `json:"blockNumber"`
	Timestamp int64  `json:"blockTimeStamp"`
	Error     string `json:"Error"`
	Receipt   struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Logs []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"log"`
}
type chainTransfer struct {
	hash           string
	index          int
	address, units string
	block          int64
	at             time.Time
}

func (g *usdtGateway) api(ctx context.Context, method, path string, body any, result any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, g.apiOrigin+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if g.apiKey != "" {
		req.Header.Set("TRON-PRO-API-KEY", g.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("chain API returned HTTP %d", response.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil || len(data) > 4<<20 {
		return errors.New("chain response too large or incomplete")
	}
	if err = json.Unmarshal(data, result); err != nil {
		return errors.New("invalid chain response")
	}
	return nil
}
func parseTronReceipt(receipt tronReceipt, expectedHash, address string) ([]chainTransfer, error) {
	if receipt.Error != "" || receipt.ID != expectedHash || !tronHash.MatchString(expectedHash) || receipt.Block <= 0 || receipt.Timestamp <= 0 {
		return nil, errors.New("confirmed transaction receipt unavailable")
	}
	if receipt.Receipt.Result != "SUCCESS" {
		return nil, nil
	}
	recipient, err := tronAddressHex(address)
	if err != nil {
		return nil, err
	}
	contract, _ := tronAddressHex(usdtContract)
	result := []chainTransfer{}
	for index, log := range receipt.Logs {
		logAddress := strings.ToLower(log.Address)
		if len(logAddress) == 42 && strings.HasPrefix(logAddress, "41") {
			logAddress = logAddress[2:]
		}
		if logAddress != contract || len(log.Topics) != 3 || strings.ToLower(log.Topics[0]) != transferTopic {
			continue
		}
		destination := strings.ToLower(log.Topics[2])
		if destination != strings.Repeat("0", 24)+recipient || len(log.Data) != 64 {
			continue
		}
		units, ok := new(big.Int).SetString(log.Data, 16)
		if !ok || units.Sign() <= 0 {
			continue
		}
		result = append(result, chainTransfer{hash: expectedHash, index: index, address: address, units: units.String(), block: receipt.Block, at: time.UnixMilli(receipt.Timestamp).UTC()})
	}
	return result, nil
}

func (s *server) watchUSDT(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.pollUSDT(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Warn("USDT monitor retry", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *server) pollUSDT(ctx context.Context) error {
	g := s.cfg.usdt
	if _, err := s.db.ExecContext(ctx, `INSERT INTO usdt_wallets(address,scan_through_ms) VALUES($1,$2) ON CONFLICT DO NOTHING`, g.address, time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT address FROM usdt_wallets ORDER BY created_at`)
	if err != nil {
		return err
	}
	var wallets []string
	for rows.Next() {
		var address string
		if err = rows.Scan(&address); err != nil {
			rows.Close()
			return err
		}
		wallets = append(wallets, address)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, address := range wallets {
		round, cancel := context.WithTimeout(ctx, 90*time.Second)
		err = s.scanUSDTWallet(round, address)
		cancel()
		if err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE usdt_invoices SET status='expired' WHERE status='waiting' AND expires_at<now()`)
	return err
}
func (s *server) scanUSDTWallet(ctx context.Context, address string) error {
	if _, err := tronAddressHex(address); err != nil {
		return err
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var locked bool
	key := "scan:" + address
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return nil
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(unlock, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key); e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	var through int64
	if err = conn.QueryRowContext(ctx, `SELECT scan_through_ms FROM usdt_wallets WHERE address=$1`, address).Scan(&through); err != nil {
		return err
	}
	// Re-read a full day to tolerate delayed indexing. The persisted watermark is
	// advanced only after every page and receipt in this scan succeeds.
	min := through - 24*60*60*1000
	if min < 0 {
		min = 0
	}
	max := time.Now().Add(-15 * time.Second).UnixMilli()
	query := url.Values{"only_confirmed": {"true"}, "only_to": {"true"}, "contract_address": {usdtContract}, "limit": {"200"}, "order_by": {"block_timestamp,asc"}, "min_timestamp": {strconv.FormatInt(min, 10)}, "max_timestamp": {strconv.FormatInt(max, 10)}}
	seen := map[string]bool{}
	fingerprints := map[string]bool{}
	for page := 0; page < 100; page++ {
		var response struct {
			Success bool `json:"success"`
			Data    []struct {
				Hash string `json:"transaction_id"`
			} `json:"data"`
			Meta struct {
				Fingerprint string `json:"fingerprint"`
				Links       struct {
					Next string `json:"next"`
				} `json:"links"`
			} `json:"meta"`
		}
		if err = s.cfg.usdt.api(ctx, http.MethodGet, "/v1/accounts/"+address+"/transactions/trc20?"+query.Encode(), nil, &response); err != nil {
			return err
		}
		if !response.Success || response.Data == nil {
			return errors.New("chain history incomplete")
		}
		for _, item := range response.Data {
			hash := strings.ToLower(item.Hash)
			if !tronHash.MatchString(hash) {
				return errors.New("invalid transaction hash in history")
			}
			if seen[hash] {
				continue
			}
			seen[hash] = true
			var receipt tronReceipt
			if err = s.cfg.usdt.api(ctx, http.MethodPost, "/walletsolidity/gettransactioninfobyid", map[string]string{"value": hash}, &receipt); err != nil {
				return err
			}
			transfers, e := parseTronReceipt(receipt, hash, address)
			if e != nil {
				return e
			}
			for _, transfer := range transfers {
				if e = s.recordUSDTTransfer(ctx, transfer); e != nil {
					return e
				}
			}
		}
		if response.Meta.Links.Next == "" {
			if len(response.Data) == 200 && response.Meta.Fingerprint == "" {
				return errors.New("chain pagination missing")
			}
			_, err = conn.ExecContext(ctx, `UPDATE usdt_wallets SET scan_through_ms=GREATEST(scan_through_ms,$2),last_success_at=now() WHERE address=$1`, address, max)
			return err
		}
		fingerprint := response.Meta.Fingerprint
		if fingerprint == "" || len(fingerprint) > 4096 || fingerprints[fingerprint] {
			return errors.New("invalid chain pagination")
		}
		fingerprints[fingerprint] = true
		query.Set("fingerprint", fingerprint)
	}
	return errors.New("chain scan exceeded page budget; watermark unchanged")
}

func (s *server) recordUSDTTransfer(ctx context.Context, t chainTransfer) error {
	if !tronHash.MatchString(t.hash) || t.index < 0 || t.block <= 0 || t.at.After(time.Now().Add(2*time.Minute)) {
		return errors.New("invalid transfer evidence")
	}
	if _, err := tronAddressHex(t.address); err != nil {
		return err
	}
	units, ok := new(big.Int).SetString(t.units, 10)
	if !ok || units.Sign() <= 0 || len(t.units) > 78 {
		return errors.New("invalid transfer amount")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("transfer:%s:%d", t.hash, t.index)); err != nil {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usdt_transfers WHERE tx_hash=$1 AND log_index=$2)`, t.hash, t.index).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var order, status string
	var created, expires time.Time
	err = tx.QueryRowContext(ctx, `SELECT order_no,status,created_at,expires_at FROM usdt_invoices WHERE address=$1 AND amount_units=$2::numeric FOR UPDATE`, t.address, t.units).Scan(&order, &status, &created, &expires)
	result, reason := "unmatched", "No exact order amount; manual review required"
	var matched any
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	} else if err == nil {
		matched = order
		result = "review"
		switch {
		case t.at.Before(created):
			reason = "Transfer predates invoice"
		case t.at.After(expires):
			reason = "Payment arrived after expiry"
		case status == "paid":
			reason = "Invoice already paid by another transfer"
		default:
			if err = s.grantOrderTx(ctx, tx, order, fmt.Sprintf("trc20:%s:%d", t.hash, t.index), "usdt"); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE usdt_invoices SET status='paid' WHERE order_no=$1`, order); err != nil {
				return err
			}
			result = "paid"
			reason = "Solidified USDT transfer matched"
		}
	}
	if err != nil {
		return err
	}
	if result == "review" {
		if _, err = tx.ExecContext(ctx, `UPDATE usdt_invoices SET status='review' WHERE order_no=$1 AND status<>'paid'`, order); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO usdt_transfers(tx_hash,log_index,address,amount_units,block_number,block_time,order_no,result,reason) VALUES($1,$2,$3,$4::numeric,$5,$6,$7,$8,$9)`, t.hash, t.index, t.address, t.units, t.block, t.at, matched, result, reason)
	if err != nil {
		return err
	}
	return tx.Commit()
}
