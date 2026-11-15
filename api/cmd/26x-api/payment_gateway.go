package main

import (
	"bytes"
	"context"
	"crypto/md5" // The gateway's documented wire protocol requires MD5.
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const paymentNotifyPath = "/api/v1/payments/juliang/notify"

type paymentGateway struct {
	apiOrigin, merchant, secret, product, notifyURL string
	client                                          *http.Client
	callbackIPs                                     map[string]struct{}
}

func loadPaymentGateway() (*paymentGateway, error) {
	if os.Getenv("JULIANG_PRODUCT") == "" {
		return nil, nil
	}
	g := &paymentGateway{
		apiOrigin: "https://tsapi-juliangpay.tszfaa.com",
		merchant:  os.Getenv("JULIANG_MERCHANT"),
		secret:    os.Getenv("JULIANG_SECRET"),
		product:   os.Getenv("JULIANG_PRODUCT"),
		notifyURL: os.Getenv("JULIANG_NOTIFY_URL"),
		client:    &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	notify, err := url.Parse(g.notifyURL)
	if err != nil || !validPaymentURL(g.notifyURL) || notify.Path != paymentNotifyPath ||
		g.merchant == "" || g.secret == "" || g.product == "P0000" {
		return nil, errors.New("invalid JULIANG payment configuration")
	}
	g.callbackIPs = make(map[string]struct{})
	for _, entry := range strings.Split(os.Getenv("JULIANG_CALLBACK_IPS"), ",") {
		ip, err := netip.ParseAddr(strings.TrimSpace(entry))
		if err != nil {
			return nil, errors.New("invalid JULIANG_CALLBACK_IPS")
		}
		g.callbackIPs[ip.Unmap().String()] = struct{}{}
	}
	return g, nil
}

func validPaymentURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

func gatewaySignature(fields map[string]string, secret string) string {
	keys := make([]string, 0, len(fields))
	for key, value := range fields {
		if key != "sign" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, key+"="+fields[key])
	}
	digest := md5.Sum([]byte(strings.Join(pairs, "&") + secret))
	return hex.EncodeToString(digest[:])
}

func (g *paymentGateway) verify(fields map[string]string) bool {
	return subtle.ConstantTimeCompare([]byte(strings.ToLower(fields["sign"])), []byte(gatewaySignature(fields, g.secret))) == 1
}

type gatewayOrder struct {
	Merchant   string `json:"mchKey"`
	Number     string `json:"mchOrderNo"`
	Serial     string `json:"serialOrderNo"`
	Product    string `json:"product"`
	Amount     int64  `json:"amount"`
	RealAmount int64  `json:"realAmount"`
	Status     string `json:"payStatus"`
	URL        struct {
		PayURL string `json:"payUrl"`
		Expire int64  `json:"expire"`
	} `json:"url"`
}

func (g *paymentGateway) request(ctx context.Context, method, path, order string, amount int64) (gatewayOrder, error) {
	var result gatewayOrder
	nonce, err := randomToken(18)
	if err != nil {
		return result, err
	}
	fields := map[string]string{"mchKey": g.merchant, "mchOrderNo": order, "nonce": nonce, "timestamp": strconv.FormatInt(time.Now().UnixMilli(), 10)}
	endpoint := g.apiOrigin + "/api/v1/payment/" + path
	var body io.Reader
	if method == http.MethodPost {
		fields["product"], fields["amount"], fields["notifyUrl"] = g.product, strconv.FormatInt(amount, 10), g.notifyURL
		fields["sign"] = gatewaySignature(fields, g.secret)
		payload := make(map[string]any, len(fields))
		for k, v := range fields {
			payload[k] = v
		}
		payload["amount"] = amount
		raw, err := json.Marshal(payload)
		if err != nil {
			return result, err
		}
		body = bytes.NewReader(raw)
	} else {
		fields["sign"] = gatewaySignature(fields, g.secret)
		query := make(url.Values, len(fields))
		for key, value := range fields {
			query.Set(key, value)
		}
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return result, errors.New("cannot build payment request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return result, errors.New("payment gateway unavailable")
	}
	defer resp.Body.Close()
	var envelope struct {
		Code json.Number  `json:"code"`
		Data gatewayOrder `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&envelope); err != nil {
		return result, fmt.Errorf("payment gateway response HTTP %d could not be decoded", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || envelope.Code.String() != "200" {
		return result, fmt.Errorf("payment gateway rejected request (HTTP %d, code %q)", resp.StatusCode, envelope.Code.String())
	}
	return envelope.Data, nil
}
