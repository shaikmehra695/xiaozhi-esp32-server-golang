package mcp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/websocket"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// DeviceMcpSession 代表一个设备的MCP会话，聚合了多种MCP连接
type DeviceMcpSession struct {
	deviceID              string
	Ctx                   context.Context
	cancel                context.CancelFunc
	wsEndPointMcp         sync.Map
	iotOverMcpByTransport map[string]*McpClientInstance
	iotMux                sync.RWMutex
}

type mcpClientInitState uint32

const (
	mcpClientInitStateIdle mcpClientInitState = iota
	mcpClientInitStateInitializing
	mcpClientInitStateReady
)

func normalizeDeviceTransportType(transportType string) string {
	transportType = strings.TrimSpace(transportType)
	if transportType == "" {
		return "unknown"
	}
	return transportType
}

func buildIotServerName(deviceID, transportType string) string {
	return fmt.Sprintf("iot_over_mcp_%s_%s", deviceID, normalizeDeviceTransportType(transportType))
}

func (dcs *DeviceMcpSession) AddWsEndPointMcp(mcpClient *McpClientInstance) {
	dcs.wsEndPointMcp.Store(mcpClient.serverName, mcpClient)

	// 设置关闭回调
	mcpClient.SetOnCloseHandler(dcs.handleMcpClientClose)

	mcpClient.refreshTools()
}

func (dcs *DeviceMcpSession) SetIotOverMcp(transportType string, mcpClient *McpClientInstance) {
	dcs.iotMux.Lock()
	defer dcs.iotMux.Unlock()
	transportType = normalizeDeviceTransportType(transportType)
	// 同 device + transportType 保持单实例
	if old := dcs.iotOverMcpByTransport[transportType]; old != nil && old != mcpClient {
		old.connected = false
		old.cancel()
	}
	dcs.iotOverMcpByTransport[transportType] = mcpClient

	// 设置关闭回调
	mcpClient.SetOnCloseHandler(dcs.handleMcpClientClose)
}

func (dcs *DeviceMcpSession) RemoveWsEndPointMcp(mcpClient *McpClientInstance) {
	dcs.wsEndPointMcp.Delete(mcpClient.serverName)
}

// GetDeviceID 获取设备ID
func (dcs *DeviceMcpSession) GetDeviceID() string {
	return dcs.deviceID
}

// handleMcpClientClose 处理MCP客户端关闭事件
func (dcs *DeviceMcpSession) handleMcpClientClose(instance *McpClientInstance, reason string) {
	logger.Infof("设备 %s 的MCP客户端 %s 已关闭，原因: %s", dcs.deviceID, instance.serverName, reason)

	// 从会话中移除已关闭的客户端
	dcs.RemoveWsEndPointMcp(instance)
	dcs.removeIotOverMcpByInstance(instance)

	if !dcs.hasAnyClient() {
		logger.Infof("设备 %s 的所有MCP连接已关闭，清理会话", dcs.deviceID)
		dcs.cancel()
		mcpClientPool.RemoveMcpClient(dcs.deviceID)
	}
}

func (dcs *DeviceMcpSession) removeIotOverMcpByInstance(instance *McpClientInstance) {
	dcs.iotMux.Lock()
	defer dcs.iotMux.Unlock()
	for transportType, iotClient := range dcs.iotOverMcpByTransport {
		if iotClient == instance {
			delete(dcs.iotOverMcpByTransport, transportType)
		}
	}
}

func (dcs *DeviceMcpSession) hasAnyClient() bool {
	hasWsClient := false
	dcs.wsEndPointMcp.Range(func(_, _ interface{}) bool {
		hasWsClient = true
		return false
	})
	if hasWsClient {
		return true
	}

	dcs.iotMux.RLock()
	defer dcs.iotMux.RUnlock()
	return len(dcs.iotOverMcpByTransport) > 0
}

// McpClientInstance 代表一个具体的MCP客户端连接
type McpClientInstance struct {
	serverName string
	mcpClient  *client.Client // 是从ws endpoint连上来的mcp server
	tools      map[string]tool.InvokableTool
	toolsMux   sync.RWMutex // 保护工具列表的互斥锁
	serverInfo *mcp.InitializeResult
	lastPing   time.Time
	Ctx        context.Context
	cancel     context.CancelFunc
	connected  bool
	conn       ConnInterface
	initState  uint32

	// 添加关闭回调
	onCloseHandler func(instance *McpClientInstance, reason string)
	closeOnce      sync.Once
}

