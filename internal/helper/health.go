package helper

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"github.com/bestruirui/octopus/internal/model"
)

// ClassifyError takes (statusCode, err) where statusCode==0 means "no HTTP
// response received". A 2xx status with err==nil is treated as success.
func ClassifyError(statusCode int, err error) model.ErrorClass {
	if err == nil && statusCode >= 200 && statusCode < 300 {
		return model.ErrorClassNone
	}
	if statusCode == 401 || statusCode == 403 || statusCode == 429 {
		return model.ErrorClassAuthOrQuota
	}
	if statusCode >= 500 && statusCode < 600 {
		return model.ErrorClassUpstreamError
	}
	if statusCode >= 400 && statusCode < 500 {
		return model.ErrorClassUpstreamError
	}
	if err != nil {
		if isNetworkError(err) {
			return model.ErrorClassNetwork
		}
		// Caller passes statusCode=200 + err for parse / decode failures on a
		// 2xx response; treat those as upstream_error.
		if statusCode >= 200 && statusCode < 300 {
			return model.ErrorClassUpstreamError
		}
		return model.ErrorClassOther
	}
	return model.ErrorClassOther
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "eof") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "lookup ") {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var sysErr syscall.Errno
	if errors.As(err, &sysErr) {
		return true
	}
	return false
}

// DeriveHealth computes the per-channel health bucket from the latest
// per-(key,model) probe results.
//
//   - no results            -> unknown
//   - all ok                -> alive
//   - some ok, some fail    -> flaky
//   - all fail, any network -> dead
//   - all fail, no network  -> zombie
func DeriveHealth(results []model.ChannelKeyModelStatus) model.ChannelHealth {
	if len(results) == 0 {
		return model.ChannelHealthUnknown
	}
	okCount := 0
	failCount := 0
	networkFails := 0
	for _, r := range results {
		if r.OK {
			okCount++
			continue
		}
		failCount++
		if r.ErrorClass == model.ErrorClassNetwork {
			networkFails++
		}
	}
	if failCount == 0 {
		return model.ChannelHealthAlive
	}
	if okCount > 0 {
		return model.ChannelHealthFlaky
	}
	if networkFails > 0 {
		return model.ChannelHealthDead
	}
	return model.ChannelHealthZombie
}
