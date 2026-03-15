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
	if h == nil || h.hub == nil {
		return payload, false, nil
	}
	return h.hub.Emit(event, toPlatformContext(ctx), payload)
}

func EmitTyped[T any](h *Hub, event string, ctx Context, payload T) (T, bool, error) {
	if h == nil || h.hub == nil {
		return payload, false, nil
	}

	out, stop, err := h.Emit(event, ctx, payload)
	if typed, ok := out.(T); ok {
		return typed, stop, err
	}
	return payload, stop, err
}

func (h *Hub) EmitASROutput(ctx Context, data ASROutputData) (ASROutputData, bool, error) {
	return EmitTyped(h, EventChatASROutput, ctx, data)
}

func (h *Hub) EmitLLMInput(ctx Context, data LLMInputData) (LLMInputData, bool, error) {
	return EmitTyped(h, EventChatLLMInput, ctx, data)
}

func (h *Hub) EmitLLMOutput(ctx Context, data LLMOutputData) (LLMOutputData, bool, error) {
	return EmitTyped(h, EventChatLLMOutput, ctx, data)
}

func (h *Hub) EmitTTSInput(ctx Context, data TTSInputData) (TTSInputData, bool, error) {
	return EmitTyped(h, EventChatTTSInput, ctx, data)
}

func (h *Hub) EmitTTSOutputStart(ctx Context) (bool, error) {
	_, stop, err := EmitTyped(h, EventChatTTSOutputStart, ctx, TTSOutputStartData{})
	return stop, err
}

func (h *Hub) EmitTTSOutputStop(ctx Context, data TTSOutputStopData) (bool, error) {
	_, stop, err := EmitTyped(h, EventChatTTSOutputStop, ctx, data)
	return stop, err
}

func (h *Hub) EmitMetric(ctx Context, data MetricData) (bool, error) {
	_, stop, err := EmitTyped(h, EventChatMetric, ctx, data)
	return stop, err
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