// NewDeviceMCPClient 创建新的MCP客户端
func NewDeviceMCPSession(deviceID string) *DeviceMcpSession {
	ctx, cancel := context.WithCancel(context.Background())

	deviceMcpClient := &DeviceMcpSession{
		deviceID:              deviceID,
		Ctx:                   ctx,
		cancel:                cancel,
		iotOverMcpByTransport: make(map[string]*McpClientInstance),
		iotMux:                sync.RWMutex{},
		// wsEndPointMcp: make(map[string]*McpClientInstance),
	}

	go deviceMcpClient.refreshToolsAndPing()

	return deviceMcpClient
}

func NewWsEndPointMcpClient(ctx context.Context, deviceID string, conn *websocket.Conn) *McpClientInstance {
	ctx, cancel := context.WithCancel(ctx)

	wsTransport, err := NewWebsocketTransport(conn)
	if err != nil {
		logger.Errorf("创建MCP客户端失败: %v", err)
		return nil
	}
	mcpClient := client.NewClient(wsTransport)

	wsEndPointMcp := &McpClientInstance{
		serverName: fmt.Sprintf("ws_endpoint_mcp_%s_%s", deviceID, conn.RemoteAddr().String()),
		mcpClient:  mcpClient,
		tools:      make(map[string]tool.InvokableTool),
		Ctx:        ctx,
		cancel:     cancel,
		connected:  true,
		lastPing:   time.Now(),
		initState:  uint32(mcpClientInitStateReady),
	}
	mcpClient.OnNotification(wsEndPointMcp.handleJSONRPCNotification)

	// 设置transport的关闭回调
	wsTransport.SetOnCloseHandler(wsEndPointMcp.handleTransportClose)

	wsEndPointMcp.sendInitlize(ctx)
	wsEndPointMcp.mcpClient.Start(ctx)
	return wsEndPointMcp
}

func NewIotOverMcpClient(deviceID string, transportType string, conn ConnInterface) *McpClientInstance {
	ctx, cancel := context.WithCancel(context.Background())

	wsTransport, err := NewIotOverMcpTransport(conn)
	if err != nil {
		logger.Errorf("创建MCP客户端失败: %v", err)
		return nil
	}
	mcpClient := client.NewClient(wsTransport)

	iotOverMcp := &McpClientInstance{
		serverName: buildIotServerName(deviceID, transportType),
		mcpClient:  mcpClient,
		tools:      make(map[string]tool.InvokableTool),
		Ctx:        ctx,
		cancel:     cancel,
		connected:  true,
		lastPing:   time.Now(),
		conn:       conn,
		initState:  uint32(mcpClientInitStateInitializing),
	}
	wsTransport.SetNotificationHandler(iotOverMcp.handleJSONRPCNotification)

	// 设置transport的关闭回调
	wsTransport.SetOnCloseHandler(iotOverMcp.handleTransportClose)

	return iotOverMcp
}

func (dc *McpClientInstance) startIotOverMcp() error {
	if err := dc.sendInitlize(dc.Ctx); err != nil {
		return err
	}
	dc.mcpClient.Start(dc.Ctx)
	return dc.refreshTools()
}

// refreshToolsCommon 通用的工具列表刷新逻辑
func (dc *McpClientInstance) refreshTools() error {
	if dc == nil || dc.mcpClient == nil {
		return fmt.Errorf("mcp client未初始化")
	}
	if dc.serverInfo == nil {
		return fmt.Errorf("client not initialized")
	}

	tools, err := dc.mcpClient.ListTools(dc.Ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Errorf("刷新工具列表失败: %v", err)
		return err
	}

	// 使用互斥锁保护工具列表的更新
	dc.toolsMux.Lock()
	dc.tools = ConvertMcpToolListToInvokableToolList(tools.Tools, dc.serverName, dc.mcpClient)
	dc.toolsMux.Unlock()

	logger.Infof("刷新工具列表成功: %s 获取到 %d 个工具", dc.serverName, len(dc.tools))
	return nil
}

func (dc *McpClientInstance) GetServerName() string {
	return dc.serverName
}

func (dc *McpClientInstance) IsInitialized() bool {
	return dc != nil && dc.serverInfo != nil
}

func (dc *McpClientInstance) getInitState() mcpClientInitState {
	if dc == nil {
		return mcpClientInitStateIdle
	}
	return mcpClientInitState(atomic.LoadUint32(&dc.initState))
}

