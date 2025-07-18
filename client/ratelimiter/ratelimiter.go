package ratelimiter

import (
	"regexp"
	"strconv"

	"github.com/sirupsen/logrus"
	"github.com/ualinker/go-tdlib/client/ratelimiter/strategy"
)

type RateLimiter interface {
	CurrentRate() float64
	Wait()
	TDLibCallback(verbosityLevel int, message string)
}

type rateLimiter struct {
	strategy    strategy.Strategy
	floodWaitRE *regexp.Regexp
}

func NewRateLimiter(strategy strategy.Strategy) RateLimiter {
	return &rateLimiter{
		strategy:    strategy,
		floodWaitRE: regexp.MustCompile(`FLOOD_WAIT_(\d+)`),
	}
}

func (rl *rateLimiter) CurrentRate() float64 {
	return rl.strategy.CurrentRate()
}

func (rl *rateLimiter) Wait() {
	rl.strategy.Wait()
}

func (rl *rateLimiter) TDLibCallback(verbosityLevel int, message string) {
	if verbosityLevel != 2 {
		return
	}
	matches := rl.floodWaitRE.FindStringSubmatch(message)
	if len(matches) < 2 {
		return
	}
	penalty, err := strconv.ParseInt(matches[1], 10, 16)
	if err != nil {
		return
	}
	if penalty >= 120 {
		logrus.Errorf("go-tdlib: FLOOD_WAIT is too big: %d secs", penalty)
	}
	rl.strategy.FloodWaitEvent(int(penalty))
}
