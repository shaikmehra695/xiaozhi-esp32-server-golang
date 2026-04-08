package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/domain/eventbus"
	"xiaozhi-esp32-server-golang/internal/domain/mcp"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcp_go "github.com/mark3labs/mcp-go/mcp"
)

// handleToolCallResponse 处理工具调用响应
func (l *LLMManager) handleToolCallResponse(ctx context.Context, respMsg *schema.Message, tools []schema.ToolCall, executor *toolCallExecutor) (bool, error) {
	if len(tools) == 0 {
		return false, nil
	}

	log.Infof("处理 %d 个工具调用", len(tools))
	if executor == nil {
		executor = newToolCallExecutor(l, ctx)
		executor.Submit(tools)
	}

	var messageList []*schema.Message

	// 只有当respMsg有内容（Content不为空或ToolCalls不为空）时才添加到messageList
	// 避免保存空的assistant消息导致后续LLM调用出现400错误
	if respMsg != nil && (respMsg.Content != "" || len(respMsg.ToolCalls) > 0) {
		messageList = append(messageList, respMsg)
	}

	invokeToolSuccess := false
	findExitTool := false
	shouldStopLLMProcessing := false

	results := executor.Wait()
	for _, result := range results {
		if result.invokeToolSuccess {
			invokeToolSuccess = true
		}
		if result.findExitTool {
			findExitTool = true
		}
		if result.shouldStopLLMProcessing {
			shouldStopLLMProcessing = true
		}
		messageList = append(messageList, result.message)
	}

	if len(messageList) > 0 {
		for _, msg := range messageList {
			// 过滤掉Content为空的assistant消息，避免保存到历史记录中
			// 空的assistant消息会导致后续LLM调用时出现400错误
			if msg != nil && msg.Role == schema.Assistant && msg.Content == "" && len(msg.ToolCalls) == 0 {
				log.Debugf("跳过保存空的assistant消息")
				continue
			}
			l.AddLlmMessage(ctx, msg)
		}
	}

	executor.WaitMedia()

	if findExitTool {
		// 发布退出聊天事件
		eventbus.Get().Publish(eventbus.TopicExitChat, &eventbus.ExitChatEvent{
			ClientState: l.clientState,
			Reason:      "工具调用退出",
			TriggerType: "tool_call",
			UserText:    "",
			Timestamp:   time.Now(),
		})

		return invokeToolSuccess, nil
	}

	// 如果工具调用成功且没有被标记为停止处理，则继续LLM调用
	if invokeToolSuccess && !shouldStopLLMProcessing {
		l.DoLLmRequest(ctx, nil, l.einoTools, true, nil)
	}

	return invokeToolSuccess, nil
}

type toolCallExecutionResult struct {
	order                   int
	message                 *schema.Message
	invokeToolSuccess       bool
	findExitTool            bool
	shouldStopLLMProcessing bool
}

type toolCallExecutor struct {
	manager *LLMManager
	ctx     context.Context
	toolCtx context.Context

	mu             sync.Mutex
	submittedCall  map[string]struct{}
	submitSequence int
	results        []toolCallExecutionResult

	wg      sync.WaitGroup
	mediaWg sync.WaitGroup
}

func newToolCallExecutor(manager *LLMManager, ctx context.Context) *toolCallExecutor {
	toolCtx := ctx
	if chatSessionOperator, ok := ctx.Value("chat_session_operator").(ChatSessionOperator); ok {
		toolCtx = context.WithValue(ctx, "chat_session_operator", chatSessionOperator)
	}

	return &toolCallExecutor{
		manager:       manager,
		ctx:           ctx,
		toolCtx:       toolCtx,
		submittedCall: make(map[string]struct{}),
	}
}

func (e *toolCallExecutor) Submit(toolCalls []schema.ToolCall) {
	for _, tc := range toolCalls {
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("auto_%s_%s", tc.Function.Name, tc.Function.Arguments)
		}

		e.mu.Lock()
		if _, exists := e.submittedCall[callID]; exists {
			e.mu.Unlock()
			continue
		}
		e.submittedCall[callID] = struct{}{}
		order := e.submitSequence
		e.submitSequence++
		e.wg.Add(1)
		e.mu.Unlock()

		toolCall := tc
		go func() {
			defer e.wg.Done()
			result := e.executeToolCall(order, toolCall)

			e.mu.Lock()
			e.results = append(e.results, result)
			e.mu.Unlock()
		}()
	}
}

