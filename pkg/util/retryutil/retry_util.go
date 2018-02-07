package retryutil

import (
	"fmt"
	"time"
)

type RetryTicker interface {
	Stop()
	Tick()
}

type Ticker struct {
	ticker *time.Ticker
}

func (t *Ticker) Stop() { t.ticker.Stop() }

func (t *Ticker) Tick() { <-t.ticker.C }

// Retry calls ConditionFunc until either:
// * it returns boolean true
// * a timeout expires
// * an error occurs
func Retry(interval time.Duration, timeout time.Duration, f func() (bool, error)) error {
	//TODO: make the retry exponential
	if timeout < interval {
		return fmt.Errorf("timout(%s) should be greater than interval(%v)", timeout, interval)
	}
	tick := &Ticker{time.NewTicker(interval)}
	return RetryWorker(interval, timeout, f, tick)
}

func RetryWorker(
	interval time.Duration,
	timeout time.Duration,
	f func() (bool, error),
	tick RetryTicker) error {

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
