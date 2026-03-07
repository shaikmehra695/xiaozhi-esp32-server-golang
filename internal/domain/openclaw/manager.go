package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/logger"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	cmap "github.com/orcaman/concurrent-map/v2"
)

const (
	MaxOfflineMessagesPerDevice = 20
	OfflineMessageTTL           = 24 * time.Hour

	openClawSentenceMinLen = 2
	openClawTestDevicePref = "__openclaw_test__:"
)

func logSnippet(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func isOpenClawTestDevice(deviceID string) bool {
	return strings.HasPrefix(strings.TrimSpace(deviceID), openClawTestDevicePref)
}

type WSMessage struct {
	ID            string          `json:"id"`
	Timestamp     int64           `json:"timestamp"`
	Type          string          `json:"type"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

type MessagePayload struct {
	Content   string                 `json:"content"`
	SessionID string                 `json:"session_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ResponsePayload struct {
	Content   string                 `json:"content"`
	SessionID string                 `json:"session_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type ResponseDelivery struct {
	DeviceID      string
	CorrelationID string
	SessionID     string
	Text          string
	IsStart       bool
	IsEnd         bool
	Metadata      map[string]interface{}
}

type OfflineMessage struct {
	Text          string
	CorrelationID string
	IsEnd         bool
	CreatedAt     time.Time
}

type pendingRoute struct {
	DeviceID  string
	CreatedAt time.Time
}

type responseStreamState struct {
	DeviceID  string
	Buffer    string
	IsFirst   bool
	LastSeq   int64
	CreatedAt time.Time
}

type AgentSession struct {
	agentID string
	conn    *websocket.Conn

	ctx    context.Context
	cancel context.CancelFunc

	writeMu sync.Mutex
	pending sync.Map // correlation_id -> pendingRoute
	modes   sync.Map // device_id -> bool
	streams sync.Map // correlation_id -> *responseStreamState
}

func newAgentSession(agentID string, conn *websocket.Conn) *AgentSession {
	ctx, cancel := context.WithCancel(context.Background())
	return &AgentSession{
		agentID: agentID,
		conn:    conn,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (s *AgentSession) Send(msg WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		logger.Warnf("OpenClaw ws marshal failed: agent=%s type=%s id=%s corr=%s err=%v", s.agentID, msg.Type, msg.ID, msg.CorrelationID, err)
		return err
	}

	logger.Debugf(
		"OpenClaw ws send start: agent=%s type=%s id=%s corr=%s payload_bytes=%d frame_bytes=%d",
		s.agentID,
		msg.Type,
		msg.ID,
		msg.CorrelationID,
		len(msg.Payload),
		len(data),
	)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Warnf("OpenClaw ws send failed: agent=%s type=%s id=%s corr=%s err=%v", s.agentID, msg.Type, msg.ID, msg.CorrelationID, err)
		return err
	}
	logger.Debugf("OpenClaw ws send ok: agent=%s type=%s id=%s corr=%s", s.agentID, msg.Type, msg.ID, msg.CorrelationID)
	return nil
}

func (s *AgentSession) TrackPending(correlationID string, deviceID string) {
	correlationID = strings.TrimSpace(correlationID)
	deviceID = strings.TrimSpace(deviceID)
	if correlationID == "" || deviceID == "" {
		return
	}
	s.pending.Store(correlationID, pendingRoute{
		DeviceID:  deviceID,
		CreatedAt: time.Now(),
	})
	logger.Debugf("OpenClaw pending tracked: agent=%s correlation_id=%s device=%s", s.agentID, correlationID, deviceID)
}

func (s *AgentSession) RemovePending(correlationID string) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return
	}
	s.pending.Delete(correlationID)
	logger.Debugf("OpenClaw pending removed: agent=%s correlation_id=%s", s.agentID, correlationID)
}

func (s *AgentSession) ResolvePending(correlationID string) (string, bool) {
	if strings.TrimSpace(correlationID) == "" {
		return "", false
	}

	value, ok := s.pending.Load(correlationID)
	if !ok {
		return "", false
	}
	s.pending.Delete(correlationID)

	route, ok := value.(pendingRoute)
	if !ok {
		return "", false
	}
	logger.Debugf("OpenClaw pending resolved: agent=%s correlation_id=%s device=%s", s.agentID, correlationID, route.DeviceID)
	return route.DeviceID, route.DeviceID != ""
}

func (s *AgentSession) PeekPending(correlationID string) (string, bool) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return "", false
	}
	value, ok := s.pending.Load(correlationID)
	if !ok {
		return "", false
	}
	route, ok := value.(pendingRoute)
	if !ok {
		return "", false
	}
	return strings.TrimSpace(route.DeviceID), strings.TrimSpace(route.DeviceID) != ""
}

func (s *AgentSession) LoadOrCreateStream(correlationID string) *responseStreamState {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return nil
	}
	if existing, ok := s.streams.Load(correlationID); ok {
		if state, ok := existing.(*responseStreamState); ok && state != nil {
			return state
		}
	}
	newState := &responseStreamState{
		IsFirst:   true,
		CreatedAt: time.Now(),
	}
	actual, _ := s.streams.LoadOrStore(correlationID, newState)
	state, _ := actual.(*responseStreamState)
	return state
}

func (s *AgentSession) GetStream(correlationID string) (*responseStreamState, bool) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return nil, false
	}
	value, ok := s.streams.Load(correlationID)
	if !ok {
		return nil, false
	}
	state, ok := value.(*responseStreamState)
	return state, ok && state != nil
}

func (s *AgentSession) RemoveStream(correlationID string) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return
	}
	s.streams.Delete(correlationID)
}

func (s *AgentSession) IsSameConn(conn *websocket.Conn) bool {
	return s.conn == conn
}

func (s *AgentSession) EnterMode(deviceID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false
	}
	s.modes.Store(deviceID, true)
	return true
}

func (s *AgentSession) ExitMode(deviceID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false
	}
	s.modes.Delete(deviceID)
	return true
}

func (s *AgentSession) IsModeEnabled(deviceID string) bool {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return false
	}
	value, ok := s.modes.Load(deviceID)
	if !ok {
		return false
	}
	enabled, ok := value.(bool)
	return ok && enabled
}

func (s *AgentSession) copyModesFrom(other *AgentSession) {
	if other == nil {
		return
	}
	other.modes.Range(func(key, value interface{}) bool {
		deviceID, ok := key.(string)
		if !ok {
			return true
		}
		enabled, ok := value.(bool)
		if !ok || !enabled {
			return true
		}
		s.modes.Store(deviceID, true)
		return true
	})
}

func (s *AgentSession) Close() {
	s.cancel()
	_ = s.conn.Close()
}

type Manager struct {
	sessions cmap.ConcurrentMap[string, *AgentSession]

	offlineMu sync.Mutex
	offline   map[string][]OfflineMessage
}

var (
	defaultManager *Manager
	managerOnce    sync.Once
)

func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{
			sessions: cmap.New[*AgentSession](),
			offline:  make(map[string][]OfflineMessage),
		}
	})
	return defaultManager
}

func (m *Manager) RegisterAgentConnection(agentID string, conn *websocket.Conn) *AgentSession {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil
	}

	newSession := newAgentSession(agentID, conn)
	if oldSession, ok := m.sessions.Get(agentID); ok && oldSession != nil {
		newSession.copyModesFrom(oldSession)
		logger.Infof("OpenClaw session replaced: agent=%s", agentID)
		oldSession.Close()
	}
	m.sessions.Set(agentID, newSession)
	logger.Infof("OpenClaw session registered: agent=%s", agentID)
	return newSession
}

func (m *Manager) UnregisterAgentConnection(agentID string, session *AgentSession) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return
	}

	current, ok := m.sessions.Get(agentID)
	if !ok || current == nil {
		return
	}

	if session == nil || current == session {
		m.sessions.Remove(agentID)
		logger.Infof("OpenClaw session unregistered: agent=%s", agentID)
	}
}

func (m *Manager) GetAgentSession(agentID string) *AgentSession {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil
	}
	session, ok := m.sessions.Get(agentID)
	if !ok {
		return nil
	}
	return session
}

func (m *Manager) SendMessage(agentID string, deviceID string, content string, sessionID string) (string, error) {
	rawContent := content
	agentID = strings.TrimSpace(agentID)
	deviceID = strings.TrimSpace(deviceID)
	content = strings.TrimSpace(content)
	sessionID = strings.TrimSpace(sessionID)

	logger.Debugf(
		"OpenClaw SendMessage requested: agent=%s device=%s session=%s content_len=%d content_trim_len=%d content_snippet=%q",
		agentID,
		deviceID,
		sessionID,
		len(rawContent),
		len(content),
		logSnippet(content, 64),
	)

	if agentID == "" {
		err := fmt.Errorf("agentID is required")
		logger.Warnf("OpenClaw SendMessage rejected: %v", err)
		return "", err
	}
	if deviceID == "" {
		err := fmt.Errorf("deviceID is required")
		logger.Warnf("OpenClaw SendMessage rejected: agent=%s err=%v", agentID, err)
		return "", err
	}
	if content == "" {
		err := fmt.Errorf("content is required")
		logger.Warnf("OpenClaw SendMessage rejected: agent=%s device=%s err=%v", agentID, deviceID, err)
		return "", err
	}

	session := m.GetAgentSession(agentID)
	if session == nil {
		err := fmt.Errorf("openclaw session not found for agent %s", agentID)
		logger.Warnf("OpenClaw SendMessage rejected: agent=%s device=%s session=%s err=%v", agentID, deviceID, sessionID, err)
		return "", err
	}

	messageID := uuid.NewString()
	payloadBytes, err := json.Marshal(MessagePayload{
		Content:   content,
		SessionID: sessionID,
		Metadata: map[string]interface{}{
			"device_id": deviceID,
			"agent_id":  agentID,
			"stream":    true,
		},
	})
	if err != nil {
		logger.Warnf("OpenClaw SendMessage payload marshal failed: agent=%s device=%s message_id=%s err=%v", agentID, deviceID, messageID, err)
		return "", err
	}

	session.TrackPending(messageID, deviceID)
	logger.Debugf("OpenClaw SendMessage dispatching: agent=%s device=%s session=%s message_id=%s payload_bytes=%d", agentID, deviceID, sessionID, messageID, len(payloadBytes))
	err = session.Send(WSMessage{
		ID:        messageID,
		Timestamp: time.Now().UnixMilli(),
		Type:      "message",
		Payload:   payloadBytes,
	})
	if err != nil {
		session.RemovePending(messageID)
		logger.Warnf("OpenClaw SendMessage send failed: agent=%s device=%s session=%s message_id=%s err=%v", agentID, deviceID, sessionID, messageID, err)
		return "", err
	}

	logger.Debugf("OpenClaw SendMessage dispatched: agent=%s device=%s session=%s message_id=%s", agentID, deviceID, sessionID, messageID)
	return messageID, nil
}

func (m *Manager) EnterMode(agentID string, deviceID string) bool {
	agentID = strings.TrimSpace(agentID)
	deviceID = strings.TrimSpace(deviceID)
	session := m.GetAgentSession(agentID)
	if session == nil {
		logger.Warnf("OpenClaw EnterMode failed: agent=%s device=%s reason=no_agent_session", agentID, deviceID)
		return false
	}
	ok := session.EnterMode(deviceID)
	logger.Infof("OpenClaw mode enabled: agent=%s device=%s ok=%v", agentID, deviceID, ok)
	return ok
}

func (m *Manager) ExitMode(agentID string, deviceID string) bool {
	agentID = strings.TrimSpace(agentID)
	deviceID = strings.TrimSpace(deviceID)
	session := m.GetAgentSession(agentID)
	if session == nil {
		logger.Debugf("OpenClaw ExitMode ignored: agent=%s device=%s reason=no_agent_session", agentID, deviceID)
		return false
	}
	ok := session.ExitMode(deviceID)
	logger.Infof("OpenClaw mode disabled: agent=%s device=%s ok=%v", agentID, deviceID, ok)
	return ok
}

func (m *Manager) IsModeEnabled(agentID string, deviceID string) bool {
	agentID = strings.TrimSpace(agentID)
	deviceID = strings.TrimSpace(deviceID)
	session := m.GetAgentSession(agentID)
	if session == nil {
		logger.Debugf("OpenClaw mode check: agent=%s device=%s enabled=false reason=no_agent_session", agentID, deviceID)
		return false
	}
	enabled := session.IsModeEnabled(deviceID)
	logger.Debugf("OpenClaw mode check: agent=%s device=%s enabled=%v", agentID, deviceID, enabled)
	return enabled
}

func (m *Manager) HandleResponse(
	agentID string,
	session *AgentSession,
	correlationID string,
	payload ResponsePayload,
	deliver func(event ResponseDelivery) bool,
) {
	agentID = strings.TrimSpace(agentID)
	correlationID = strings.TrimSpace(correlationID)
	sessionID := strings.TrimSpace(payload.SessionID)
	content := strings.TrimSpace(payload.Content)
	streamDone := parseMetadataBool(payload.Metadata, "done")
	streamSeq := parseMetadataInt64(payload.Metadata, "seq")
	streamID := readMetadataString(payload.Metadata, "stream_id")
	streamPhase := readMetadataString(payload.Metadata, "phase")
	isStreaming := streamDone || streamSeq > 0 || streamID != "" || streamPhase != ""

	// 非流式默认视为一次性完成；缺失 correlation_id 的流式响应也降级为一次性处理。
	if !isStreaming || correlationID == "" {
		streamDone = true
	}

	deviceID := ""
	routeSource := ""
	if payload.Metadata != nil {
		deviceID = readMetadataString(payload.Metadata, "device_id")
		if deviceID != "" {
			routeSource = "metadata.device_id"
		}
	}
	if deviceID == "" && session != nil && correlationID != "" {
		if state, ok := session.GetStream(correlationID); ok && state != nil {
			if cached := strings.TrimSpace(state.DeviceID); cached != "" {
				deviceID = cached
				routeSource = "stream.correlation_id"
			}
		}
	}
	if deviceID == "" && session != nil && correlationID != "" {
		if resolvedDeviceID, ok := session.PeekPending(correlationID); ok {
			deviceID = strings.TrimSpace(resolvedDeviceID)
			if deviceID != "" {
				routeSource = "pending.correlation_id"
			}
		}
	}

	if deviceID == "" {
		logger.Warnf(
			"OpenClaw response missing device route, agent=%s correlation_id=%s session=%s done=%v seq=%d stream_id=%s phase=%s",
			agentID,
			correlationID,
			sessionID,
			streamDone,
			streamSeq,
			streamID,
			streamPhase,
		)
		return
	}

	var state *responseStreamState
	if session != nil && correlationID != "" {
		state = session.LoadOrCreateStream(correlationID)
		if state != nil && strings.TrimSpace(state.DeviceID) == "" {
			state.DeviceID = deviceID
		}
	}

	if state != nil && streamSeq > 0 {
		if state.LastSeq > 0 && streamSeq <= state.LastSeq {
			logger.Warnf(
				"OpenClaw response seq out-of-order, agent=%s correlation_id=%s seq=%d last_seq=%d",
				agentID,
				correlationID,
				streamSeq,
				state.LastSeq,
			)
		}
		state.LastSeq = streamSeq
	}

	workingText := content
	if state != nil {
		if content != "" {
			state.Buffer += content
		}
		workingText = state.Buffer
	}

	logger.Infof(
		"OpenClaw response routed: agent=%s device=%s session=%s correlation_id=%s route=%s done=%v seq=%d stream_id=%s phase=%s content_len=%d content_snippet=%q",
		agentID,
		deviceID,
		sessionID,
		correlationID,
		routeSource,
		streamDone,
		streamSeq,
		streamID,
		streamPhase,
		len(content),
		logSnippet(content, 64),
	)

	isFirst := true
	if state != nil {
		isFirst = state.IsFirst
	}

	sentences := make([]string, 0)
	remaining := strings.TrimSpace(workingText)
	if remaining != "" {
		sentences, remaining = extractOpenClawSentences(workingText, openClawSentenceMinLen, isFirst)
	}
	if state != nil {
		state.Buffer = remaining
	}

	emit := func(text string, isStart bool, isEnd bool) {
		text = strings.TrimSpace(text)
		if text == "" && !isEnd {
			return
		}
		event := ResponseDelivery{
			DeviceID:      deviceID,
			CorrelationID: correlationID,
			SessionID:     sessionID,
			Text:          text,
			IsStart:       isStart,
			IsEnd:         isEnd,
			Metadata:      payload.Metadata,
		}
		if deliver != nil && deliver(event) {
			logger.Debugf(
				"OpenClaw response delivered online: agent=%s device=%s correlation_id=%s start=%v end=%v text_len=%d",
				agentID,
				deviceID,
				correlationID,
				isStart,
				isEnd,
				len(text),
			)
			return
		}
		logger.Warnf("OpenClaw response queued offline: agent=%s device=%s correlation_id=%s", agentID, deviceID, correlationID)
		m.AddOfflineMessage(deviceID, text, correlationID, isEnd)
	}

	// 对话测试设备（__openclaw_test__）直接透传分片，避免拆句导致离线队列条目暴涨并触发20条上限截断。
	if isOpenClawTestDevice(deviceID) {
		if content != "" {
			emit(content, isFirst, streamDone)
			if state != nil {
				state.IsFirst = false
				state.Buffer = ""
			}
		} else if streamDone {
			emit("", isFirst, true)
		}

		if streamDone && session != nil && correlationID != "" {
			session.RemovePending(correlationID)
			session.RemoveStream(correlationID)
		}
		return
	}

	for i, sentence := range sentences {
		emit(sentence, isFirst && i == 0, false)
	}
	if state != nil && len(sentences) > 0 {
		state.IsFirst = false
	}

	if !streamDone {
		return
	}

	finalText := remaining
	finalIsStart := isFirst
	if state != nil {
		finalText = strings.TrimSpace(state.Buffer)
		finalIsStart = state.IsFirst
	}

	if finalText != "" {
		emit(finalText, finalIsStart, true)
		if state != nil {
			state.IsFirst = false
			state.Buffer = ""
		}
	} else {
		// 结束帧允许空 content，用于驱动接收端收尾。
		emit("", finalIsStart, true)
	}

	if session != nil && correlationID != "" {
		session.RemovePending(correlationID)
		session.RemoveStream(correlationID)
	}
}

func readMetadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, exists := metadata[key]
	if !exists || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func parseMetadataBool(metadata map[string]interface{}, key string) bool {
	if metadata == nil {
		return false
	}
	value, exists := metadata[key]
	if !exists || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		return err == nil && b
	case json.Number:
		n, err := v.Int64()
		return err == nil && n != 0
	case float64:
		return v != 0
	case float32:
		return v != 0
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	default:
		return false
	}
}

func parseMetadataInt64(metadata map[string]interface{}, key string) int64 {
	if metadata == nil {
		return 0
	}
	value, exists := metadata[key]
	if !exists || value == nil {
		return 0
	}
	switch v := value.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		if v > uint64((1<<63)-1) {
			return 0
		}
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return n
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

func isOpenClawSentenceSeparator(r rune, isFirst bool) bool {
	switch r {
	case '。', '？', '！', ';', '；', ':', '：', '\n', '.', '?', '!':
		return true
	case '，', ',':
		return isFirst
	default:
		return false
	}
}

func extractOpenClawSentences(text string, minLen int, isFirst bool) ([]string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, ""
	}
	runes := []rune(trimmed)
	start := 0
	sentences := make([]string, 0, 4)
	firstSplit := isFirst

	for i, r := range runes {
		if !isOpenClawSentenceSeparator(r, firstSplit) {
			continue
		}
		segment := strings.TrimSpace(string(runes[start : i+1]))
		if segment != "" && len([]rune(segment)) >= minLen {
			sentences = append(sentences, segment)
			start = i + 1
			if firstSplit {
				firstSplit = false
			}
		}
	}

	remaining := strings.TrimSpace(string(runes[start:]))
	return sentences, remaining
}

func (m *Manager) AddOfflineMessage(deviceID string, text string, correlationID string, isEnd bool) {
	deviceID = strings.TrimSpace(deviceID)
	text = strings.TrimSpace(text)
	correlationID = strings.TrimSpace(correlationID)
	if deviceID == "" {
		return
	}
	if text == "" && !isEnd {
		return
	}

	m.offlineMu.Lock()
	defer m.offlineMu.Unlock()

	m.pruneOfflineLocked(deviceID)
	msgList := m.offline[deviceID]
	if text == "" && isEnd {
		// 结束帧允许空内容：优先标记同 correlation 的最后一条为结束；不存在则写入空结束标记。
		for i := len(msgList) - 1; i >= 0; i-- {
			if correlationID == "" || strings.TrimSpace(msgList[i].CorrelationID) == correlationID {
				msgList[i].IsEnd = true
				m.offline[deviceID] = msgList
				logger.Infof("OpenClaw offline message marked end: device=%s correlation_id=%s total=%d", deviceID, correlationID, len(msgList))
				return
			}
		}
	}

	msgList = append(msgList, OfflineMessage{
		Text:          text,
		CorrelationID: correlationID,
		IsEnd:         isEnd,
		CreatedAt:     time.Now(),
	})
	if len(msgList) > MaxOfflineMessagesPerDevice {
		msgList = msgList[len(msgList)-MaxOfflineMessagesPerDevice:]
	}
	m.offline[deviceID] = msgList
	logger.Infof("OpenClaw offline message appended: device=%s correlation_id=%s end=%v total=%d", deviceID, correlationID, isEnd, len(msgList))
}

func (m *Manager) ReplayOfflineMessages(deviceID string, deliver func(msg OfflineMessage) error) (int, int) {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" || deliver == nil {
		return 0, 0
	}

	m.offlineMu.Lock()
	m.pruneOfflineLocked(deviceID)
	snapshot := append([]OfflineMessage(nil), m.offline[deviceID]...)
	m.offlineMu.Unlock()

	delivered := 0
	for _, msg := range snapshot {
		if err := deliver(msg); err != nil {
			break
		}
		delivered++
	}

	m.offlineMu.Lock()
	defer m.offlineMu.Unlock()

	m.pruneOfflineLocked(deviceID)
	current := m.offline[deviceID]
	if delivered > 0 {
		if delivered >= len(current) {
			delete(m.offline, deviceID)
			return delivered, 0
		}
		m.offline[deviceID] = current[delivered:]
		current = m.offline[deviceID]
	}
	return delivered, len(current)
}

func (m *Manager) pruneOfflineLocked(deviceID string) {
	msgList, exists := m.offline[deviceID]
	if !exists || len(msgList) == 0 {
		delete(m.offline, deviceID)
		return
	}

	now := time.Now()
	filtered := make([]OfflineMessage, 0, len(msgList))
	for _, msg := range msgList {
		if msg.CreatedAt.IsZero() {
			continue
		}
		if now.Sub(msg.CreatedAt) > OfflineMessageTTL {
			continue
		}
		filtered = append(filtered, msg)
	}

	if len(filtered) == 0 {
		delete(m.offline, deviceID)
		return
	}
	m.offline[deviceID] = filtered
}
