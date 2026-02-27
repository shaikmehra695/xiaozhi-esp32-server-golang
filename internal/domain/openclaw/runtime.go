package openclaw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	cmap "github.com/orcaman/concurrent-map/v2"

	"xiaozhi-esp32-server-golang/internal/util"
)

type RuntimeConfig struct {
	BaseURL  string `json:"base_url"`
	AuthType string `json:"auth_type"`
	Token    string `json:"token"`
}

type OfflineMessage struct {
	ID          uint   `json:"id"`
	DeviceID    string `json:"device_id"`
	PayloadJSON string `json:"payload_json"`
	TaskID      string `json:"task_id"`
}

type GatewayResponse struct {
	Reply   string `json:"reply"`
	TaskID  string `json:"task_id"`
	Pending bool   `json:"pending"`
}

type sessionState struct {
	mu          sync.RWMutex
	cfg         RuntimeConfig
	gwClient    GatewayClient
	failCount   int
	lastActive  time.Time
	initialized bool
}

func (s *sessionState) getConfig(ctx context.Context, fetcher func(context.Context) (RuntimeConfig, error)) (RuntimeConfig, error) {
	s.mu.RLock()
	if s.initialized {
		cfg := s.cfg
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return s.cfg, nil
	}
	cfg, err := fetcher(ctx)
	if err != nil {
		return RuntimeConfig{}, err
	}
	s.cfg = cfg
	s.gwClient = newGatewayClient(cfg)
	s.initialized = true
	s.lastActive = time.Now()
	return cfg, nil
}

func (s *sessionState) resetFail() {
	s.mu.Lock()
	s.failCount = 0
	s.lastActive = time.Now()
	s.mu.Unlock()
}

func (s *sessionState) incFail() int {
	s.mu.Lock()
	s.failCount++
	count := s.failCount
	s.lastActive = time.Now()
	s.mu.Unlock()
	return count
}

func (s *sessionState) gatewayClient() GatewayClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gwClient
}

type fallbackTask struct {
	deviceID string
	userID   uint
	agentID  string
	configID string
	text     string
	cfg      RuntimeConfig
}

type Runtime struct {
	sessions      cmap.ConcurrentMap[string, *sessionState]
	fallbackQueue chan fallbackTask
	client        *http.Client
	initWorkers   sync.Once
}

func NewRuntime() *Runtime {
	r := &Runtime{
		sessions:      cmap.New[*sessionState](),
		fallbackQueue: make(chan fallbackTask, 512),
		client:        &http.Client{Timeout: 10 * time.Second},
	}
	r.startFallbackWorkers()
	return r
}

func (r *Runtime) startFallbackWorkers() {
	r.initWorkers.Do(func() {
		for i := 0; i < 4; i++ {
			go r.fallbackWorker()
		}
	})
}

func (r *Runtime) fallbackWorker() {
	for task := range r.fallbackQueue {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err := newGatewayClient(task.cfg).SendMessage(ctx, task.text, task.deviceID)
		if err == nil {
			payload := strings.TrimSpace(resp.Reply)
			if payload == "" {
				payload = "OpenClaw 任务完成。"
			}
			_ = r.createOfflineMessage(ctx, task.deviceID, task.userID, task.agentID, task.configID, resp.TaskID, payload)
		}
		cancel()
	}
}

func (r *Runtime) sessionKey(deviceID, configID string) string {
	return strings.TrimSpace(deviceID) + "||" + strings.TrimSpace(configID)
}

func (r *Runtime) getOrCreateSession(deviceID, configID string) *sessionState {
	key := r.sessionKey(deviceID, configID)
	if v, ok := r.sessions.Get(key); ok {
		return v
	}
	st := &sessionState{}
	_ = r.sessions.SetIfAbsent(key, st)
	v, _ := r.sessions.Get(key)
	return v
}

