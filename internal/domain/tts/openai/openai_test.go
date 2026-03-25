package openai

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestOpenAITTSOnline(t *testing.T) {
	if os.Getenv("RUN_ONLINE_TESTS") != "1" {
		t.Skip("skip online TTS tests: set RUN_ONLINE_TESTS=1")
	}

	apiKey := os.Getenv("TTS_OPENAI_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		t.Skip("skip OpenAI TTS online test: missing TTS_OPENAI_API_KEY or OPENAI_API_KEY")
	}

	provider := NewOpenAITTSProvider(map[string]interface{}{
		"api_key":         apiKey,
		"model":           "tts-1",
		"voice":           getenvOrDefault("TTS_OPENAI_VOICE", "alloy"),
		"response_format": "mp3",
		"speed":           1.0,
	})

	t.Run("text_to_speech", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		frames, err := provider.TextToSpeech(ctx, "online openai tts test", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeech failed: %v", err)
		}
		if len(frames) == 0 {
			t.Fatalf("TextToSpeech returned empty frames")
		}
	})

	t.Run("text_to_speech_stream", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		stream, err := provider.TextToSpeechStream(ctx, "online openai stream tts test", 16000, 1, 60)
		if err != nil {
			t.Fatalf("TextToSpeechStream failed: %v", err)
		}

		select {
		case frame, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed without data")
			}
			if len(frame) == 0 {
				t.Fatalf("received empty frame")
			}
		case <-ctx.Done():
			t.Fatalf("stream timeout: %v", ctx.Err())
		}
	})
}

func getenvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
