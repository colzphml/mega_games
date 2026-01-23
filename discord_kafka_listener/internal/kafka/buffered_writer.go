package kafka

import (
	"container/heap"
	"context"
	"math/rand"
	"time"

	"github.com/rs/zerolog"
)

type BufferedWriterConfig struct {
	WriteTimeout time.Duration
	BaseDelay    time.Duration
	MaxDelay     time.Duration
	MaxAttempts  int
	MaxPending   int
	BufferSize   int
}

type BufferedWriter struct {
	producer *Producer
	log      zerolog.Logger
	cfg      BufferedWriterConfig
	incoming chan string
	rng      *rand.Rand
}

func NewBufferedWriter(producer *Producer, cfg BufferedWriterConfig, log zerolog.Logger) *BufferedWriter {
	if cfg.MaxPending < 1 {
		cfg.MaxPending = 1
	}
	if cfg.BufferSize < 1 {
		cfg.BufferSize = 1
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = 500 * time.Millisecond
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	return &BufferedWriter{
		producer: producer,
		log:      log,
		cfg:      cfg,
		incoming: make(chan string, cfg.BufferSize),
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (w *BufferedWriter) Enqueue(ctx context.Context, id string, timeout time.Duration) bool {
	if id == "" {
		return false
	}
	if timeout <= 0 {
		select {
		case w.incoming <- id:
			return true
		default:
			return false
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case w.incoming <- id:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (w *BufferedWriter) Run(ctx context.Context, onSuccess func(string)) {
	var queue retryHeap
	pending := make(map[string]struct{})
	var timer *time.Timer
	var timerC <-chan time.Time
	var processing string

	for {
		if queue.Len() == 0 {
			if timer != nil {
				timer.Stop()
			}
			timerC = nil
		} else {
			next := queue[0].nextAttempt
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			if timer == nil {
				timer = time.NewTimer(delay)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(delay)
			}
			timerC = timer.C
		}

		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case id := <-w.incoming:
			if id == "" {
				continue
			}
			if id == processing {
				continue
			}
			if _, ok := pending[id]; ok {
				continue
			}
			if w.cfg.MaxPending > 0 && queue.Len() >= w.cfg.MaxPending {
				w.log.Warn().Str("message_id", id).Msg("retry queue full, dropping")
				continue
			}
			item := &retryItem{
				id:          id,
				attempts:    0,
				nextAttempt: time.Now(),
			}
			heap.Push(&queue, item)
			pending[id] = struct{}{}
		case <-timerC:
			if queue.Len() == 0 {
				continue
			}
			item := heap.Pop(&queue).(*retryItem)
			delete(pending, item.id)
			processing = item.id
			writeCtx, cancel := context.WithTimeout(ctx, w.cfg.WriteTimeout)
			err := w.producer.WriteMessage(writeCtx, item.id)
			cancel()
			processing = ""
			if err == nil {
				onSuccess(item.id)
				continue
			}
			item.attempts++
			if w.cfg.MaxAttempts > 0 && item.attempts > w.cfg.MaxAttempts {
				w.log.Error().Err(err).Str("message_id", item.id).Int("attempts", item.attempts).Msg("dropping message after max attempts")
				continue
			}
			delay := w.backoffDelay(item.attempts)
			item.nextAttempt = time.Now().Add(delay)
			w.log.Warn().Err(err).Str("message_id", item.id).Int("attempts", item.attempts).Dur("retry_in", delay).Msg("failed to write to kafka, retrying")
			heap.Push(&queue, item)
			pending[item.id] = struct{}{}
		}
	}
}

func (w *BufferedWriter) backoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	delay := w.cfg.BaseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= w.cfg.MaxDelay {
			delay = w.cfg.MaxDelay
			break
		}
	}
	if delay <= 0 {
		return w.cfg.MaxDelay
	}
	jitter := time.Duration(w.rng.Int63n(int64(delay/5 + 1)))
	return delay + jitter
}

type retryItem struct {
	id          string
	attempts    int
	nextAttempt time.Time
	index       int
}

type retryHeap []*retryItem

func (h retryHeap) Len() int { return len(h) }

func (h retryHeap) Less(i, j int) bool {
	if h[i].nextAttempt.Equal(h[j].nextAttempt) {
		return h[i].id < h[j].id
	}
	return h[i].nextAttempt.Before(h[j].nextAttempt)
}

func (h retryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *retryHeap) Push(x interface{}) {
	item := x.(*retryItem)
	item.index = len(*h)
	*h = append(*h, item)
}

func (h *retryHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	item.index = -1
	*h = old[:n-1]
	return item
}
