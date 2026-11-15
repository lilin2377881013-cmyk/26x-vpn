package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCashierRedirect(t *testing.T) {
	const cashier = "http://cashier.example:56001"
	g := &paymentGateway{cashierOrigin: cashier, notifyURL: "https://merchant.example" + paymentNotifyPath, secret: "test"}
	s := &server{cfg: config{payment: g}}
	expires := time.Now().Add(time.Minute).UnixMilli()
	address := cashier + "/order/123"
	entry := g.checkoutURL(address, expires)
	if !validPaymentURL(entry) || g.checkoutURL("http://other.example/order", expires) != "" {
		t.Fatal("wrong cashier URL validation")
	}
	for _, test := range []struct {
		link string
		code int
	}{
		{entry, http.StatusFound},
		{entry + "x", http.StatusBadRequest},
		{g.checkoutURL(address, time.Now().Add(-time.Minute).UnixMilli()), http.StatusGone},
	} {
		result := httptest.NewRecorder()
		s.paymentCashier(result, httptest.NewRequest(http.MethodGet, test.link, nil))
		if result.Code != test.code || test.code == http.StatusFound && result.Header().Get("Location") != address {
			t.Errorf("unexpected redirect status %d (expected %d)", result.Code, test.code)
		}
	}
}
