package chat

import (
	"context"
	"testing"
	"time"

	types_conn "xiaozhi-esp32-server-golang/internal/app/server/types"
	data_audio "xiaozhi-esp32-server-golang/internal/data/audio"
	data_client "xiaozhi-esp32-server-golang/internal/data/client"
	msgdata "xiaozhi-esp32-server-golang/internal/data/msg"
	"xiaozhi-esp32-server-golang/internal/util"

	"github.com/spf13/viper"
)

func TestIsWithinCommandTTLUses800msWindow(t *testing.T) {
	now := time.Now()

	if !isWithinCommandTTL(now.Add(-800*time.Millisecond), now) {
		t.Fatal("expected 800ms-old command to stay within TTL")
	}
	if isWithinCommandTTL(now.Add(-801*time.Millisecond), now) {
		t.Fatal("expected command older than 800ms to fall outside TTL")
	}
}

func TestHandleListenDetectDebouncesLLMQueue(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	setViperValueForTest(t, "enable_greeting", false)

	if err := session.HandleListenDetect(&data_client.ClientMessage{
		Type:     msgdata.MessageTypeListen,
		DeviceID: session.clientState.DeviceID,
		Text:     "普通问题",
	}); err != nil {
		t.Fatalf("HandleListenDetect returned error: %v", err)
	}

	if _, err := session.chatTextQueue.Pop(context.Background(), 200*time.Millisecond); err != util.ErrQueueTimeout {
		t.Fatalf("expected debounce to delay queue push, got %v", err)
	}

	item, err := session.chatTextQueue.Pop(context.Background(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("expected debounced detect llm to be enqueued, got %v", err)
	}
	if item.text != "普通问题" {
		t.Fatalf("expected debounced text to be preserved, got %q", item.text)
	}
}

func TestHandleListenStartCancelsPendingDetectLLM(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	setViperValueForTest(t, "enable_greeting", false)

	if err := session.HandleListenDetect(&data_client.ClientMessage{
		Type:     msgdata.MessageTypeListen,
		DeviceID: session.clientState.DeviceID,
		Text:     "普通问题",
	}); err != nil {
		t.Fatalf("HandleListenDetect returned error: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	session.clientState.SetListenPhase(data_client.ListenPhaseStarting)

	if err := session.HandleListenStart(&data_client.ClientMessage{
		Type:     msgdata.MessageTypeListen,
		DeviceID: session.clientState.DeviceID,
		Mode:     "auto",
	}); err != nil {
		t.Fatalf("HandleListenStart returned error: %v", err)
	}

	if _, err := session.chatTextQueue.Pop(context.Background(), 500*time.Millisecond); err != util.ErrQueueTimeout {
		t.Fatalf("expected listen start to cancel pending detect llm, got %v", err)
	}
}

func TestHandleListenStartIgnoresRecentDetectWhileWelcomePlaying(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	session.clientState.IsWelcomePlaying = true
	session.clientState.RecordCommandArrival(data_client.CommandTypeDetect, time.Now())

	if err := session.HandleListenStart(&data_client.ClientMessage{
		Type:     msgdata.MessageTypeListen,
		DeviceID: session.clientState.DeviceID,
		Mode:     "auto",
	}); err != nil {
		t.Fatalf("HandleListenStart returned error: %v", err)
	}

	if got := session.clientState.GetListenPhase(); got != data_client.ListenPhaseIdle {
		t.Fatalf("expected welcome playback to keep listen phase idle, got %s", got)
	}
	if got := session.clientState.ListenMode; got != "" {
		t.Fatalf("expected ignored listen start not to set listen mode, got %s", got)
	}
	history := session.clientState.GetCommandHistorySnapshot()
	if history.LastCmdType != data_client.CommandTypeDetect {
		t.Fatalf("expected ignored listen start to preserve detect history, got %s", history.LastCmdType)
	}
	if session.isRealtimeListenSessionActive() {
		t.Fatal("expected ignored listen start not to mark realtime listen active")
	}
}

func TestHandleListenStartIgnoresDuplicateRealtimeStart(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	session.clientState.ListenMode = "realtime"
	session.clientState.SetListenPhase(data_client.ListenPhaseListening)
	session.realtimeListenSessionActive.Store(true)
	session.clientState.RecordCommandArrival(data_client.CommandTypeListenStart, time.Now().Add(-2*time.Second))

	sessionCtx := session.clientState.SessionCtx.Get(session.clientState.Ctx)
	afterAsrCtx := session.clientState.AfterAsrSessionCtx.Get(sessionCtx)
	initialSeq := session.listenStartSeq.Load()
	initialHistory := session.clientState.GetCommandHistorySnapshot()

	if err := session.HandleListenStart(&data_client.ClientMessage{
		Type:     msgdata.MessageTypeListen,
		DeviceID: session.clientState.DeviceID,
		Mode:     "realtime",
	}); err != nil {
		t.Fatalf("HandleListenStart returned error: %v", err)
	}

	if got := session.clientState.GetListenPhase(); got != data_client.ListenPhaseListening {
		t.Fatalf("expected listen phase to remain listening, got %s", got)
	}
	if sessionCtx.Err() != nil {
		t.Fatalf("expected duplicate realtime start to keep session context alive, got %v", sessionCtx.Err())
	}
	if afterAsrCtx.Err() != nil {
		t.Fatalf("expected duplicate realtime start to keep after-asr context alive, got %v", afterAsrCtx.Err())
	}
	if got := session.listenStartSeq.Load(); got != initialSeq {
		t.Fatalf("expected duplicate realtime start not to advance listenStartSeq, got %d want %d", got, initialSeq)
	}
	if !session.isRealtimeListenSessionActive() {
		t.Fatal("expected duplicate realtime start to keep realtime listen session active")
	}

	history := session.clientState.GetCommandHistorySnapshot()
	if history.LastCmdType != initialHistory.LastCmdType || !history.LastCmdAt.Equal(initialHistory.LastCmdAt) {
		t.Fatalf("expected duplicate realtime start not to update command history, got %+v want %+v", history, initialHistory)
	}
}

func TestManualListenStopResetsStateForRepeatedTurns(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	session.clientState.ListenMode = "manual"

	for turn := 1; turn <= 3; turn++ {
		session.clientState.SetStatus(data_client.ClientStatusListening)
		session.clientState.SetListenPhase(data_client.ListenPhaseListening)
		session.clientState.Asr.AsrAudioChannel = make(chan []float32, 1)

		if err := session.HandleListenStop(); err != nil {
			t.Fatalf("turn %d HandleListenStop returned error: %v", turn, err)
		}
		if got := session.clientState.GetListenPhase(); got != data_client.ListenPhaseIdle {
			t.Fatalf("turn %d expected listen phase idle, got %s", turn, got)
		}
		if got := session.clientState.GetStatus(); got != data_client.ClientStatusListenStop {
			t.Fatalf("turn %d expected status listenStop, got %s", turn, got)
		}
		if session.clientState.Asr.AsrAudioChannel != nil {
			t.Fatalf("turn %d expected ASR audio channel to be closed", turn)
		}
		history := session.clientState.GetCommandHistorySnapshot()
		if history.LastCmdType != data_client.CommandTypeListenStop {
			t.Fatalf("turn %d expected last command listen_stop, got %s", turn, history.LastCmdType)
		}
	}
}

func TestStopSpeakingClearsRealtimeListenSessionActive(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	session.realtimeListenSessionActive.Store(true)

	session.StopSpeaking(true)

	if session.isRealtimeListenSessionActive() {
		t.Fatal("expected session cancel path to clear realtime listen session active")
	}
}

func TestHandleListenStopClearsRealtimeListenSessionActive(t *testing.T) {
	session := newDetectDebounceTestSession(t)
	session.clientState.ListenMode = "realtime"
	session.realtimeListenSessionActive.Store(true)
	initialSeq := session.listenStartSeq.Load()

	if err := session.HandleListenStop(); err != nil {
		t.Fatalf("HandleListenStop returned error: %v", err)
	}

	if session.isRealtimeListenSessionActive() {
		t.Fatal("expected realtime listen stop to clear realtime listen session active")
	}
	if got := session.listenStartSeq.Load(); got != initialSeq+1 {
		t.Fatalf("expected realtime listen stop to invalidate listen start sequence, got %d want %d", got, initialSeq+1)
	}
}

func TestListenStartSequencingIsTransportAgnosticForMqttUDP(t *testing.T) {
	session := newDetectDebounceTestSessionWithTransport(t, types_conn.TransportTypeMqttUdp)
	session.clientState.ListenMode = "realtime"

	startSeq := session.beginListenStart()

	if startSeq == 0 {
		t.Fatal("expected non-zero listen start sequence")
	}
	if got := session.clientState.GetListenPhase(); got != data_client.ListenPhaseStarting {
		t.Fatalf("expected mqtt udp listen phase starting, got %s", got)
	}
	if !session.isRealtimeListenSessionActive() {
		t.Fatal("expected mqtt udp realtime listen session to be active")
	}
	if !session.isCurrentListenStart(startSeq) {
		t.Fatal("expected current listen start sequence to be valid")
	}

	session.invalidateListenStart()

	if session.isCurrentListenStart(startSeq) {
		t.Fatal("expected invalidated mqtt udp listen start sequence to be stale")
	}
	if session.isRealtimeListenSessionActive() {
		t.Fatal("expected invalidation to clear mqtt udp realtime listen activity")
	}
	if got := session.clientState.GetListenPhase(); got != data_client.ListenPhaseIdle {
		t.Fatalf("expected mqtt udp listen phase idle after invalidation, got %s", got)
	}
}

func newDetectDebounceTestSession(t *testing.T) *ChatSession {
	return newDetectDebounceTestSessionWithTransport(t, types_conn.TransportTypeWebsocket)
}

func newDetectDebounceTestSessionWithTransport(t *testing.T, transportType string) *ChatSession {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	clientState := &data_client.ClientState{
		Ctx:         ctx,
		Cancel:      cancel,
		Dialogue:    &data_client.Dialogue{},
		DeviceID:    "detect-test-device",
		SessionID:   "detect-test-session",
		ListenPhase: data_client.ListenPhaseIdle,
		Status:      data_client.ClientStatusInit,
		OutputAudioFormat: data_audio.AudioFormat{
			SampleRate:    data_audio.SampleRate,
			Channels:      data_audio.Channels,
			FrameDuration: data_audio.FrameDuration,
		},
	}

	conn := &speakRequestTestConn{
		transportType: transportType,
		deviceID:      clientState.DeviceID,
	}
	session := NewChatSession(clientState, NewServerTransport(conn, clientState), nil, nil)

	t.Cleanup(func() {
		session.cancelPendingDetectLLM()
		cancel()
	})

	return session
}

func setViperValueForTest(t *testing.T, key string, value any) {
	t.Helper()

	oldValue := viper.Get(key)
	viper.Set(key, value)
	t.Cleanup(func() {
		viper.Set(key, oldValue)
	})
}
