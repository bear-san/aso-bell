// Package testutil は Manager のテストで共有する固定時刻・連番 ID・MongoDB 起動ヘルパーを提供する。
package testutil

import (
	"fmt"
	"sync"
	"time"
)

// FakeClock は Now() を固定し、Advance / Set で明示的に進める時計。
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock は now を初期時刻とする FakeClock を返す。
func NewFakeClock(now time.Time) *FakeClock {
	return &FakeClock{now: now}
}

// Now は現在の固定時刻を返す。
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance は時刻を d だけ進める。
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set は時刻を t に置き換える。
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// FakeIDs は prefix 付きの連番 ID を生成する。
type FakeIDs struct {
	mu     sync.Mutex
	prefix string
	n      int
}

// NewFakeIDs は prefix を持つ FakeIDs を返す。
func NewFakeIDs(prefix string) *FakeIDs {
	return &FakeIDs{prefix: prefix}
}

// New は次の ID(例 "evt-1")を返す。
func (f *FakeIDs) New() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return fmt.Sprintf("%s-%d", f.prefix, f.n)
}
