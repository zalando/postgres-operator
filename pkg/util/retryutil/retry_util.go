package retryutil

import (
	"fmt"
	"time"
)

// RetryTicker is a wrapper aroung time.Tick,
// that allows to mock its implementation
type RetryTicker interface {
	Stop()
	Tick()
}

// Ticker is a real implementation of RetryTicker interface
type Ticker struct {
	ticker *time.Ticker
}

// Stop is a convenience wrapper around ticker.Stop
func (t *Ticker) Stop() { t.ticker.Stop() }

// Tick is a convenience wrapper around ticker.C
func (t *Ticker) Tick() { <-t.ticker.C }

// Retry is a wrapper around RetryWorker that provides a real RetryTicker
func Retry(interval time.Duration, timeout time.Duration, f func() (bool, error)) error {
	//TODO: make the retry exponential
	if timeout < interval {
		return fmt.Errorf("timout(%s) should be greater than interval(%v)", timeout, interval)
	}
	tick := &Ticker{time.NewTicker(interval)}
	return RetryWorker(interval, timeout, tick, f)
}

// RetryWorker calls ConditionFunc until either:
// * it returns boolean true
// * a timeout expires
// * an error occurs
func RetryWorker(
	interval time.Duration,
	timeout time.Duration,
	tick RetryTicker,
	f func() (bool, error)) error {

	maxRetries := int(timeout / interval)
	defer tick.Stop()

	for i := 0; ; i++ {
		ok, err := f()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if i+1 == maxRetries {
			break
		}
		tick.Tick()
	}
	return fmt.Errorf("still failing after %d retries", maxRetries)
}