func (r *Runtime) HandleRequest(ctx context.Context, deviceID, agentID string, userID uint, configID string, text string) (string, error) {
	st := r.getOrCreateSession(deviceID, configID)
	cfg, err := st.getConfig(ctx, func(fetchCtx context.Context) (RuntimeConfig, error) {
		return r.fetchRuntimeConfig(fetchCtx, deviceID, configID)
	})
	if err != nil {
		return "", err
	}

	reply, err := st.gatewayClient().SendMessage(ctx, text, deviceID)
	if err == nil {
		st.resetFail()
		if strings.TrimSpace(reply.Reply) != "" {
			return reply.Reply, nil
		}
		if reply.Pending {
			return "OpenClaw 任务处理中，完成后会推送结果。", nil
		}
		return "OpenClaw 已接收请求。", nil
	}

	failCount := st.incFail()
	select {
	case r.fallbackQueue <- fallbackTask{
		deviceID: deviceID,
		userID:   userID,
		agentID:  agentID,
		configID: configID,
		text:     text,
		cfg:      cfg,
	}:
	default:
		// 队列满时直接丢弃兜底任务，避免阻塞主聊天链路
	}

	if failCount >= 3 {
		return "OpenClaw 暂不可用，已自动切回默认助手。", ErrFallbackToLLM
	}
	return "OpenClaw 处理中，请稍后。", nil
}

func (r *Runtime) fetchRuntimeConfig(ctx context.Context, deviceID, configID string) (RuntimeConfig, error) {
	q := url.Values{}
	q.Set("device_id", strings.TrimSpace(deviceID))
	q.Set("config_id", strings.TrimSpace(configID))
	runtimeURL := strings.TrimSuffix(util.GetBackendURL(), "/") + "/api/internal/openclaw/runtime-config?" + q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, runtimeURL, nil)
	resp, err := r.client.Do(req)
	if err != nil {
		return RuntimeConfig{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return RuntimeConfig{}, fmt.Errorf("runtime config http %d", resp.StatusCode)
	}
	var result struct {
		Data RuntimeConfig `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RuntimeConfig{}, err
	}
	return result.Data, nil
}

func (r *Runtime) createOfflineMessage(ctx context.Context, deviceID string, userID uint, agentID, configID, taskID, payload string) error {
	apiURL := strings.TrimSuffix(util.GetBackendURL(), "/") + "/api/internal/openclaw/offline-messages"
	cfgUint, _ := strconv.Atoi(strings.TrimSpace(configID))
	agentUint, _ := strconv.Atoi(strings.TrimSpace(agentID))
	body := map[string]interface{}{
		"device_id":          deviceID,
		"user_id":            userID,
		"agent_id":           agentUint,
		"openclaw_config_id": uint(cfgUint),
		"task_id":            taskID,
		"message_type":       "text",
		"payload_json":       payload,
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("offline create http %d", resp.StatusCode)
	}
	return nil
}

func (r *Runtime) ListPendingOfflineMessages(ctx context.Context, deviceID string) ([]OfflineMessage, error) {
	q := url.Values{}
	q.Set("device_id", strings.TrimSpace(deviceID))
	q.Set("status", "pending")
	listURL := strings.TrimSuffix(util.GetBackendURL(), "/") + "/api/internal/openclaw/offline-messages?" + q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("offline list http %d", resp.StatusCode)
	}
	var result struct {
		Data []OfflineMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func (r *Runtime) MarkOfflineMessageDelivered(ctx context.Context, id uint) error {
	apiURL := fmt.Sprintf("%s/api/internal/openclaw/offline-messages/%d/delivered", strings.TrimSuffix(util.GetBackendURL(), "/"), id)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, nil)
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("offline mark delivered http %d", resp.StatusCode)
	}
	return nil
}

var (
	ErrFallbackToLLM = fmt.Errorf("openclaw fallback to llm")
	defaultRuntime   = NewRuntime()
)

func HandleRequest(ctx context.Context, deviceID, agentID string, userID uint, configID string, text string) (string, error) {
	return defaultRuntime.HandleRequest(ctx, deviceID, agentID, userID, configID, text)
}

func ListPendingOfflineMessages(ctx context.Context, deviceID string) ([]OfflineMessage, error) {
	return defaultRuntime.ListPendingOfflineMessages(ctx, deviceID)
}

func MarkOfflineMessageDelivered(ctx context.Context, id uint) error {
	return defaultRuntime.MarkOfflineMessageDelivered(ctx, id)
}
