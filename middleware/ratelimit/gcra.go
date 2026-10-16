package ratelimit

import "time"

// decide applies the Generic Cell Rate Algorithm to one key.
//
// tat is the key's theoretical arrival time (the zero time for a new key),
// interval the emission interval (period / limit) and capacity the burst
// tolerance (burst × interval). It returns the new TAT, whether the request is
// allowed, and, when it is not, how long until a request would be.
func decide(tat, now time.Time, interval, capacity time.Duration) (time.Time, bool, time.Duration) {
	t := tat
	if t.Before(now) {
		t = now
	}
	next := t.Add(interval)
	if next.Sub(now) <= capacity {
		return next, true, 0
	}
	return tat, false, next.Sub(now) - capacity
}
