package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const usdtContract = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
const usdtUnit int64 = 1000000

var decimalValue = regexp.MustCompile(`^[0-9]{1,9}(\.[0-9]{1,6})?$`)
var tronHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

type usdtGateway struct {
	accepting                          bool
	testMode                           bool
	address, origin, apiOrigin, apiKey string
	prices                             map[string]int64
	cnyRate                            *big.Rat
	client                             *http.Client
}

func loadUSDTGateway() (*usdtGateway, error) {
	enabled, err := strconv.ParseBool(env("USDT_ACCEPT_PAYMENTS", "false"))
	if err != nil {
		return nil, errors.New("invalid USDT_ACCEPT_PAYMENTS")
	}
	address := strings.TrimSpace(os.Getenv("USDT_RECEIVE_ADDRESS"))
	if address == "" && !enabled {
		return nil, nil
	}
	if _, err = tronAddressHex(address); err != nil {
		return nil, errors.New("invalid USDT_RECEIVE_ADDRESS")
	}
	origin := strings.TrimRight(os.Getenv("USDT_PUBLIC_ORIGIN"), "/")
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("USDT_PUBLIC_ORIGIN must be an HTTPS origin")
	}
	g := &usdtGateway{accepting: enabled, address: address, origin: origin, apiOrigin: "https://api.trongrid.io", apiKey: os.Getenv("TRONGRID_API_KEY"), prices: map[string]int64{}, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if value := os.Getenv("USDT_PLAN_PRICES"); value != "" {
		var prices map[string]string
		if json.Unmarshal([]byte(value), &prices) != nil {
			return nil, errors.New("USDT_PLAN_PRICES must map plan codes to decimal strings")
		}
		for code, value := range prices {
			units, e := parseUSDT(value)
			if e != nil || units <= 0 || units > 100000*usdtUnit {
				return nil, errors.New("invalid USDT plan price")
			}
			g.prices[code] = units
		}
	}
	if rate := os.Getenv("USDT_CNY_RATE"); rate != "" {
		if !decimalValue.MatchString(rate) {
			return nil, errors.New("invalid USDT_CNY_RATE")
		}
		g.cnyRate, _ = new(big.Rat).SetString(rate)
		if g.cnyRate.Sign() <= 0 {
			return nil, errors.New("invalid USDT_CNY_RATE")
		}
	}
	if enabled && (g.apiKey == "" || (len(g.prices) == 0 && g.cnyRate == nil)) {
		return nil, errors.New("USDT accepting requires TRONGRID_API_KEY and explicit pricing")
	}
	return g, nil
}

func parseUSDT(value string) (int64, error) {
	if !decimalValue.MatchString(value) {
		return 0, errors.New("invalid decimal amount")
	}
	parts := strings.SplitN(value, ".", 2)
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	fraction += strings.Repeat("0", 6-len(fraction))
	return strconv.ParseInt(parts[0]+fraction, 10, 64)
}
func formatUSDT(units int64) string { return fmt.Sprintf("%d.%06d", units/usdtUnit, units%usdtUnit) }
func (g *usdtGateway) quote(code string, cents int64) (int64, error) {
	if value, ok := g.prices[code]; ok && value > 0 {
		return value, nil
	}
	if g.cnyRate == nil || cents <= 0 {
		return 0, errors.New("USDT price not configured")
	}
	value := new(big.Rat).Quo(new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(cents), big.NewInt(usdtUnit)), big.NewInt(100)), g.cnyRate)
	n, rem := new(big.Int), new(big.Int)
	n.QuoRem(value.Num(), value.Denom(), rem)
	if rem.Sign() > 0 {
		n.Add(n, big.NewInt(1))
	}
	if !n.IsInt64() || n.Sign() <= 0 || n.Int64() > 100000*usdtUnit {
		return 0, errors.New("USDT quote outside allowed range")
	}
	return n.Int64(), nil
}

func tronAddressHex(address string) (string, error) {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	if len(address) != 34 || !strings.HasPrefix(address, "T") {
		return "", errors.New("invalid TRON address")
	}
	n := new(big.Int)
	for _, c := range address {
		i := strings.IndexRune(alphabet, c)
		if i < 0 {
			return "", errors.New("invalid base58")
		}
		n.Mul(n, big.NewInt(58))
		n.Add(n, big.NewInt(int64(i)))
	}
	b := n.Bytes()
	if len(b) != 25 || b[0] != 0x41 {
		return "", errors.New("invalid TRON address length")
	}
	first := sha256.Sum256(b[:21])
	second := sha256.Sum256(first[:])
	if string(b[21:]) != string(second[:4]) {
		return "", errors.New("invalid TRON checksum")
	}
	return hex.EncodeToString(b[1:21]), nil
}
