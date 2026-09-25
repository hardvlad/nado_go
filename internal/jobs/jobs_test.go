package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestPermanent(t *testing.T) {
	base := errors.New("нет токена")
	if !IsPermanent(Permanent(base)) {
		t.Error("Permanent должна распознаваться IsPermanent")
	}
	if IsPermanent(base) {
		t.Error("обычная ошибка не должна считаться permanent")
	}
	if !errors.Is(Permanent(base), base) {
		t.Error("Permanent должна разворачиваться до исходной ошибки")
	}
}

func TestBackoffGrows(t *testing.T) {
	prev := time.Duration(0)
	for a := 1; a <= 6; a++ {
		d := backoff(a)
		if d <= 0 || d > time.Hour+time.Minute {
			t.Fatalf("backoff(%d) = %v вне разумных границ", a, d)
		}
		if a > 1 && d < prev {
			t.Errorf("backoff должен расти: backoff(%d)=%v < backoff(%d)=%v", a, d, a-1, prev)
		}
		prev = d
	}
}

func TestPayloadJSON(t *testing.T) {
	if got, _ := payloadJSON(nil); string(got) != "{}" {
		t.Errorf("nil payload = %q, ожидалось {}", got)
	}
	got, err := payloadJSON(map[string]int{"connection_id": 42})
	if err != nil || string(got) != `{"connection_id":42}` {
		t.Errorf("payloadJSON = %q, %v", got, err)
	}
	raw := json.RawMessage(`{"a":1}`)
	if got, _ := payloadJSON(raw); string(got) != `{"a":1}` {
		t.Errorf("RawMessage должен проходить как есть, получено %q", got)
	}
}

// fakeStore — очередь в памяти для теста Runner без БД.
type fakeStore struct {
	mu        sync.Mutex
	pending   []*Job
	completed map[int64][]byte
	failed    map[int64]string
}

func newFakeStore() *fakeStore {
	return &fakeStore{completed: map[int64][]byte{}, failed: map[int64]string{}}
}

func (f *fakeStore) add(j *Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, j)
}

func (f *fakeStore) Lease(_ context.Context, _ string, _ Execution, _ []string, _ time.Duration) (*Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) == 0 {
		return nil, nil
	}
	j := f.pending[0]
	f.pending = f.pending[1:]
	j.Attempts++
	return j, nil
}

func (f *fakeStore) Complete(_ context.Context, id int64, result []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed[id] = result
	return nil
}

func (f *fakeStore) Fail(_ context.Context, j *Job, errMsg string, _ bool, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed[j.ID] = errMsg
	return nil
}

func (f *fakeStore) ExtendLease(context.Context, int64, string, time.Duration) error { return nil }

func (f *fakeStore) done(id int64) (bool, []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.completed[id]
	return ok, r
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunnerProcessesJob(t *testing.T) {
	store := newFakeStore()
	r := newRunner(store, discardLogger(), WithPoll(time.Millisecond), WithLease(time.Second))

	done := make(chan Job, 1)
	r.Register("test.echo", func(_ context.Context, j Job) (json.RawMessage, error) {
		done <- j
		return json.RawMessage(`{"ok":true}`), nil
	})
	store.add(&Job{ID: 7, Kind: "test.echo", Execution: Local, Payload: json.RawMessage(`{"x":1}`), MaxAttempts: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	select {
	case j := <-done:
		if j.ID != 7 || string(j.Payload) != `{"x":1}` {
			t.Fatalf("обработчик получил не ту задачу: %+v", j)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("задача не дошла до обработчика")
	}

	waitFor(t, func() bool { ok, _ := store.done(7); return ok })
	if _, res := store.done(7); string(res) != `{"ok":true}` {
		t.Errorf("результат не сохранён: %q", res)
	}
}

func TestRunnerFailsWithoutHandler(t *testing.T) {
	store := newFakeStore()
	r := newRunner(store, discardLogger(), WithPoll(time.Millisecond))
	store.add(&Job{ID: 9, Kind: "unknown.kind", Execution: Local, MaxAttempts: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	waitFor(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		_, ok := store.failed[9]
		return ok
	})
}

func TestRunnerRecoversPanic(t *testing.T) {
	store := newFakeStore()
	r := newRunner(store, discardLogger(), WithPoll(time.Millisecond))
	r.Register("test.panic", func(context.Context, Job) (json.RawMessage, error) {
		panic("бум")
	})
	store.add(&Job{ID: 11, Kind: "test.panic", Execution: Local, MaxAttempts: 3})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	waitFor(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		msg, ok := store.failed[11]
		return ok && msg != ""
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("условие не выполнилось за отведённое время")
}