func (dc *McpClientInstance) setInitState(state mcpClientInitState) {
	if dc == nil {
		return
	}
	atomic.StoreUint32(&dc.initState, uint32(state))
}

func (dc *McpClientInstance) IsInitializing() bool {
	return dc.getInitState() == mcpClientInitStateInitializing
}

func (dc *McpClientInstance) IsReady() bool {
	return dc.getInitState() == mcpClientInitStateReady
}

func (dc *McpClientInstance) closeWithReason(reason string) {
	if dc == nil {
		return
	}
	dc.closeOnce.Do(func() {
		logger.Infof("MCP客户端 %s 关闭，原因: %s", dc.serverName, reason)

		dc.connected = false
		dc.setInitState(mcpClientInitStateIdle)
		dc.cancel()

		if dc.onCloseHandler != nil {
			dc.onCloseHandler(dc, reason)
		}
	})
}

func (dc *DeviceMcpSession) heartbeatMcpInstance(mcpInstance *McpClientInstance) {
	if mcpInstance == nil || !mcpInstance.IsInitialized() {
		return
	}
	if err := mcpInstance.refreshTools(); err != nil {
		logger.Warnf("设备 %s 心跳刷新工具列表失败，主动销毁 runtime: %v", mcpInstance.serverName, err)
		mcpInstance.closeWithReason("refresh_tools_failed")
		return
	}
	err := mcpInstance.mcpClient.Ping(mcpInstance.Ctx)
	if err == nil {
		mcpInstance.lastPing = time.Now()
		logger.Debugf("设备 %s ping成功", mcpInstance.serverName)
	} else {
		logger.Warnf("设备 %s ping失败: %v", mcpInstance.serverName, err)
	}
}

func (dc *DeviceMcpSession) refreshToolsAndPing() {
	// 只在初始化时获取一次工具列表
	findTools := func(mcpInstance *McpClientInstance) {
		if mcpInstance == nil || !mcpInstance.IsInitialized() {
			return
		}
		mcpInstance.refreshTools()
	}

	// 初始化时获取工具列表
	dc.wsEndPointMcp.Range(func(_, mcpInstance interface{}) bool {
		findTools(mcpInstance.(*McpClientInstance))
		return true
	})

	dc.iotMux.RLock()
	for _, instance := range dc.iotOverMcpByTransport {
		findTools(instance)
	}
	dc.iotMux.RUnlock()

	// 每2分钟进行一次ping
	pingTick := time.NewTicker(2 * time.Minute)
	defer pingTick.Stop()

	for {
		select {
		case <-dc.Ctx.Done():
			logger.Infof("设备 %s 会话已取消，停止ping", dc.deviceID)
			return
		case <-pingTick.C:
			dc.wsEndPointMcp.Range(func(_, mcpInstance interface{}) bool {
				dc.heartbeatMcpInstance(mcpInstance.(*McpClientInstance))
				return true
			})
			dc.iotMux.RLock()
			for _, instance := range dc.iotOverMcpByTransport {
				dc.heartbeatMcpInstance(instance)
			}
			dc.iotMux.RUnlock()
		}
	}
}

func (dc *McpClientInstance) sendInitlize(ctx context.Context) error {
	initRequest := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "mcp-go",
				Version: "0.1.0",
			},
			Capabilities: mcp.ClientCapabilities{},
		},
	}

	serverInfo, err := dc.mcpClient.Initialize(ctx, initRequest)
	if err != nil {
		fmt.Printf("Failed to initialize: %v", err)
		return err
	}
	dc.serverInfo = serverInfo
	return nil
}

func (dc *McpClientInstance) findTools() (*mcp.ListToolsResult, error) {
	tools, err := dc.mcpClient.ListTools(dc.Ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Errorf("获取工具列表失败: %v", err)
		return nil, err
	}
	return tools, nil
}

// handleJSONRPCNotification 处理JSON-RPC通知
func (dc *McpClientInstance) handleJSONRPCNotification(notification mcp.JSONRPCNotification) {
	switch notification.Method {
	case "notifications/progress":
		//handleProgressNotification(notification)
	case "notifications/message":
		//handleMessageNotification(notification)
	case "notifications/resources/updated":
		//handleResourceUpdateNotification(notification)
	case "notifications/tools/updated":
		// 收到工具更新通知，刷新工具列表
		logger.Infof("收到工具更新通知，刷新工具列表")
		go dc.refreshToolsOnNotification()
	default:
		log.Printf("Unknown notification: %s", notification.Method)
	}
}

