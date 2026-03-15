package hooks

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

type Context struct {
	Ctx  context.Context
	Meta any
}

type SyncHandler func(Context, any) (any, bool, error)
type AsyncHandler func(Context, any)

type namedSync struct {
	name     string
	priority int
	handler  SyncHandler
}

type namedAsync struct {
	name     string
	priority int
	handler  AsyncHandler
}

type Hub struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu sync.RWMutex

	syncHandlers  map[string][]namedSync
	asyncHandlers map[string][]namedAsync

	asyncMu    sync.Mutex
	asyncCond  *sync.Cond
	asyncQueue []func()
}

func NewHub(parent context.Context) *Hub {
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithCancel(parent)
	h := &Hub{
		ctx:           ctx,
		cancel:        cancel,
		syncHandlers:  make(map[string][]namedSync),
		asyncHandlers: make(map[string][]namedAsync),
	}
	h.asyncCond = sync.NewCond(&h.asyncMu)

	go h.runAsync()
	return h
}

func (h *Hub) Close() {
	if h == nil || h.cancel == nil {
		return
	}
	h.cancel()
	h.asyncCond.Broadcast()
}

func (h *Hub) runAsync() {
	for {
		task := h.popAsync()
		if task == nil {
			return
		}
		task()
	}
}

func (h *Hub) popAsync() func() {
	h.asyncMu.Lock()
	defer h.asyncMu.Unlock()

	for len(h.asyncQueue) == 0 && h.ctx.Err() == nil {
		h.asyncCond.Wait()
	}

	if len(h.asyncQueue) == 0 {
		return nil
	}

	task := h.asyncQueue[0]
	h.asyncQueue[0] = nil
	h.asyncQueue = h.asyncQueue[1:]
	return task
}

func (h *Hub) pushAsync(task func()) {
	if task == nil {
		return
	}

	h.asyncMu.Lock()
	defer h.asyncMu.Unlock()

	if h.ctx.Err() != nil {
		return
	}

	h.asyncQueue = append(h.asyncQueue, task)
	h.asyncCond.Signal()
}

func (h *Hub) emitAsync(ctx Context, hooks []namedAsync, payload any) {
	for _, hk := range hooks {
		handler := hk.handler
		c := ctx
		p := payload

		if h.ctx.Err() != nil {
			return
		}

		h.pushAsync(func() { handler(c, p) })
	}
}

func (h *Hub) RegisterSync(event, name string, priority int, handler SyncHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cur := h.syncHandlers[event]
	next := make([]namedSync, 0, len(cur)+1)
	next = append(next, cur...)
	next = append(next, namedSync{name: name, priority: priority, handler: handler})
	sort.SliceStable(next, func(i, j int) bool { return next[i].priority < next[j].priority })
	h.syncHandlers[event] = next
}

func (h *Hub) RegisterAsync(event, name string, priority int, handler AsyncHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cur := h.asyncHandlers[event]
	next := make([]namedAsync, 0, len(cur)+1)
	next = append(next, cur...)
	next = append(next, namedAsync{name: name, priority: priority, handler: handler})
	sort.SliceStable(next, func(i, j int) bool { return next[i].priority < next[j].priority })
	h.asyncHandlers[event] = next
}

func (h *Hub) Emit(event string, ctx Context, payload any) (any, bool, error) {
	h.mu.RLock()
	syncs := h.syncHandlers[event]
	asyncs := h.asyncHandlers[event]
	h.mu.RUnlock()

	out := payload
	for _, hk := range syncs {
		next, stop, err := hk.handler(ctx, out)
		if err != nil {
			return out, stop, fmt.Errorf("hook %s failed: %w", hk.name, err)
		}
		out = next
		if stop {
			h.emitAsync(ctx, asyncs, out)
			return out, true, nil
		}
	}

	h.emitAsync(ctx, asyncs, out)
	return out, false, nil
}
