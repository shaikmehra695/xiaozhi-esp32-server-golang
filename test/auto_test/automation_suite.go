package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const defaultHelloTimeout = 10 * time.Second

var (
	runnerMode      = "manual"
	autoCasesFilter = "all"
	autoCaseTimeout = 20 * time.Second
	autoTurns       = 1
)

type protocolEvent struct {
	Direction   string
	Type        string
	State       string
	Mode        string
	Text        string
	Transport   string
	BinaryBytes int
	Note        string
	At          time.Time
}

type sessionRuntime struct {
	conn     *websocket.Conn
	deviceID string

	writeMu sync.Mutex
	eventMu sync.Mutex
	events  []protocolEvent

	helloAckCh chan ServerMessage
	ttsStartCh chan struct{}
	ttsStopCh  chan struct{}
}

type listenExpectation struct {
	State string
	Mode  string
	Text  string
}

type protocolTestCase struct {
	Name                  string
	Description           string
	LocalMode             string
	InputText             string
	Turns                 int
	Timeout               time.Duration
	ExpectListenSequence  []listenExpectation
	ExpectListenCount     int
	ExpectHelloTransport  string
	ExpectRestartAfterTTS bool
}

type protocolTestResult struct {
	Name     string
	Mode     string
	Passed   bool
	Duration time.Duration
	Err      error
}

func newSessionRuntime(conn *websocket.Conn, deviceID string) *sessionRuntime {
	return &sessionRuntime{
		conn:       conn,
		deviceID:   deviceID,
		helloAckCh: make(chan ServerMessage, 1),
		ttsStartCh: make(chan struct{}, 4),
		ttsStopCh:  make(chan struct{}, 4),
	}
}

func (rt *sessionRuntime) writeText(data []byte) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	return rt.conn.WriteMessage(websocket.TextMessage, data)
}

func (rt *sessionRuntime) writeBinary(data []byte) error {
	rt.writeMu.Lock()
	defer rt.writeMu.Unlock()
	return rt.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (rt *sessionRuntime) recordEvent(event protocolEvent) {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	rt.events = append(rt.events, event)
}

func (rt *sessionRuntime) snapshotEvents() []protocolEvent {
	rt.eventMu.Lock()
	defer rt.eventMu.Unlock()
	out := make([]protocolEvent, len(rt.events))
	copy(out, rt.events)
	return out
}

func (rt *sessionRuntime) recordOutgoingMessage(msg ClientMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "send",
		Type:      msg.Type,
		State:     msg.State,
		Mode:      msg.Mode,
		Text:      msg.Text,
		At:        time.Now(),
	})
}

func (rt *sessionRuntime) recordIncomingMessage(msg ServerMessage) {
	rt.recordEvent(protocolEvent{
		Direction: "recv",
		Type:      msg.Type,
		State:     msg.State,
		Transport: msg.Transport,
		Text:      msg.Text,
		At:        time.Now(),
	})
}

func (rt *sessionRuntime) recordIncomingBinary(size int, accepted bool) {
	note := "accepted"
	if !accepted {
		note = "ignored"
	}
	rt.recordEvent(protocolEvent{
		Direction:   "recv_binary",
		Type:        "binary",
		BinaryBytes: size,
		Note:        note,
		At:          time.Now(),
	})
}

func (rt *sessionRuntime) recordNote(note string) {
	rt.recordEvent(protocolEvent{
		Direction: "note",
		Type:      "note",
		Note:      note,
		At:        time.Now(),
	})
}

func (rt *sessionRuntime) notifyHelloAck(msg ServerMessage) {
	select {
	case rt.helloAckCh <- msg:
	default:
	}
}

func (rt *sessionRuntime) notifyTTSStart() {
	select {
	case rt.ttsStartCh <- struct{}{}:
	default:
	}
}

func (rt *sessionRuntime) notifyTTSStop() {
	select {
	case rt.ttsStopCh <- struct{}{}:
	default:
	}
}

func waitForHelloAck(rt *sessionRuntime, timeout time.Duration) error {
	select {
	case msg := <-rt.helloAckCh:
		if msg.Transport != "websocket" {
			return fmt.Errorf("hello 响应 transport 非 websocket: %s", msg.Transport)
		}
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("等待 hello 响应超时")
	}
}

