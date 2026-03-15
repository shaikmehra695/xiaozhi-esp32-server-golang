package hooks

import (
	"context"

	pkghooks "xiaozhi-esp32-server-golang/internal/pkg/hooks"
)

type SyncHandler func(Context, any) (any, bool, error)
type AsyncHandler func(Context, any)

type Hub struct {
	hub *pkghooks.Hub
}

func NewHub(parent context.Context) *Hub {
	return &Hub{hub: pkghooks.NewHub(parent)}
}

func (h *Hub) Close() {
	if h == nil || h.hub == nil {
		return
	}
	h.hub.Close()
}

func (h *Hub) Emit(event string, ctx Context, payload any) (any, bool, error) {
	return h.hub.Emit(event, toPlatformContext(ctx), payload)
}

func (h *Hub) RegisterSync(event, name string, priority int, handler SyncHandler) {
	h.hub.RegisterSync(event, name, priority, func(ctx pkghooks.Context, payload any) (any, bool, error) {
		return handler(fromPkgContext(ctx), payload)
	})
}

func (h *Hub) RegisterAsync(event, name string, priority int, handler AsyncHandler) {
	h.hub.RegisterAsync(event, name, priority, func(ctx pkghooks.Context, payload any) {
		handler(fromPkgContext(ctx), payload)
	})
}

func toPlatformContext(ctx Context) pkghooks.Context {
	return pkghooks.Context{
		Ctx:  ctx.Ctx,
		Meta: ctx,
	}
}

func fromPkgContext(ctx pkghooks.Context) Context {
	chatCtx, ok := ctx.Meta.(Context)
	if !ok {
		return Context{Ctx: ctx.Ctx}
	}
	if chatCtx.Ctx == nil {
		chatCtx.Ctx = ctx.Ctx
	}
	return chatCtx
}
