package hooks

import (
	"sync"
	"time"

	log "xiaozhi-esp32-server-golang/logger"
)

type turnMetric struct {
	turnID int64

	turnStartTs     int64
	asrFirstTextTs  int64
	asrFinalTextTs  int64
	llmStartTs      int64
	llmFirstTokenTs int64
	llmEndTs        int64
	ttsStartTs      int64
	ttsFirstFrameTs int64
	ttsStopTs       int64
}

type statisticPlugin struct {
	mu sync.Mutex

	currentTurn map[string]int64
	turns       map[string]*turnMetric
	lastSeen    map[string]int64
}

func RegisterBuiltinPlugins(hub *Hub) {
	if hub == nil {
		return
	}

	plugin := &statisticPlugin{
		currentTurn: make(map[string]int64),
		turns:       make(map[string]*turnMetric),
		lastSeen:    make(map[string]int64),
	}
	hub.RegisterAsync(EventChatMetric, "statistic_plugin", 100, plugin.onMetric)
}

func (p *statisticPlugin) onMetric(ctx Context, payload any) {
	data, ok := payload.(MetricData)
	if !ok || ctx.SessionID == "" {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	nowTs := time.Now().UnixMilli()
	p.lastSeen[ctx.SessionID] = nowTs
	p.cleanupStale(nowTs)

	tm := p.getOrCreateTurn(ctx.SessionID, data.Stage)
	switch data.Stage {
	case MetricTurnStart:
		tm.turnStartTs = data.Ts
	case MetricAsrFirstText:
		tm.asrFirstTextTs = data.Ts
	case MetricAsrFinalText:
		tm.asrFinalTextTs = data.Ts
	case MetricLlmStart:
		tm.llmStartTs = data.Ts
	case MetricLlmFirstToken:
		tm.llmFirstTokenTs = data.Ts
	case MetricLlmEnd:
		tm.llmEndTs = data.Ts
	case MetricTtsStart:
		tm.ttsStartTs = data.Ts
	case MetricTtsFirstFrame:
		tm.ttsFirstFrameTs = data.Ts
	case MetricTtsStop:
		tm.ttsStopTs = data.Ts
		p.logTurnMetric(ctx.SessionID, tm)
		delete(p.turns, ctx.SessionID)
	}
}

func (p *statisticPlugin) getOrCreateTurn(sessionID string, stage MetricStage) *turnMetric {
	if stage == MetricTurnStart {
		p.currentTurn[sessionID]++
		tm := &turnMetric{turnID: p.currentTurn[sessionID]}
		p.turns[sessionID] = tm
		return tm
	}

	if tm, ok := p.turns[sessionID]; ok {
		return tm
	}

	if p.currentTurn[sessionID] == 0 {
		p.currentTurn[sessionID] = 1
	}

	tm := &turnMetric{turnID: p.currentTurn[sessionID]}
	p.turns[sessionID] = tm
	return tm
}

func calcDelta(start, end int64) int64 {
	if start <= 0 || end <= 0 || end < start {
		return 0
	}
	return end - start
}

func (p *statisticPlugin) logTurnMetric(sessionID string, tm *turnMetric) {
	log.Infof(
		"metric turn=%d session=%s asr_first=%dms asr_final=%dms llm_first=%dms llm_total=%dms tts_first=%dms tts_total=%dms e2e_first=%dms e2e_total=%dms",
		tm.turnID,
		sessionID,
		calcDelta(tm.turnStartTs, tm.asrFirstTextTs),
		calcDelta(tm.turnStartTs, tm.asrFinalTextTs),
		calcDelta(tm.llmStartTs, tm.llmFirstTokenTs),
		calcDelta(tm.llmStartTs, tm.llmEndTs),
		calcDelta(tm.ttsStartTs, tm.ttsFirstFrameTs),
		calcDelta(tm.ttsStartTs, tm.ttsStopTs),
		calcDelta(tm.turnStartTs, tm.ttsFirstFrameTs),
		calcDelta(tm.turnStartTs, tm.ttsStopTs),
	)
}

func (p *statisticPlugin) cleanupStale(nowTs int64) {
	const ttl = int64(2 * 60 * 1000)
	for sessionID, last := range p.lastSeen {
		if nowTs-last > ttl {
			delete(p.lastSeen, sessionID)
			delete(p.turns, sessionID)
			delete(p.currentTurn, sessionID)
		}
	}
}