func drainSignal(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func waitForSignal(ch chan struct{}, timeout time.Duration, label string) error {
	select {
	case <-ch:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("等待 %s 超时", label)
	}
}

func buildProtocolTestCases() []protocolTestCase {
	manualListenCount := 2 * autoTurns
	autoListenCount := 2 + autoTurns
	return []protocolTestCase{
		{
			Name:        "manual_roundtrip",
			Description: "验证 manual 模式的 listen start/stop 与 tts 生命周期",
			LocalMode:   LocalModeManual,
			InputText:   speectText,
			Turns:       autoTurns,
			Timeout:     autoCaseTimeout,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: LocalModeManual},
				{State: MessageStateStop, Mode: LocalModeManual},
			},
			ExpectListenCount:    manualListenCount,
			ExpectHelloTransport: "websocket",
		},
		{
			Name:        "auto1_roundtrip",
			Description: "验证 auto1: detect -> start auto，tts stop 后重新 start auto",
			LocalMode:   LocalModeAuto1,
			InputText:   speectText,
			Turns:       autoTurns,
			Timeout:     autoCaseTimeout,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     autoListenCount,
			ExpectHelloTransport:  "websocket",
			ExpectRestartAfterTTS: true,
		},
		{
			Name:        "auto2_roundtrip",
			Description: "验证 auto2: start auto -> detect，tts stop 后重新 start auto",
			LocalMode:   LocalModeAuto2,
			InputText:   speectText,
			Turns:       autoTurns,
			Timeout:     autoCaseTimeout,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateStart, Mode: "auto"},
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: "auto"},
			},
			ExpectListenCount:     autoListenCount,
			ExpectHelloTransport:  "websocket",
			ExpectRestartAfterTTS: true,
		},
		{
			Name:        "realtime_roundtrip",
			Description: "验证 realtime: detect -> start realtime，tts stop 后不追加 listen start",
			LocalMode:   LocalModeRealtime,
			InputText:   speectText,
			Turns:       autoTurns,
			Timeout:     autoCaseTimeout,
			ExpectListenSequence: []listenExpectation{
				{State: MessageStateDetect, Text: defaultDetectText},
				{State: MessageStateStart, Mode: LocalModeRealtime},
			},
			ExpectListenCount:    2,
			ExpectHelloTransport: "websocket",
		},
	}
}

func selectProtocolTestCases() ([]protocolTestCase, error) {
	allCases := buildProtocolTestCases()
	if strings.EqualFold(strings.TrimSpace(autoCasesFilter), "all") || strings.TrimSpace(autoCasesFilter) == "" {
		return allCases, nil
	}

	lookup := make(map[string]protocolTestCase, len(allCases))
	for _, testCase := range allCases {
		lookup[testCase.Name] = testCase
	}

	names := strings.Split(autoCasesFilter, ",")
	selected := make([]protocolTestCase, 0, len(names))
	for _, name := range names {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		testCase, ok := lookup[key]
		if !ok {
			return nil, fmt.Errorf("未知自动化用例: %s", key)
		}
		selected = append(selected, testCase)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("未选择任何自动化用例")
	}
	return selected, nil
}

func runAutomationSuite(serverAddr, deviceID, audioFile string) error {
	if audioFile != "" {
		return fmt.Errorf("自动化模式暂不支持 -audio，请改用文本驱动测试")
	}

	testCases, err := selectProtocolTestCases()
	if err != nil {
		return err
	}

	results := make([]protocolTestResult, 0, len(testCases))
	for _, testCase := range testCases {
		mode = testCase.LocalMode
		startedAt := time.Now()
		fmt.Printf("\n=== 开始自动化用例: %s (%s) ===\n", testCase.Name, testCase.Description)
		err := runClient(serverAddr, deviceID, audioFile, &testCase)
		result := protocolTestResult{
			Name:     testCase.Name,
			Mode:     testCase.LocalMode,
			Passed:   err == nil,
			Duration: time.Since(startedAt),
			Err:      err,
		}
		results = append(results, result)
		if err != nil {
			fmt.Printf("=== 用例失败: %s, err=%v ===\n", testCase.Name, err)
			continue
		}
		fmt.Printf("=== 用例通过: %s, 耗时=%s ===\n", testCase.Name, result.Duration)
	}

	failures := 0
	fmt.Println("\n自动化测试汇总:")
	for _, result := range results {
		status := "PASS"
		if !result.Passed {
			status = "FAIL"
			failures++
		}
		if result.Err != nil {
			fmt.Printf("- [%s] %s (%s): %v\n", status, result.Name, result.Mode, result.Err)
			continue
		}
		fmt.Printf("- [%s] %s (%s): %s\n", status, result.Name, result.Mode, result.Duration)
	}
	if failures > 0 {
		return fmt.Errorf("自动化测试失败: %d/%d", failures, len(results))
	}
	return nil
}

func runProtocolTestCase(rt *sessionRuntime, testCase *protocolTestCase, runTurn func(string) error) error {
	drainSignal(rt.ttsStartCh)
	drainSignal(rt.ttsStopCh)

	for turn := 0; turn < testCase.Turns; turn++ {
		if err := runTurn(testCase.InputText); err != nil {
			return fmt.Errorf("执行发言轮次失败: %v", err)
		}
		if err := waitForSignal(rt.ttsStartCh, testCase.Timeout, "tts start"); err != nil {
			return err
		}
		if err := waitForSignal(rt.ttsStopCh, testCase.Timeout, "tts stop"); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}

	return evaluateProtocolCase(rt, testCase)
}

