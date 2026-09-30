package auth

import (
	"sync"
	"time"
)

const (
	// maxFailures — сколько неудачных попыток входа разрешено с одного адреса за failureWindow.
	maxFailures = 5

	failureWindow = 10 * time.Minute

	// maxTrackedClients ограничивает число адресов в памяти.
	maxTrackedClients = 10_000
)

// limiter ограничивает число неудачных попыток входа с одного адреса.
type limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	now      func() time.Time
}

// newLimiter создает ограничитель попыток.
func newLimiter() *limiter {
	return &limiter{
		mu:       sync.Mutex{},
		failures: map[string][]time.Time{},
		now:      time.Now,
	}
}

// RetryAfter возвращает, через сколько можно повторить вход с адреса client (0 — можно сейчас).
func (l *limiter) RetryAfter(client string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	failures := l.recent(client)

	if len(failures) < maxFailures {
		return 0
	}

	return failures[0].Add(failureWindow).Sub(l.now())
}

// Fail запоминает неудачную попытку входа.
func (l *limiter) Fail(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.failures) >= maxTrackedClients {
		l.prune()
	}

	l.failures[client] = append(l.recent(client), l.now())
}

// Reset сбрасывает счетчик после успешного входа.
func (l *limiter) Reset(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.failures, client)
}

// recent возвращает попытки клиента за последние failureWindow. Вызывается под блокировкой.
func (l *limiter) recent(client string) []time.Time {
	since := l.now().Add(-failureWindow)
	failures := l.failures[client]

	for len(failures) > 0 && failures[0].Before(since) {
		failures = failures[1:]
	}

	if len(failures) == 0 {
		delete(l.failures, client)

		return nil
	}

	l.failures[client] = failures

	return failures
}

// prune удаляет устаревшие записи, а если их не осталось — все. Вызывается под блокировкой.
func (l *limiter) prune() {
	for client := range l.failures {
		l.recent(client)
	}

	if len(l.failures) >= maxTrackedClients {
		l.failures = map[string][]time.Time{}
	}
}
