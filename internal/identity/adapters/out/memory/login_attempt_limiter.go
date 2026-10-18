package memory

import (
	"strings"
	"sync"
	"time"
)

// Login throttling policy (DEC-05): after freeAttempts failures in a row,
// each further attempt waits firstBackoff, doubling per failure up to
// maxBackoff. A success resets the counters. State lives in memory only; a
// restart forgets it, which is acceptable for back-off.
const (
	freeAttempts  = 5
	firstBackoff  = time.Second
	maxBackoff    = 15 * time.Minute
	entryLifetime = time.Hour
	// maxEntries bounds memory: usernames and IPs come from attackers.
	maxEntries = 10000
)

type failureRecord struct {
	failures    int
	lastFailure time.Time
}

// MemoryLoginAttemptLimiter tracks failures per username and per client IP;
// an attempt waits for whichever of the two is throttled longer.
type MemoryLoginAttemptLimiter struct {
	mu      sync.Mutex
	records map[string]failureRecord
}

func NewMemoryLoginAttemptLimiter() *MemoryLoginAttemptLimiter {
	return &MemoryLoginAttemptLimiter{records: map[string]failureRecord{}}
}

func (l *MemoryLoginAttemptLimiter) RetryAfter(username, clientIP string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, key := range keys(username, clientIP) {
		wait = max(wait, l.waitFor(key, now))
	}
	return wait
}

func (l *MemoryLoginAttemptLimiter) RecordFailure(username, clientIP string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	for _, key := range keys(username, clientIP) {
		record := l.records[key]
		record.failures++
		record.lastFailure = now
		l.records[key] = record
	}
}

func (l *MemoryLoginAttemptLimiter) RecordSuccess(username, clientIP string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys(username, clientIP) {
		delete(l.records, key)
	}
}

func (l *MemoryLoginAttemptLimiter) waitFor(key string, now time.Time) time.Duration {
	record, ok := l.records[key]
	if !ok || record.failures < freeAttempts {
		return 0
	}
	backoff := firstBackoff << min(record.failures-freeAttempts, 20)
	backoff = min(backoff, maxBackoff)
	return max(record.lastFailure.Add(backoff).Sub(now), 0)
}

// prune drops stale entries, and the oldest ones when the map is full.
func (l *MemoryLoginAttemptLimiter) prune(now time.Time) {
	for key, record := range l.records {
		if now.Sub(record.lastFailure) > entryLifetime {
			delete(l.records, key)
		}
	}
	for len(l.records) >= maxEntries {
		var oldestKey string
		var oldest time.Time
		for key, record := range l.records {
			if oldestKey == "" || record.lastFailure.Before(oldest) {
				oldestKey, oldest = key, record.lastFailure
			}
		}
		delete(l.records, oldestKey)
	}
}

func keys(username, clientIP string) []string {
	result := []string{"user:" + strings.ToLower(strings.TrimSpace(username))}
	if clientIP != "" {
		result = append(result, "ip:"+clientIP)
	}
	return result
}