func evaluateProtocolCase(rt *sessionRuntime, testCase *protocolTestCase) error {
	events := rt.snapshotEvents()
	if err := assertHelloTransport(events, testCase.ExpectHelloTransport); err != nil {
		return err
	}
	if err := assertListenSequence(events, testCase.ExpectListenSequence, testCase.ExpectListenCount); err != nil {
		return err
	}
	if err := assertTTSLifecycle(events); err != nil {
		return err
	}
	if err := assertBinaryAcceptedAfterTTSStart(events); err != nil {
		return err
	}
	if testCase.ExpectRestartAfterTTS {
		if err := assertRestartAfterTTSStop(events); err != nil {
			return err
		}
	}
	return nil
}

func assertHelloTransport(events []protocolEvent, expected string) error {
	for _, event := range events {
		if event.Direction == "recv" && event.Type == MessageTypeHello {
			if expected != "" && event.Transport != expected {
				return fmt.Errorf("hello transport 不符合预期: got=%s want=%s", event.Transport, expected)
			}
			return nil
		}
	}
	return fmt.Errorf("未收到 hello 响应")
}

func assertListenSequence(events []protocolEvent, expected []listenExpectation, expectedCount int) error {
	listenEvents := make([]protocolEvent, 0)
	for _, event := range events {
		if event.Direction == "send" && event.Type == MessageTypeListen {
			listenEvents = append(listenEvents, event)
		}
	}

	if expectedCount >= 0 && len(listenEvents) != expectedCount {
		return fmt.Errorf("listen 消息数量不符合预期: got=%d want=%d", len(listenEvents), expectedCount)
	}
	if len(listenEvents) < len(expected) {
		return fmt.Errorf("listen 消息数量不足: got=%d want>=%d", len(listenEvents), len(expected))
	}
	for idx, want := range expected {
		got := listenEvents[idx]
		if got.State != want.State {
			return fmt.Errorf("listen[%d] state 不符合预期: got=%s want=%s", idx, got.State, want.State)
		}
		if want.Mode != "" && got.Mode != want.Mode {
			return fmt.Errorf("listen[%d] mode 不符合预期: got=%s want=%s", idx, got.Mode, want.Mode)
		}
		if want.Text != "" && got.Text != want.Text {
			return fmt.Errorf("listen[%d] text 不符合预期: got=%s want=%s", idx, got.Text, want.Text)
		}
	}
	return nil
}

func assertTTSLifecycle(events []protocolEvent) error {
	startIndex := -1
	stopIndex := -1
	for idx, event := range events {
		if event.Direction != "recv" || event.Type != "tts" {
			continue
		}
		if startIndex == -1 && event.State == MessageStateStart {
			startIndex = idx
		}
		if event.State == MessageStateStop {
			stopIndex = idx
			break
		}
	}
	if startIndex == -1 {
		return fmt.Errorf("未收到 tts start")
	}
	if stopIndex == -1 {
		return fmt.Errorf("未收到 tts stop")
	}
	if stopIndex < startIndex {
		return fmt.Errorf("tts stop 早于 tts start")
	}
	return nil
}

func assertBinaryAcceptedAfterTTSStart(events []protocolEvent) error {
	startIndex := -1
	for idx, event := range events {
		if event.Direction == "recv" && event.Type == "tts" && event.State == MessageStateStart {
			startIndex = idx
			break
		}
	}
	if startIndex == -1 {
		return fmt.Errorf("未找到 tts start，无法验证音频接收顺序")
	}

	for idx, event := range events {
		if event.Direction != "recv_binary" || event.Note != "accepted" {
			continue
		}
		if idx < startIndex {
			return fmt.Errorf("在 tts start 之前接受到了音频二进制帧")
		}
		return nil
	}
	return fmt.Errorf("未接收到任何被接受的音频二进制帧")
}

func assertRestartAfterTTSStop(events []protocolEvent) error {
	stopIndex := -1
	for idx, event := range events {
		if event.Direction == "recv" && event.Type == "tts" && event.State == MessageStateStop {
			stopIndex = idx
			break
		}
	}
	if stopIndex == -1 {
		return fmt.Errorf("未找到 tts stop，无法验证重启监听")
	}
	for idx := stopIndex + 1; idx < len(events); idx++ {
		event := events[idx]
		if event.Direction == "send" && event.Type == MessageTypeListen && event.State == MessageStateStart && event.Mode == "auto" {
			return nil
		}
	}
	return fmt.Errorf("tts stop 之后未重新发送 listen start auto")
}
