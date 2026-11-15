package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"time"
)

const paymentRedirectPath = "/api/v1/payments/juliang/cashier"

type cashierTicket struct {
	Address string `json:"url"`
	Expires int64  `json:"expires"`
}

func (g *paymentGateway) allowedCashier(address string) bool {
	u, err := url.Parse(address)
	return err == nil && u.User == nil && u.Scheme == "http" && u.Host != "" &&
		g.cashierOrigin == u.Scheme+"://"+u.Host
}

func (g *paymentGateway) cashierSignature(ticket string) []byte {
	mac := hmac.New(sha256.New, []byte(g.secret))
	mac.Write([]byte("26x-cashier-v1:" + ticket))
	return mac.Sum(nil)
}

func (g *paymentGateway) checkoutURL(address string, expires int64) string {
	if validPaymentURL(address) {
		return address
	}
	if !g.allowedCashier(address) {
		return ""
	}
	payload, _ := json.Marshal(cashierTicket{Address: address, Expires: expires})
	ticket := base64.RawURLEncoding.EncodeToString(payload)
	signature := base64.RawURLEncoding.EncodeToString(g.cashierSignature(ticket))
	redirect, _ := url.Parse(g.notifyURL)
	redirect.Path = paymentRedirectPath
	redirect.RawPath, redirect.Fragment = "", ""
	redirect.RawQuery = url.Values{"ticket": {ticket}, "signature": {signature}}.Encode()
	return redirect.String()
}

func (s *server) paymentCashier(w http.ResponseWriter, r *http.Request) {
	g := s.cfg.payment
	if g == nil {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	ticket := query.Get("ticket")
	sig, err := base64.RawURLEncoding.DecodeString(query.Get("signature"))
	if err != nil || len(ticket) > 8192 || !hmac.Equal(sig, g.cashierSignature(ticket)) {
		http.Error(w, "Invalid payment link", http.StatusBadRequest)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(ticket)
	var details cashierTicket
	if err != nil || json.Unmarshal(raw, &details) != nil || !g.allowedCashier(details.Address) {
		http.Error(w, "Invalid payment link", http.StatusBadRequest)
		return
	}
	if details.Expires <= time.Now().UnixMilli() {
		http.Error(w, "支付链接已过期，请返回 26x 重新下单", http.StatusGone)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, details.Address, http.StatusFound)
}
