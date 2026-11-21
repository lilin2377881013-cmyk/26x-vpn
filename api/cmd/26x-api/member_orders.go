package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

func (s *server) myOrders(w http.ResponseWriter, r *http.Request) {
	const size = 10
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 1000000 {
			writeError(w, 400, "invalid_page", "订单页码无效")
			return
		}
		page = n
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT o.order_no,p.name,o.created_at,o.payment_channel,o.amount_cents,i.amount_units,
		CASE WHEN o.paid_at IS NOT NULL THEN 'paid' WHEN i.status='review' THEN 'review'
		WHEN i.status='expired' OR COALESCE(i.expires_at,o.checkout_expires_at)<=now() THEN 'expired'
		WHEN o.payment_channel='selection' THEN 'selection' ELSE 'pending' END
		FROM payment_orders o JOIN plans p ON p.id=o.plan_id LEFT JOIN usdt_invoices i ON i.order_no=o.order_no
		WHERE o.user_id=$1 ORDER BY o.created_at DESC,o.order_no DESC LIMIT $2 OFFSET $3`,
		actorFromContext(r).id, size+1, (page-1)*size)
	if err != nil {
		writeError(w, 500, "orders_unavailable", "订单暂时无法读取")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, size+1)
	for rows.Next() {
		var number, plan, channel, status string
		var cents int64
		var units sql.NullInt64
		var created time.Time
		if err := rows.Scan(&number, &plan, &created, &channel, &cents, &units, &status); err != nil {
			writeError(w, 500, "orders_unavailable", "订单暂时无法读取")
			return
		}
		item := map[string]any{"orderNo": number, "plan": plan, "paymentChannel": channel, "status": status, "amountCents": cents, "createdAt": created.UTC()}
		if units.Valid {
			item["usdtAmount"] = formatUSDT(units.Int64)
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		writeError(w, 500, "orders_unavailable", "订单暂时无法读取")
		return
	}
	more := len(items) > size
	if more {
		items = items[:size]
	}
	writeJSON(w, 200, map[string]any{"items": items, "hasMore": more})
}
