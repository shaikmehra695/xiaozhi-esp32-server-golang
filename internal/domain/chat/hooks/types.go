package hooks

import (
	"context"

	"github.com/cloudwego/eino/schema"
	"xiaozhi-esp32-server-golang/internal/domain/speaker"
)

type Context struct {
	Ctx       context.Context
	SessionID string
	DeviceID  string
}

const (
	EventChatASROutput      = "chat.asr.output"
	EventChatLLMInput       = "chat.llm.input"
	EventChatLLMOutput      = "chat.llm.output"
	EventChatTTSInput       = "chat.tts.input"
	EventChatTTSOutputStart = "chat.tts.output.start"
	EventChatTTSOutputStop  = "chat.tts.output.stop"
	EventChatMetric         = "chat.metric"
)

type ASROutputData struct {
	Text          string
	SpeakerResult *speaker.IdentifyResult
}

type LLMInputData struct {
	UserMessage     *schema.Message
	RequestMessages []*schema.Message
	Tools           []*schema.ToolInfo
}

type LLMOutputData struct {
	FullText string
	Err      error
}

type TTSInputData struct {
	Text    string
	IsStart bool
	IsEnd   bool
}

type TTSOutputStartData struct{}

type TTSOutputStopData struct {
	Err error
}

type MetricStage string

const (
	MetricTurnStart     MetricStage = "turn_start"
	MetricAsrFirstText  MetricStage = "asr_first_text"
	MetricAsrFinalText  MetricStage = "asr_final_text"
	MetricLlmStart      MetricStage = "llm_start"
	MetricLlmFirstToken MetricStage = "llm_first_token"
	MetricLlmEnd        MetricStage = "llm_end"
	MetricTtsStart      MetricStage = "tts_start"
	MetricTtsFirstFrame MetricStage = "tts_first_frame"
	MetricTtsStop       MetricStage = "tts_stop"
)

type MetricData struct {
	Stage MetricStage
	Ts    int64
	Err   error
}
