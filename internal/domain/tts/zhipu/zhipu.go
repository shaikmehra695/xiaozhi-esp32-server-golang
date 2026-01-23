package zhipu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"xiaozhi-esp32-server-golang/internal/data/audio"
	"xiaozhi-esp32-server-golang/internal/util"
	log "xiaozhi-esp32-server-golang/logger"
)

// 全局HTTP客户端，实现连接池
var (
	httpClient     *http.Client
	httpClientOnce sync.Once
)

// 获取配置了连接池的HTTP客户端
func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second, // 智谱TTS可能需要更长时间
		}
	})
	return httpClient
}

// ZhipuTTSProvider 智谱TTS提供者
type ZhipuTTSProvider struct {
	APIKey         string
	APIURL         string
	Model          string
	Voice          string
	ResponseFormat string
	Speed          float64
	Volume         float64
	Stream         bool
	FrameDuration  int
}

// 请求结构体
type zhipuRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
	Volume         float64 `json:"volume,omitempty"`
	Stream         bool    `json:"stream,omitempty"`
}

// NewZhipuTTSProvider 创建新的智谱TTS提供者
func NewZhipuTTSProvider(config map[string]interface{}) *ZhipuTTSProvider {
	apiKey, _ := config["api_key"].(string)
	apiURL, _ := config["api_url"].(string)
	model, _ := config["model"].(string)
	voice, _ := config["voice"].(string)
	responseFormat, _ := config["response_format"].(string)
	speed, _ := config["speed"].(float64)
	volume, _ := config["volume"].(float64)
	stream, _ := config["stream"].(bool)
	frameDuration, _ := config["frame_duration"].(float64)

	// 设置默认值
	if apiURL == "" {
		apiURL = "https://open.bigmodel.cn/api/paas/v4/audio/speech"
	}
	if model == "" {
		model = "glm-tts"
	}
	if voice == "" {
		voice = "tongtong" // 默认音色
	}
	// 智谱API只支持wav和pcm格式
	if responseFormat == "" {
		if stream {
			// 流式输出时仅支持pcm
			responseFormat = "pcm"
		} else {
			// 非流式默认使用wav
			responseFormat = "wav"
		}
	}
	// 验证格式是否支持
	if responseFormat != "wav" && responseFormat != "pcm" {
		log.Warnf("智谱API不支持的格式 %s，将使用默认格式 wav", responseFormat)
		if stream {
			responseFormat = "pcm"
		} else {
			responseFormat = "wav"
		}
	}
	// 流式输出时强制使用pcm
	if stream && responseFormat != "pcm" {
		log.Warnf("流式输出时仅支持pcm格式，将格式从 %s 改为 pcm", responseFormat)
		responseFormat = "pcm"
	}
	if speed == 0 {
		speed = 1.0 // 0.5 到 2.0
	}
	if volume == 0 {
		volume = 1.0 // (0, 10]
	}
	if frameDuration == 0 {
		frameDuration = audio.FrameDuration
	}

	return &ZhipuTTSProvider{
		APIKey:         apiKey,
		APIURL:         apiURL,
		Model:          model,
		Voice:          voice,
		ResponseFormat: responseFormat,
		Stream:         stream,
		Speed:          speed,
		Volume:         volume,
		FrameDuration:  int(frameDuration),
	}
}