func (e *toolCallExecutor) Wait() []toolCallExecutionResult {
	e.wg.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	results := append([]toolCallExecutionResult(nil), e.results...)
	sort.Slice(results, func(i, j int) bool {
		return results[i].order < results[j].order
	})
	return results
}

func (e *toolCallExecutor) WaitMedia() {
	e.mediaWg.Wait()
}

func (e *toolCallExecutor) executeToolCall(order int, toolCall schema.ToolCall) toolCallExecutionResult {
	resultMessage := &schema.Message{
		Role:       schema.Tool,
		ToolCallID: toolCall.ID,
	}

	state := e.manager.clientState
	toolName := toolCall.Function.Name
	toolObj, ok := mcp.GetToolByName(state.DeviceID, state.AgentID, toolName, state.DeviceConfig.MCPServiceNames)
	if !ok || toolObj == nil {
		log.Errorf("未找到工具: %s", toolName)
		resultMessage.Content = fmt.Sprintf("未找到工具: %s", toolName)
		return toolCallExecutionResult{order: order, message: resultMessage}
	}

	log.Infof("进行工具调用请求: %s, 参数: %+v", toolName, toolCall.Function.Arguments)
	startTs := time.Now().UnixMilli()
	fcResult, err := toolObj.InvokableRun(e.toolCtx, toolCall.Function.Arguments)
	if err != nil {
		log.Errorf("工具调用失败: %v", err)
		resultMessage.Content = fmt.Sprintf("工具 %s 调用失败: %v", toolName, err)
		return toolCallExecutionResult{order: order, message: resultMessage}
	}
	costTs := time.Now().UnixMilli() - startTs
	if len(fcResult) > 2048 {
		log.Infof("工具调用结果 len: %d, 耗时: %dms", len(fcResult), costTs)
	} else {
		log.Infof("工具调用结果 %s, 耗时: %dms", fcResult, costTs)
	}

	execResult := toolCallExecutionResult{
		order:             order,
		message:           resultMessage,
		invokeToolSuccess: true,
	}
	normalizedContent := fcResult

	var contentList []mcp_go.Content
	if mcpResp, ok := e.manager.handleLocalToolResult(fcResult); ok {
		if mcpResp.GetType() == MCPResponseTypeAction && mcpResp.GetAction() == "exit_conversation" {
			execResult.findExitTool = true
		}
		contentList = mcpResp.GetContent()
	} else if toolCallResult, ok := e.manager.handleToolResult(fcResult); ok {
		if toolCallResult.IsError {
			log.Errorf("工具调用失败: %s, 错误标记: %t", fcResult, toolCallResult.IsError)
		}
		contentList = toolCallResult.Content
	}

	if len(contentList) > 0 {
		var mcpContent string
		for _, content := range contentList {
			if audioContent, ok := content.(mcp_go.AudioContent); ok {
				log.Debugf("调用工具 %s 返回音频资源长度: %d", toolName, len(audioContent.Data))
				mcpContent = "执行成功"
				if err := e.manager.handleAudioContent(e.ctx, mcpContent, audioContent, &e.mediaWg); err != nil {
					log.Errorf("mcp播放音频资源失败: %v", err)
					mcpContent = "执行失败"
				}
				execResult.shouldStopLLMProcessing = true
				break
			}
			if resourceLink, ok := content.(mcp_go.ResourceLink); ok {
				log.Debugf("调用工具 %s 返回资源链接: %+v", toolName, resourceLink)
				mcpContent = "执行成功"
				if err := e.manager.handleResourceLink(e.ctx, resourceLink, toolObj, &e.mediaWg); err != nil {
					log.Errorf("mcp播放资源链接失败: %v", err)
					mcpContent = "执行失败"
				}
				execResult.shouldStopLLMProcessing = true
				break
			}
			if textContent, ok := content.(mcp_go.TextContent); ok {
				log.Debugf("调用工具 %s 返回文本资源长度: %s", toolName, textContent.Text)
				mcpContent += textContent.Text
			}
		}
		if mcpContent != "" {
			normalizedContent = mcpContent
		}
	}

	execResult.message.Content = normalizedContent
	return execResult
}

