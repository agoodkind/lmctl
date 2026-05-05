package lmctl

import "time"

// now returns the current wall-clock time. All non-test callers in this
// package use this helper so that [time.Now] is centralized in a single
// clock-injection point. Tests can override [nowFunc] to make
// time-sensitive logic deterministic.
var nowFunc = time.Now

// now returns the current wall-clock time via [nowFunc].
func now() time.Time { return nowFunc() }

// since returns the duration elapsed since t, computed via [now].
func since(t time.Time) time.Duration { return now().Sub(t) }