// TextToSpeech 将文本转换为语音，返回音频帧数据和错误
func (p *ZhipuTTSProvider) TextToSpeech(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) ([][]byte, error) {
	startTs := time.Now().UnixMilli()

	// 创建请求体
	reqBody := zhipuRequest{
		Model:          p.Model,
		Input:          text,
		Voice:          p.Voice,
		ResponseFormat: p.ResponseFormat,
		Speed:          p.Speed,
		Volume:         p.Volume,
		Stream:         false, // 非流式请求
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	// 使用连接池发送请求
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	// 检查响应内容长度
	contentLength := resp.ContentLength
	log.Debugf("收到智谱TTS响应，Content-Length: %d", contentLength)

	// 判断Content-Length是否合理
	if contentLength == 0 {
		log.Errorf("API返回空响应，Content-Length为0")
		return nil, fmt.Errorf("API返回空响应，Content-Length为0")
	}

	// 根据音频格式处理响应
	if p.ResponseFormat == "wav" || p.ResponseFormat == "pcm" {
		// 创建一个通道来收集音频帧
		outputChan := make(chan []byte, 1000)

		// 创建音频解码器
		decoder, err := util.CreateAudioDecoder(ctx, resp.Body, outputChan, frameDuration, p.ResponseFormat)
		if err != nil {
			return nil, fmt.Errorf("创建音频解码器失败: %v", err)
		}

		// 启动解码过程
		go func() {
			if err := decoder.Run(startTs); err != nil {
				log.Errorf("音频解码失败: %v", err)
			}
		}()

		// 收集所有的音频帧
		var audioFrames [][]byte
		for frame := range outputChan {
			audioFrames = append(audioFrames, frame)
		}

		log.Infof("智谱TTS完成，从输入到获取音频数据结束耗时: %d ms", time.Now().UnixMilli()-startTs)
		return audioFrames, nil
	}

	return nil, fmt.Errorf("不支持的音频格式: %s，智谱API仅支持wav和pcm格式", p.ResponseFormat)
}

// TextToSpeechStream 流式语音合成实现
func (p *ZhipuTTSProvider) TextToSpeechStream(ctx context.Context, text string, sampleRate int, channels int, frameDuration int) (outputChan chan []byte, err error) {
	startTs := time.Now().UnixMilli()

	// 流式输出时强制使用pcm格式
	responseFormat := p.ResponseFormat
	if responseFormat != "pcm" {
		log.Warnf("流式输出时仅支持pcm格式，将格式从 %s 改为 pcm", responseFormat)
		responseFormat = "pcm"
	}

	// 创建请求体
	reqBody := zhipuRequest{
		Model:          p.Model,
		Input:          text,
		Voice:          p.Voice,
		ResponseFormat: responseFormat,
		Speed:          p.Speed,
		Volume:         p.Volume,
		Stream:         true, // 启用流式输出
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", p.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.APIKey))

	// 使用连接池创建客户端
	client := getHTTPClient()

	// 创建输出通道
	outputChan = make(chan []byte, 100)

	// 启动goroutine处理流式响应
	go func() {
		// 发送请求
		resp, err := client.Do(req)
		if err != nil {
			log.Errorf("发送智谱请求失败: %v", err)
			close(outputChan)
			return
		}
		defer resp.Body.Close()

		// 检查响应状态码
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Errorf("智谱API请求失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
			close(outputChan)
			return
		}

		// 检查响应内容长度
		contentLength := resp.ContentLength
		log.Debugf("收到智谱TTS响应，Content-Length: %d", contentLength)

		// 判断Content-Length是否合理
		if contentLength == 0 {
			log.Errorf("智谱API返回空响应，Content-Length为0")
			close(outputChan)
			return
		}

		// 流式输出仅支持pcm格式
		if responseFormat == "pcm" {
			// 创建音频解码器
			decoder, err := util.CreateAudioDecoder(ctx, resp.Body, outputChan, frameDuration, responseFormat)
			if err != nil {
				log.Errorf("创建智谱音频解码器失败: %v", err)
				close(outputChan)
				return
			}

			// 启动解码过程
			if err := decoder.Run(startTs); err != nil {
				log.Errorf("智谱音频解码失败: %v", err)
				return
			}

			select {
			case <-ctx.Done():
				log.Debugf("智谱TTS流式合成取消, 文本: %s", text)
				return
			default:
				log.Infof("智谱TTS耗时: 从输入至获取音频数据结束耗时: %d ms", time.Now().UnixMilli()-startTs)
			}
		} else {
			log.Errorf("智谱流式输出仅支持PCM格式")
			close(outputChan)
		}
	}()

	return outputChan, nil
}

// SetVoice 设置音色参数
func (p *ZhipuTTSProvider) SetVoice(voiceConfig map[string]interface{}) error {
	if voice, ok := voiceConfig["voice"].(string); ok && voice != "" {
		p.Voice = voice
		return nil
	}
	return fmt.Errorf("无效的音色配置: 缺少 voice")
}

// Close 关闭资源（无状态 Provider，无需关闭）
func (p *ZhipuTTSProvider) Close() error {
	return nil
}

// IsValid 检查资源是否有效
func (p *ZhipuTTSProvider) IsValid() bool {
	return p != nil
}
