package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"
)

func (s *server) checkout(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PlanCode string `json:"planCode"`
	}
	if decodeJSON(w, r, &input) != nil || input.PlanCode == "" || len(input.PlanCode) > 80 {
		writeError(w, http.StatusBadRequest, "invalid_plan", "请选择套餐")
		return
	}
	if s.cfg.usdt != nil && s.cfg.usdt.accepting {
		s.checkoutSelection(w, r, input.PlanCode)
		return
	}
	g := s.cfg.payment
	if g != nil && g.maintenance {
		writeError(w, http.StatusServiceUnavailable, "payment_maintenance", "支付宝维护中，请稍后重试")
		return
	}
	if g == nil {
		writeError(w, http.StatusServiceUnavailable, "payment_unavailable", "支付暂未开放")
		return
	}
	ctx, userID := r.Context(), actorFromContext(r).id
	var trial, unlimited bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM trial_accounts WHERE user_id=$1),
		EXISTS (SELECT 1 FROM subscriptions WHERE user_id=$1 AND status='active' AND ends_at IS NULL)`, userID).Scan(&trial, &unlimited)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checkout_failed", "无法读取用户套餐")
		return
	}
	if trial {
		writeError(w, http.StatusConflict, "trial_checkout", "请先登录正式账号再购买")
		return
	}
	if unlimited {
		writeError(w, http.StatusConflict, "unlimited_subscription", "当前账号已有永久会员")
		return
	}
	token, err := randomToken(18)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checkout_failed", "无法创建订单")
		return
	}
	orderNo := "26x_" + token
	var amount int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO payment_orders
		(order_no, user_id, plan_id, product, amount_cents, duration_days)
		SELECT $1, $2, id, $3, price_cents, duration_days FROM plans
		WHERE code=$4 AND active AND currency='CNY' AND price_cents BETWEEN 2000 AND 200000
		RETURNING amount_cents`, orderNo, userID, g.product, input.PlanCode).Scan(&amount)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "plan_unavailable", "支付金额须在 ¥20 至 ¥2000 之间，请选择可购买的套餐")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checkout_failed", "无法创建订单")
		return
	}
	provider, err := g.request(ctx, http.MethodPost, "init", orderNo, amount)
	if err != nil {
		s.logger.Warn("payment init failed", "order", orderNo, "error", err)
		writeError(w, http.StatusBadGateway, "checkout_failed", "无法打开支付页面，请稍后重试")
		return
	}
	payURL := g.checkoutURL(provider.URL.PayURL, provider.URL.Expire)
	if !g.matches(provider, orderNo, g.product, amount) || provider.Status != "PROCESSING" ||
		payURL == "" || provider.URL.Expire <= time.Now().UnixMilli() {
		s.logger.Warn("unexpected payment init response", "order", orderNo)
		writeError(w, http.StatusBadGateway, "checkout_failed", "支付链接不可用")
		return
	}
	result, err := s.db.ExecContext(ctx, `UPDATE payment_orders SET provider_order_no=$2
		WHERE order_no=$1 AND (provider_order_no IS NULL OR provider_order_no=$2)`, orderNo, provider.Serial)
	if err == nil {
		var updated int64
		updated, err = result.RowsAffected()
		if updated != 1 && err == nil {
			err = errors.New("payment order could not be matched")
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checkout_failed", "无法保存订单")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"orderNo": orderNo, "payUrl": payURL,
		"expiresAt": time.UnixMilli(provider.URL.Expire).UTC().Format(time.RFC3339)})
}

func (g *paymentGateway) matches(order gatewayOrder, number, product string, amount int64) bool {
	return order.Merchant == g.merchant && order.Number == number && order.Product == product &&
		order.Amount == amount && order.Serial != ""
}

