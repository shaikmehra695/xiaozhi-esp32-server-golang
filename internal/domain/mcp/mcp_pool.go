package mcp

import (
	"time"

	"github.com/cloudwego/eino/components/tool"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type McpClientPool struct {
	device2McpClient cmap.ConcurrentMap[string, *DeviceMcpSession]
}

var mcpClientPool *McpClientPool

func init() {
	mcpClientPool = &McpClientPool{
		device2McpClient: cmap.New[*DeviceMcpSession](),
	}
	go mcpClientPool.checkOffline()
}

func (p *McpClientPool) GetMcpClient(deviceID string) *DeviceMcpSession {
	client, ok := p.device2McpClient.Get(deviceID)
	if !ok {
		return nil
	}
	return client
}

func (p *McpClientPool) RemoveMcpClient(deviceID string) {
	p.device2McpClient.Remove(deviceID)
}

func (p *McpClientPool) AddMcpClient(deviceID string, client *DeviceMcpSession) {
	p.device2McpClient.Set(deviceID, client)
}

func (p *McpClientPool) GetToolByDeviceId(deviceId string, toolsName string) (tool.InvokableTool, bool) {
	client := p.GetMcpClient(deviceId)
	if client == nil {
		return nil, false
	}
	return client.GetToolByName(toolsName)
}

func (p *McpClientPool) GetAllToolsByDeviceIdAndAgentId(deviceId string, agentId string) (map[string]tool.InvokableTool, error) {
	retTools := make(map[string]tool.InvokableTool)
	deviceClient := p.GetMcpClient(deviceId)
	if deviceClient != nil {
		deviceTools := deviceClient.GetTools()
		for toolName, tool := range deviceTools {
			retTools[toolName] = tool
		}
	}
	agentClient := p.GetMcpClient(agentId)
	if agentClient != nil {
		agentTools := agentClient.GetTools()
		for toolName, tool := range agentTools {
			retTools[toolName] = tool
		}
	}
	return retTools, nil
}

func (p *McpClientPool) GetWsEndpointMcpTools(agentId string) (map[string]tool.InvokableTool, error) {
	retTools := make(map[string]tool.InvokableTool)
	agentClient := p.GetMcpClient(agentId)
	if agentClient == nil {
		return retTools, nil
	}
	agentTools := agentClient.GetWsEndpointMcpTools()
	for toolName, tool := range agentTools {
		retTools[toolName] = tool
	}
	return retTools, nil
}

func (p *McpClientPool) checkOffline() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		for _, deviceSession := range p.device2McpClient.Items() {
			// 检查WebSocket端点MCP连接
			hasActiveWsConnections := false
			deviceSession.wsEndPointMcp.Range(func(_, value interface{}) bool {
				wsInstance := value.(*McpClientInstance)
				if time.Since(wsInstance.lastPing) > 2*time.Minute {
					wsInstance.connected = false
					wsInstance.cancel()
					deviceSession.RemoveWsEndPointMcp(wsInstance)
				} else {
					hasActiveWsConnections = true
				}
				return true // continue
			})

			// 检查IoT over MCP连接
			hasActiveIotConnection := false
			deviceSession.iotMux.Lock()
			if deviceSession.iotOverMcp != nil {
				if time.Since(deviceSession.iotOverMcp.lastPing) > 2*time.Minute {
					deviceSession.iotOverMcp.connected = false
					deviceSession.iotOverMcp.cancel()
					deviceSession.iotOverMcp = nil
				} else {
					hasActiveIotConnection = true
				}
			}
			deviceSession.iotMux.Unlock()

			// 如果没有任何活跃连接，移除设备维度MCP会话
			if !hasActiveWsConnections && !hasActiveIotConnection {
				deviceSession.cancel()
				p.RemoveMcpClient(deviceSession.deviceID)
			}
		}
	}
}
