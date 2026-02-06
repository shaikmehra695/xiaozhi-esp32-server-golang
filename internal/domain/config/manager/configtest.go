package manager

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"

	"xiaozhi-esp32-server-golang/internal/domain/asr"
	"xiaozhi-esp32-server-golang/internal/domain/llm"
	"xiaozhi-esp32-server-golang/internal/domain/tts"
	"xiaozhi-esp32-server-golang/internal/domain/vad/inter"
	"xiaozhi-esp32-server-golang/internal/pool"
	log "xiaozhi-esp32-server-golang/logger"
)

// DefaultTestWavPath 配置测试用固定 WAV 路径（16kHz 单声道，约 1–3 秒），可选
const DefaultTestWavPath = "internal/testdata/config_test.wav"

// DefaultTestText LLM/TTS 固定测试文本
const DefaultTestText = "配置测试"

// 用于 VAD/ASR 的备用 PCM：1 秒静音 16kHz 单声道，无文件时使用
var fallbackPCM = make([]float32, 16000)

// loadTestWav 加载固定 WAV 为 float32 PCM，若文件不存在则返回 nil 与 nil error（调用方用 fallbackPCM）
func loadTestWav(path string) ([]float32, error) {
	if path == "" {
		path = DefaultTestWavPath
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	dec := wav.NewDecoder(f)
	if !dec.IsValidFile() {
		return nil, nil
	}
	dec.ReadInfo()
	wavFmt := dec.Format()
	frameSize := int(wavFmt.SampleRate) * 20 / 1000 * wavFmt.NumChannels
	buf := &audio.IntBuffer{Format: wavFmt, SourceBitDepth: 16, Data: make([]int, frameSize)}
	var out []float32
	for {
		n, err := dec.PCMBuffer(buf)
		if err == io.EOF || n == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		for i := 0; i < n; i++ {
			out = append(out, float32(buf.Data[i])/32767.0)
		}
	}
	return out, nil
}

// RunConfigTest 根据下发的 data（与实时配置一致）执行 VAD/ASR/LLM/TTS 轻量测试，返回每类按 config_id 的结果
func RunConfigTest(data map[string]interface{}, testText string) (vadResult, asrResult, llmResult, ttsResult map[string]interface{}) {
	vadResult = make(map[string]interface{})
	asrResult = make(map[string]interface{})
	llmResult = make(map[string]interface{})
	ttsResult = make(map[string]interface{})

	if testText == "" {
		testText = DefaultTestText
	}
	log.Debugf("[config_test] RunConfigTest 开始 test_text=%q data.keys=%v", testText, mapKeys(data))

	pcm, _ := loadTestWav(DefaultTestWavPath)
	if pcm == nil || len(pcm) == 0 {
		pcm = fallbackPCM
	}

	// VAD
	if v, ok := data["vad"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				continue
			}
			wrapper, err := pool.Acquire[inter.VAD]("vad", configID, cfg)
			if err != nil {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			vad := wrapper.GetProvider()
			_, err = vad.IsVAD(pcm[:min(320, len(pcm))])
			pool.Release(wrapper)
			if err != nil {
				vadResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
			} else {
				vadResult[configID] = map[string]interface{}{"ok": true, "message": "通过"}
			}
		}
	}

	// ASR
	if v, ok := data["asr"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				continue
			}
			wrapper, err := pool.Acquire[asr.AsrProvider]("asr", configID, cfg)
			if err != nil {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			asrProvider := wrapper.GetProvider()
			_, err = asrProvider.Process(pcm)
			pool.Release(wrapper)
			if err != nil {
				asrResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
			} else {
				asrResult[configID] = map[string]interface{}{"ok": true, "message": "通过"}
			}
		}
	}

	// LLM
	if v, ok := data["llm"].(map[string]interface{}); ok {
		n := 0
		for k := range v {
			if k != "provider" {
				n++
			}
		}
		log.Debugf("[config_test] LLM 待测 config 数: %d", n)
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				llmResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				log.Debugf("[config_test] LLM config_id=%s 配置格式无效", configID)
				continue
			}
			wrapper, err := pool.Acquire[llm.LLMProvider]("llm", configID, cfg)
			if err != nil {
				llmResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				log.Debugf("[config_test] LLM config_id=%s Acquire 失败: %v", configID, err)
				continue
			}
			llmProvider := wrapper.GetProvider()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			msgChan := llmProvider.ResponseWithContext(ctx, "config_test", []*schema.Message{
				{Role: "user", Content: testText},
			}, nil)
			var gotMessage bool
			for range msgChan {
				gotMessage = true
				break
			}
			cancel()
			pool.Release(wrapper)
			if gotMessage {
				llmResult[configID] = map[string]interface{}{"ok": true, "message": "通过"}
				log.Debugf("[config_test] LLM config_id=%s 通过", configID)
			} else if ctx.Err() == context.DeadlineExceeded {
				llmResult[configID] = map[string]interface{}{"ok": false, "message": "超时"}
				log.Debugf("[config_test] LLM config_id=%s 超时", configID)
			} else {
				llmResult[configID] = map[string]interface{}{"ok": true, "message": "通过"}
				log.Debugf("[config_test] LLM config_id=%s 通过(无消息)", configID)
			}
		}
	} else {
		log.Debugf("[config_test] LLM data.llm 缺失或非 map, ok=%v", ok)
	}

	// TTS
	if v, ok := data["tts"].(map[string]interface{}); ok {
		for configID, val := range v {
			if configID == "provider" {
				continue
			}
			cfg, ok := val.(map[string]interface{})
			if !ok {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": "配置格式无效"}
				continue
			}
			wrapper, err := pool.Acquire[tts.TTSProvider]("tts", configID, cfg)
			if err != nil {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				continue
			}
			ttsProvider := wrapper.GetProvider()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			_, err = ttsProvider.TextToSpeech(ctx, testText, 24000, 1, 60)
			cancel()
			pool.Release(wrapper)
			if err != nil {
				ttsResult[configID] = map[string]interface{}{"ok": false, "message": err.Error()}
				log.Warnf("TTS config test %s: %v", configID, err)
			} else {
				ttsResult[configID] = map[string]interface{}{"ok": true, "message": "通过"}
			}
		}
	}

	return vadResult, asrResult, llmResult, ttsResult
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// mapKeys 返回 map 的键列表，用于 debug 日志
func mapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
