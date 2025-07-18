package strategy

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type windUp struct {
	currentRate  float64
	updateEvery  int
	windUpFactor float64

	limiter *rate.Limiter
	mu      sync.Mutex

	isServingPenalty bool
	reqsSinceUpdate  int
	reqsSinceFW      int
	firstReqSinceFW  time.Time
}

const (
	startRate    = 2
	updateEvery  = 2
	windUpFactor = 1.05
	maxRate      = 10
)

func WindUp() Strategy {
	return &windUp{
		currentRate:  startRate,
		updateEvery:  updateEvery,
		windUpFactor: windUpFactor,
		limiter:      rate.NewLimiter(rate.Limit(startRate), 1),
	}
}

func (w *windUp) CurrentRate() float64 {
	return w.currentRate
}

func (w *windUp) Wait() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.reqsSinceUpdate >= w.updateEvery {
		w.updateRate(w.currentRate * w.windUpFactor)
	}
	if w.firstReqSinceFW.IsZero() {
		w.firstReqSinceFW = time.Now()
	}
	_ = w.limiter.Wait(context.Background())
	w.reqsSinceUpdate++
	w.reqsSinceFW++
}

func (w *windUp) FloodWaitEvent(secs int) {
	if w.isServingPenalty {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.isServingPenalty = true
	if !w.firstReqSinceFW.IsZero() && w.reqsSinceFW > 0 {
		reqsDuration := time.Now().Sub(w.firstReqSinceFW).Abs().Seconds()
		w.updateRate(float64(w.reqsSinceFW) / (reqsDuration + float64(secs)))
	} else {
		w.updateRate(startRate)
	}
	time.Sleep(time.Duration(secs) * time.Second)
	w.reqsSinceFW = 0
	w.firstReqSinceFW = time.Time{}
	w.isServingPenalty = false
}

func (w *windUp) updateRate(newRate float64) {
	if newRate > maxRate {
		newRate = maxRate
	}
	w.reqsSinceUpdate = 0
	w.currentRate = newRate
	w.limiter.SetLimit(rate.Limit(newRate))
}
