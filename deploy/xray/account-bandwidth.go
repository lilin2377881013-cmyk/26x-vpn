package dispatcher

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

type bandwidthRule struct {
	Account string `json:"account"`
	Mbps    int    `json:"mbps"`
}
type bandwidthConfig struct {
	Users    map[string]bandwidthRule `json:"users"`
	NodeMbps int                      `json:"nodeMbps"`
}

var bandwidth = struct {
	sync.Mutex
	nextRead time.Time
	config   bandwidthConfig
	accounts map[string]*rate.Limiter
	node     *rate.Limiter
}{accounts: make(map[string]*rate.Limiter), node: rate.NewLimiter(rate.Inf, 64*1024)}

func bandwidthLimiters(label string) (*rate.Limiter, *rate.Limiter) {
	bandwidth.Lock()
	defer bandwidth.Unlock()
	if now := time.Now(); !now.Before(bandwidth.nextRead) {
		bandwidth.nextRead = now.Add(2 * time.Second)
		if raw, err := os.ReadFile("/etc/26x/bandwidth.json"); err == nil {
			var cfg bandwidthConfig
			if json.Unmarshal(raw, &cfg) == nil && cfg.Users != nil {
				bandwidth.config = cfg
				nodeRate := rate.Inf
				if cfg.NodeMbps > 0 {
					nodeRate = rate.Limit(float64(cfg.NodeMbps) * 125000)
				}
				bandwidth.node.SetLimitAt(now, nodeRate)
				for _, rule := range cfg.Users {
					if limit := bandwidth.accounts[rule.Account]; limit != nil && rule.Mbps > 0 {
						limit.SetLimitAt(now, rate.Limit(float64(rule.Mbps)*125000))
					}
				}
			}
		}
	}
	rule := bandwidth.config.Users[label]
	if rule.Account == "" || rule.Mbps < 1 {
		rule = bandwidthRule{Account: label, Mbps: 20}
	}
	limit := bandwidth.accounts[rule.Account]
	if limit == nil {
		limit = rate.NewLimiter(rate.Limit(float64(rule.Mbps)*125000), 64*1024)
		bandwidth.accounts[rule.Account] = limit
	}
	return limit, bandwidth.node
}

type bandwidthWriter struct {
	ctx    context.Context
	label  string
	writer buf.Writer
}

func (w *bandwidthWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	account, node := bandwidthLimiters(w.label)
	for remaining := int(mb.Len()); remaining > 0; {
		chunk := min(remaining, 64*1024)
		if err := account.WaitN(w.ctx, chunk); err != nil {
			buf.ReleaseMulti(mb)
			return err
		}
		if err := node.WaitN(w.ctx, chunk); err != nil {
			buf.ReleaseMulti(mb)
			return err
		}
		remaining -= chunk
	}
	return w.writer.WriteMultiBuffer(mb)
}
func (w *bandwidthWriter) Close() error { return common.Close(w.writer) }
func (w *bandwidthWriter) Interrupt()   { common.Interrupt(w.writer) }