// refreshToolsOnNotification 基于通知刷新工具列表
func (dc *McpClientInstance) refreshToolsOnNotification() {
	// 添加短暂延迟避免频繁刷新
	time.Sleep(100 * time.Millisecond)
	dc.refreshTools()
}

// handleJSONRPCError 处理JSON-RPC错误
func (dc *McpClientInstance) handleJSONRPCError(errMsg mcp.JSONRPCError) error {
	logger.Errorf("收到MCP服务器错误: %+v", errMsg.Error)
	return nil
}

// handleTransportClose 处理transport层关闭事件
func (dc *McpClientInstance) handleTransportClose(reason string) {
	dc.closeWithReason(reason)
}

// SetOnCloseHandler 设置关闭回调
func (dc *McpClientInstance) SetOnCloseHandler(handler func(instance *McpClientInstance, reason string)) {
	dc.onCloseHandler = handler
}

// IsConnected 检查连接是否仍然活跃
func (dc *McpClientInstance) IsConnected() bool {
	return dc.connected
}

func (dc *DeviceMcpSession) ShouldScheduleIotInit(transportType string, conn ConnInterface) bool {
	transportType = normalizeDeviceTransportType(transportType)
	if transportType == "unknown" || conn == nil {
		return false
	}

	dc.iotMux.RLock()
	existing := dc.iotOverMcpByTransport[transportType]
	dc.iotMux.RUnlock()
	if existing == nil {
		return true
	}
	if existing.conn != conn {
		return true
	}

	switch existing.getInitState() {
	case mcpClientInitStateReady, mcpClientInitStateInitializing:
		return false
	default:
		return true
	}
}

// GetConnectionStatus 获取连接状态信息
func (dc *McpClientInstance) GetConnectionStatus() map[string]interface{} {
	dc.toolsMux.RLock()
	toolsCount := len(dc.tools)
	dc.toolsMux.RUnlock()

	initState := "idle"
	switch dc.getInitState() {
	case mcpClientInitStateInitializing:
		initState = "initializing"
	case mcpClientInitStateReady:
		initState = "ready"
	}

	return map[string]interface{}{
		"server_name": dc.serverName,
		"connected":   dc.connected,
		"init_state":  initState,
		"last_ping":   dc.lastPing,
		"tools_count": toolsCount,
	}
}

func (dc *McpClientInstance) RawCallTool(ctx context.Context, toolName string, arguments map[string]interface{}) (string, error) {
	if dc == nil || dc.mcpClient == nil {
		return "", fmt.Errorf("MCP客户端未初始化")
	}
	if !dc.IsConnected() || !dc.IsInitialized() {
		return "", fmt.Errorf("MCP客户端未就绪")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := dc.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		return "", fmt.Errorf("调用工具失败: %v", err)
	}

	resultBytes, err := result.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("工具调用返回内容转换失败: %v", err)
	}
	return string(resultBytes), nil
}

// GetTools 获取工具列表
func (dc *DeviceMcpSession) GetTools() map[string]tool.InvokableTool {
	tools := make(map[string]tool.InvokableTool)
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		for k, v := range mcpInstance.tools {
			tools[k] = v
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})

	dc.iotMux.RLock()
	for _, iotClient := range dc.iotOverMcpByTransport {
		iotClient.toolsMux.RLock()
		for k, v := range iotClient.tools {
			tools[k] = v
		}
		iotClient.toolsMux.RUnlock()
	}
	dc.iotMux.RUnlock()
	return tools
}

func (dc *DeviceMcpSession) GetWsEndpointMcpTools() map[string]tool.InvokableTool {
	tools := make(map[string]tool.InvokableTool)
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		for k, v := range mcpInstance.tools {
			tools[k] = v
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})
	return tools
}