func (s *server) paymentNotify(w http.ResponseWriter, r *http.Request) {
	g := s.cfg.payment
	if g == nil {
		http.Error(w, "payment unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, allowed := g.callbackIPs[clientIP(r, s.cfg.trustedProxies)]; !allowed {
		http.Error(w, "invalid sender", http.StatusForbidden)
		return
	}
	var payload map[string]json.RawMessage
	if decodeJSON(w, r, &payload) != nil {
		http.Error(w, "invalid notification", http.StatusBadRequest)
		return
	}
	fields := make(map[string]string, len(payload))
	for name, value := range payload {
		var field string
		if err := json.Unmarshal(value, &field); err != nil {
			var integer json.Number
			if json.Unmarshal(value, &integer) != nil {
				http.Error(w, "invalid notification", http.StatusBadRequest)
				return
			}
			field = integer.String()
		}
		fields[name] = field
	}
	orderNo, serial := fields["mchOrderNo"], fields["serialOrderNo"]
	amount, amountErr := strconv.ParseInt(fields["amount"], 10, 64)
	realAmount, realAmountErr := strconv.ParseInt(fields["realAmount"], 10, 64)
	if !g.verify(fields) || fields["mchKey"] != g.merchant || fields["payStatus"] != "SUCCESS" ||
		orderNo == "" || serial == "" || amountErr != nil || realAmountErr != nil || amount <= 0 || amount != realAmount {
		http.Error(w, "invalid notification", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	var product string
	var expectedAmount int64
	var savedSerial, subscription sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT product, amount_cents, provider_order_no, subscription_id::text
		FROM payment_orders WHERE order_no=$1`, orderNo).Scan(&product, &expectedAmount, &savedSerial, &subscription)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unknown order", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "order unavailable", http.StatusServiceUnavailable)
		return
	}
	if expectedAmount != amount || product == "P0000" || (savedSerial.Valid && savedSerial.String != serial) {
		http.Error(w, "payment does not match order", http.StatusBadRequest)
		return
	}
	if !subscription.Valid {
		confirmed, err := g.request(ctx, http.MethodGet, "query", orderNo, 0)
		if err != nil {
			s.logger.Warn("payment verification failed", "order", orderNo, "error", err)
			http.Error(w, "unable to verify payment", http.StatusServiceUnavailable)
			return
		}
		if !g.matches(confirmed, orderNo, product, amount) || confirmed.Serial != serial ||
			confirmed.Status != "SUCCESS" || confirmed.RealAmount != amount {
			http.Error(w, "payment not confirmed", http.StatusConflict)
			return
		}
		if err := s.grantPayment(r, orderNo, serial); err != nil {
			s.logger.Error("payment subscription grant failed", "order", orderNo, "error", err)
			http.Error(w, "unable to apply payment", http.StatusServiceUnavailable)
			return
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("SUCCESS"))
}

func (s *server) grantPayment(r *http.Request, orderNo, serial string) error {
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.grantOrderTx(r.Context(), tx, orderNo, serial, "alipay"); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *server) grantOrderTx(ctx context.Context, tx *sql.Tx, orderNo, serial, channel string) error {
	var savedChannel string
	var userID, planID string
	var duration int
	var existingSerial, subscription sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT user_id::text, plan_id::text, duration_days, provider_order_no, subscription_id::text, payment_channel
		FROM payment_orders WHERE order_no=$1 FOR UPDATE`, orderNo).
		Scan(&userID, &planID, &duration, &existingSerial, &subscription, &savedChannel)
	if err != nil {
		return err
	}
	if savedChannel != channel {
		return errors.New("payment channel mismatch")
	}
	if existingSerial.Valid && existingSerial.String != serial {
		return errors.New("payment provider order number changed")
	}
	if subscription.Valid {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&userID); err != nil {
		return err
	}
	var startsAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT GREATEST(clock_timestamp(), max(ends_at))
		FROM subscriptions WHERE user_id=$1 AND status='active'`, userID).Scan(&startsAt)
	if err != nil {
		return err
	}
	var id string
	if err := tx.QueryRowContext(ctx, `INSERT INTO subscriptions (user_id, plan_id, status, starts_at, ends_at)
		VALUES ($1, $2, 'active', $3, $4) RETURNING id::text`, userID, planID, startsAt,
		startsAt.AddDate(0, 0, duration)).Scan(&id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_orders SET provider_order_no=$2, subscription_id=$3, paid_at=now()
		WHERE order_no=$1`, orderNo, serial, id); err != nil {
		return err
	}
	return nil
}
