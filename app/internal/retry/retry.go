package retry

import (
	"context"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"math"
	"time"
)

type Retryer struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (r Retryer) Do(ctx context.Context, opName string, fn func() error, retriable func(error) bool) error {
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = 5
	}
	if r.BaseDelay <= 0 {
		r.BaseDelay = 500 * time.Millisecond
	}
	if r.MaxDelay <= 0 {
		r.MaxDelay = 10 * time.Second
	}

	var attempt int
	for {
		attempt++
		err := fn()
		if err == nil {
			return nil
		}
		if !retriable(err) || attempt >= r.MaxAttempts {
			return fmt.Errorf("%s failed after %d attempt(s): %w", opName, attempt, err)
		}
		backoff := time.Duration(float64(r.BaseDelay) * math.Pow(2, float64(attempt-1)))
		if backoff > r.MaxDelay {
			backoff = r.MaxDelay
		}
		jitter := time.Duration(float64(backoff) * (0.2 * (0.5 - randFloat())))
		sleep := backoff + jitter

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}
	}
}

func randFloat() float64 {
	now := time.Now().UnixNano()
	seed := uint64(now*1103515245 + 12345)
	return float64(seed%1000) / 1000.0
}

func IsRetriable(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var respErr *azcore.ResponseError
	if errors.As(err, &respErr) {
		//Retry in following case
		// 408 - Request Timeout
		// 429 - Too Many Requests
		// 5xx - Server Error
		if respErr.StatusCode == 408 || respErr.StatusCode == 429 || (respErr.StatusCode >= 500 && respErr.StatusCode <= 599) {
			return true
		}
		return false
	}
	return true
}
