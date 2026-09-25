package httpx

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimiter — ограничитель частоты по ключу (IP, email) с фиксированным окном.
//
// Защищает формы входа, регистрации и обратной связи от перебора паролей и
// спама. Счётчики живут в памяти процесса: при нескольких экземплярах
// приложения лимит действует на каждый отдельно — для одной инсталляции этого
// достаточно, общий счётчик понадобится вместе с балансировщиком.
type RateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu    sync.Mutex
	hits  map[string]*rateWindow
	calls int
}

type rateWindow struct {
	start time.Time
	count int
}

// NewRateLimiter разрешает не больше limit событий на ключ за window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:  limit,
		window: window,
		now:    time.Now,
		hits:   make(map[string]*rateWindow),
	}
}

// Allow учитывает событие и сообщает, укладывается ли ключ в лимит.
func (l *RateLimiter) Allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	// Периодически выбрасываем истёкшие окна, иначе карта растёт бесконечно
	// от ключей, которые больше не появляются.
	l.calls++
	if l.calls%1000 == 0 {
		for k, w := range l.hits {
			if now.Sub(w.start) >= l.window {
				delete(l.hits, k)
			}
		}
	}

	w, ok := l.hits[key]
	if !ok || now.Sub(w.start) >= l.window {
		l.hits[key] = &rateWindow{start: now, count: 1}
		return true
	}
	if w.count >= l.limit {
		return false
	}
	w.count++
	return true
}

// ClientIP — адрес клиента. RemoteAddr уже подменён middleware.RealIP на
// адрес из X-Real-IP / X-Forwarded-For: приложение слушает только localhost
// за nginx, поэтому этим заголовкам можно доверять.
func ClientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