func (l *LLMManager) handleResourceLink(ctx context.Context, resourceLink mcp_go.ResourceLink, toolCall tool.InvokableTool, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()
	//从resourceLink中获取资源
	client := toolCall.(*mcp.McpTool).GetClient()

	var pipeReader *io.PipeReader
	var pipeWriter *io.PipeWriter
	pipeReader, pipeWriter = io.Pipe()

	audioFormat := util.GetAudioFormatByMimeType(resourceLink.MIMEType)

	streamChan := make(chan []byte, 0) // 增加缓冲区大小
	go func() error {
		defer func() {
			close(streamChan)
		}()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case audioData, ok := <-streamChan:
					if !ok {
						pipeWriter.Close()
						return
					}
					if _, err := pipeWriter.Write(audioData); err != nil {
						log.Errorf("写入pipe失败: %v", err)
						return
					}
				}
			}
		}()

		start := 0
		page := McpReadResourcePageSize
		totalRead := 0
		pageCount := 0

		log.Infof("开始读取资源: %s, 分页大小: %d", resourceLink.URI, page)

		for {
			select {
			case <-ctx.Done():
				log.Infof("上下文取消，停止读取资源")
				return nil
			default:
			}

			pageCount++
			readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			resourceResult, err := client.ReadResource(readCtx, mcp_go.ReadResourceRequest{
				Params: mcp_go.ReadResourceParams{
					URI:    resourceLink.URI,
					Offset: start,
					Limit:  page,
				},
			})
			cancel()

			if err != nil {
				log.Errorf("读取资源失败 (offset=%d, page=%d): %v", start, pageCount, err)
				return err
			}

			if resourceResult == nil || len(resourceResult.Contents) == 0 {
				log.Infof("资源读取完成，总页数: %d，总字节数: %d", pageCount-1, totalRead)
				break
			}

			pageReadBytes := 0
			hasData := false

			for i, content := range resourceResult.Contents {
				if audioContent, ok := content.(mcp_go.BlobResourceContents); ok {
					if audioContent.Blob == "" {
						continue
					}

					if audioContent.Blob == McpReadResourceStreamDoneFlag {
						log.Infof("收到流结束标记，总页数: %d，总字节数: %d", pageCount, totalRead)
						return nil
					}

					audioData := []byte(audioContent.Blob)
					dataLen := len(audioData)
					if dataLen > 0 {
						hasData = true
						pageReadBytes += dataLen
						totalRead += dataLen

						select {
						case streamChan <- audioData:
							log.Debugf("第%d页第%d块: 读取%d字节，总计%d字节", pageCount, i+1, dataLen, totalRead)
						case <-ctx.Done():
							log.Infof("上下文取消，停止发送音频数据")
							return nil
						}
					}
				}
			}

			if !hasData {
				log.Infof("第%d页无有效音频数据，读取结束", pageCount)
				break
			}

			start += pageReadBytes
			log.Infof("第%d页读取完成: %d字节，下一页offset: %d", pageCount, pageReadBytes, start)

			if pageReadBytes < page {
				log.Infof("第%d页数据不足一页(%d < %d)，读取完成，总字节数: %d", pageCount, pageReadBytes, page, totalRead)
				break
			}
		}

		return nil
	}()

	musicName := strings.TrimSpace(resourceLink.Name)
	if musicName == "" {
		musicName = "tool_resource"
	}
	err := l.clientState.SendAudioToClientAsync(ctx, pipeReader, musicName, audioFormat, 0)
	if err != nil {
		log.Errorf("发送音频到客户端失败: %v", err)
		return err
	}
	go func() {
		<-ctx.Done()
		log.Debugf("关闭tool pipe reader")
		pipeReader.Close()
	}()
	return nil
}

func (l *LLMManager) handleAudioContent(ctx context.Context, realMusicName string, audioContent mcp_go.AudioContent, wg *sync.WaitGroup) error {
	wg.Add(1)
	defer wg.Done()
	audioBytes := audioContent.Data
	_, err := l.clientState.SendAudioToClient(ctx, audioBytes, realMusicName, util.GetAudioFormatByMimeType(audioContent.MIMEType), 0)
	if err != nil {
		return err
	}
	return nil
}

func (l *LLMManager) handleLocalToolResult(toolResult string) (MCPResponse, bool) {
	// 如果是mcp返回格式, 则解析
	var response MCPResponse
	var err error
	if response, err = ParseMCPResponse(toolResult); err != nil {
		return nil, false
	}
	return response, true
}

func (l *LLMManager) handleToolResult(toolResultStr string) (mcp_go.CallToolResult, bool) {
	var toolResult mcp_go.CallToolResult
	if err := json.Unmarshal([]byte(toolResultStr), &toolResult); err != nil {
		log.Debugf("Failed to unmarshal tool result: %v", err)
		toolResult.Content = []mcp_go.Content{
			mcp_go.TextContent{
				Type: "text",
				Text: toolResultStr,
			},
		}
		return toolResult, true
	}

	return toolResult, true
}
