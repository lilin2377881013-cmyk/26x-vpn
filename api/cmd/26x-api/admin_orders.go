package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

func (s *server) listPaymentOrders(w http.ResponseWriter, r *http.Request) {
	page := 1
	if value := r.URL.Query().Get("page"); value != "" {
		var err error
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 || page > 1000000 {
			writeError(w, http.StatusBadRequest, "invalid_page", "页码无效")
			return
		}
	}
	const pageSize = 50
	rows, err := s.db.QueryContext(r.Context(), `SELECT o.order_no, u.email::text, p.name,
		o.product, o.amount_cents, o.created_at, o.paid_at, o.provider_order_no
		FROM payment_orders o JOIN users u ON u.id=o.user_id JOIN plans p ON p.id=o.plan_id
		ORDER BY o.created_at DESC, o.order_no DESC LIMIT $1 OFFSET $2`, pageSize+1, (page-1)*pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "orders_unavailable", "无法读取订单")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, pageSize+1)
	for rows.Next() {
		var order, email, plan, product string
		var amount int64
		var created time.Time
		var paid sql.NullTime
		var serial sql.NullString
		if err := rows.Scan(&order, &email, &plan, &product, &amount, &created, &paid, &serial); err != nil {
			writeError(w, http.StatusInternalServerError, "orders_unavailable", "无法读取订单")
			return
		}
		status := "created"
		if paid.Valid {
			status = "paid"
		} else if serial.Valid {
			status = "unconfirmed"
		}
		items = append(items, map[string]any{"orderNo": order, "email": email, "plan": plan, "product": product,
			"amountCents": amount, "createdAt": created.UTC().Format(time.RFC3339), "paidAt": nullableTime(paid),
			"providerOrderNo": nullableString(serial), "status": status})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "orders_unavailable", "无法读取订单")
		return
	}
	more := len(items) > pageSize
	if more {
		items = items[:pageSize]
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "hasMore": more})
}