// GetPreferredIotTransportType 返回当前设备最适合用于设备维度 MCP 查询/调用的 transport。
// 优先选择仍处于 connected 状态且最近有心跳的 transport；如果都不活跃，则退回最近一次存在的 transport。
func (dc *DeviceMcpSession) GetPreferredIotTransportType() string {
	dc.iotMux.RLock()
	defer dc.iotMux.RUnlock()

	preferredTransport := ""
	var preferredClient *McpClientInstance
	isSupportedTransport := func(transportType string) bool {
		switch normalizeDeviceTransportType(transportType) {
		case "websocket", "udp", "mqtt_udp":
			return true
		default:
			return false
		}
	}

	selectPreferred := func(connectedOnly bool) string {
		preferredTransport = ""
		preferredClient = nil
		for transportType, iotClient := range dc.iotOverMcpByTransport {
			transportType = normalizeDeviceTransportType(transportType)
			if iotClient == nil {
				continue
			}
			if !isSupportedTransport(transportType) {
				continue
			}
			if connectedOnly && !iotClient.IsConnected() {
				continue
			}
			if preferredClient == nil {
				preferredTransport = transportType
				preferredClient = iotClient
				continue
			}
			if iotClient.lastPing.After(preferredClient.lastPing) {
				preferredTransport = transportType
				preferredClient = iotClient
				continue
			}
			if iotClient.lastPing.Equal(preferredClient.lastPing) && transportType < preferredTransport {
				preferredTransport = transportType
				preferredClient = iotClient
			}
		}
		return preferredTransport
	}

	if transportType := selectPreferred(true); transportType != "" {
		return transportType
	}
	return selectPreferred(false)
}

func (dc *DeviceMcpSession) GetIotToolsByTransport(transportType string) map[string]tool.InvokableTool {
	transportType = strings.TrimSpace(transportType)
	tools := make(map[string]tool.InvokableTool)
	if transportType == "" {
		return tools
	}

	dc.iotMux.RLock()
	iotClient := dc.iotOverMcpByTransport[transportType]
	dc.iotMux.RUnlock()
	if iotClient == nil {
		return tools
	}

	iotClient.toolsMux.RLock()
	for k, v := range iotClient.tools {
		tools[k] = v
	}
	iotClient.toolsMux.RUnlock()

	return tools
}

func (dc *DeviceMcpSession) GetIotToolByTransportAndName(transportType, toolName string) (tool.InvokableTool, bool) {
	transportType = strings.TrimSpace(transportType)
	if transportType == "" {
		return nil, false
	}

	dc.iotMux.RLock()
	iotClient := dc.iotOverMcpByTransport[transportType]
	dc.iotMux.RUnlock()
	if iotClient == nil {
		return nil, false
	}

	iotClient.toolsMux.RLock()
	defer iotClient.toolsMux.RUnlock()
	invokable, ok := iotClient.tools[toolName]
	return invokable, ok
}

func (dc *DeviceMcpSession) RawCallIotToolByTransport(ctx context.Context, transportType, toolName string, arguments map[string]interface{}) (string, bool, error) {
	transportType = strings.TrimSpace(transportType)
	if transportType == "" {
		return "", false, nil
	}

	dc.iotMux.RLock()
	iotClient := dc.iotOverMcpByTransport[transportType]
	dc.iotMux.RUnlock()
	if iotClient == nil || !iotClient.IsConnected() || !iotClient.IsInitialized() {
		return "", false, nil
	}

	result, err := iotClient.RawCallTool(ctx, toolName, arguments)
	return result, true, err
}

func (dc *DeviceMcpSession) RawCallWsEndpointTool(ctx context.Context, toolName string, arguments map[string]interface{}) (string, bool, error) {
	var selected *McpClientInstance
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		if mcpInstance == nil || !mcpInstance.IsConnected() || !mcpInstance.IsInitialized() {
			return true
		}
		selected = mcpInstance
		return false
	})
	if selected == nil {
		return "", false, nil
	}

	result, err := selected.RawCallTool(ctx, toolName, arguments)
	return result, true, err
}

func (dc *DeviceMcpSession) GetToolByName(toolName string) (tool tool.InvokableTool, ok bool) {
	dc.wsEndPointMcp.Range(func(_, value interface{}) bool {
		mcpInstance := value.(*McpClientInstance)
		mcpInstance.toolsMux.RLock()
		logger.Infof("wsEndPointMcp 工具列表: %+v", mcpInstance.tools)
		if tool, ok = mcpInstance.tools[toolName]; ok {
			mcpInstance.toolsMux.RUnlock()
			return false
		}
		mcpInstance.toolsMux.RUnlock()
		return true
	})
	if ok {
		return tool, true
	}

	dc.iotMux.RLock()
	defer dc.iotMux.RUnlock()
	for transportType, iotClient := range dc.iotOverMcpByTransport {
		iotClient.toolsMux.RLock()
		logger.Infof("iotOverMcp 工具列表(%s): %+v", transportType, iotClient.tools)
		if tool, ok = iotClient.tools[toolName]; ok {
			iotClient.toolsMux.RUnlock()
			return tool, true
		}
		iotClient.toolsMux.RUnlock()
	}
	return nil, false
}
