package main

import (
	"database/sql"
	"net/http"
	"strings"
	"time"
)

func (s *server) listUSDTTransfers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT t.tx_hash,t.log_index,t.address,t.amount_units::text,t.block_number,t.block_time,t.order_no,t.result,t.reason,
 u.email::text FROM usdt_transfers t LEFT JOIN payment_orders o ON o.order_no=t.order_no LEFT JOIN users u ON u.id=o.user_id
 ORDER BY t.created_at DESC,t.tx_hash,t.log_index LIMIT 200`)
	if err != nil {
		writeError(w, 503, "usdt_unavailable", "Unable to read USDT payments")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var hash, address, units, result, reason string
		var index int
		var block int64
		var at time.Time
		var order, email sql.NullString
		if err = rows.Scan(&hash, &index, &address, &units, &block, &at, &order, &result, &reason, &email); err != nil {
			writeError(w, 503, "usdt_unavailable", "Unable to read USDT payments")
			return
		}
		items = append(items, map[string]any{"txHash": hash, "logIndex": index, "address": address, "amountUnits": units, "blockNumber": block, "blockTime": at.UTC().Format(time.RFC3339), "orderNo": nullableString(order), "email": nullableString(email), "result": result, "reason": reason})
	}
	if rows.Err() != nil {
		writeError(w, 503, "usdt_unavailable", "Unable to read USDT payments")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": items})
}

// Recheck reads the solidified chain receipt. A submitted TXID is never proof
// of payment by itself and cannot override the amount, recipient or deadline.
func (s *server) recheckUSDT(w http.ResponseWriter, r *http.Request) {
	g := s.cfg.usdt
	if g == nil {
		writeError(w, 503, "usdt_unavailable", "USDT monitor is not configured")
		return
	}
	var input struct {
		TxHash string `json:"txHash"`
	}
	if decodeJSON(w, r, &input) != nil {
		writeError(w, 400, "invalid_transaction", "Provide a transaction hash")
		return
	}
	input.TxHash = strings.ToLower(strings.TrimSpace(input.TxHash))
	if !tronHash.MatchString(input.TxHash) {
		writeError(w, 400, "invalid_transaction", "Invalid transaction hash")
		return
	}
	var receipt tronReceipt
	if err := g.api(r.Context(), http.MethodPost, "/walletsolidity/gettransactioninfobyid", map[string]string{"value": input.TxHash}, &receipt); err != nil {
		writeError(w, 503, "chain_unavailable", "Chain query unavailable; retry later")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT address FROM usdt_wallets ORDER BY created_at`)
	if err != nil {
		writeError(w, 503, "usdt_unavailable", "Unable to read wallets")
		return
	}
	var addresses []string
	for rows.Next() {
		var address string
		if err = rows.Scan(&address); err != nil {
			break
		}
		addresses = append(addresses, address)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		writeError(w, 503, "usdt_unavailable", "Unable to read wallets")
		return
	}
	matched := 0
	for _, address := range addresses {
		transfers, e := parseTronReceipt(receipt, input.TxHash, address)
		if e != nil {
			writeError(w, 409, "transaction_unconfirmed", "No successful solidified receipt available")
			return
		}
		for _, transfer := range transfers {
			if e = s.recordUSDTTransfer(r.Context(), transfer); e != nil {
				writeError(w, 503, "usdt_unavailable", "Unable to process transfer")
				return
			}
			matched++
		}
	}
	if err = s.auditWithExecutor(r.Context(), s.db, actorFromContext(r), "usdt.transaction_rechecked", "usdt_transaction", input.TxHash, map[string]any{"transferCount": matched}); err != nil {
		writeError(w, 503, "audit_unavailable", "Unable to record recheck")
		return
	}
	writeJSON(w, 200, map[string]any{"transferCount": matched})
}
