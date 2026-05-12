package services

import (
	"encoding/json"
	"fmt"
	"strings"
)

// FeedbackService generates structured feedback using LLM
type FeedbackService struct {
	llm *LLMService
}

// NewFeedbackService creates a new feedback service
func NewFeedbackService(llm *LLMService) *FeedbackService {
	return &FeedbackService{llm: llm}
}

// WritingFeedbackResult is the structured feedback for a writing submission
type WritingFeedbackResult struct {
	GrammarScore     int                `json:"grammar_score"`
	VocabularyScore  int                `json:"vocabulary_score"`
	StructureScore   int                `json:"structure_score"`
	Professionalism int                `json:"professionalism_score"`
	XPAwarded        int                `json:"xp_awarded"`
	SummaryZH        string             `json:"summary_zh"`
	SummaryEN        string             `json:"summary_en"`
	Corrections      []CorrectionResult `json:"corrections"`
	SuggestedRewrite string             `json:"suggested_rewrite"`
}

// SpeakingFeedbackResult is the structured feedback for a speaking submission
type SpeakingFeedbackResult struct {
	GrammarScore      int                `json:"grammar_score"`
	VocabularyScore   int                `json:"vocabulary_score"`
	FluencyScore      int                `json:"fluency_score"`
	PronunciationScore int               `json:"pronunciation_score"`
	XPAwarded         int                `json:"xp_awarded"`
	SummaryZH         string             `json:"summary_zh"`
	SummaryEN         string             `json:"summary_en"`
	Corrections       []CorrectionResult `json:"corrections"`
	SuggestedResponse string             `json:"suggested_response"`
}

// CorrectionResult is a single correction
type CorrectionResult struct {
	Original      string `json:"original"`
	Corrected     string `json:"corrected"`
	ExplanationZH string `json:"explanation_zh"`
	ExplanationEN string `json:"explanation_en"`
}

const writingSystemPrompt = `You are an IT English writing tutor for Chinese-speaking IT professionals.
Your job is to evaluate their English writing in IT work scenarios (standup updates, code reviews, incident reports, etc.).

You MUST respond with valid JSON in this exact format (no markdown, no code blocks):
{
  "grammar_score": 0-100,
  "vocabulary_score": 0-100,
  "structure_score": 0-100,
  "professionalism_score": 0-100,
  "xp_awarded": 5-25,
  "summary_zh": "用繁體中文寫嘅簡短評語",
  "summary_en": "Brief English summary",
  "corrections": [
    {
      "original": "the wrong text",
      "corrected": "the correct text",
      "explanation_zh": "繁體中文解釋點解錯",
      "explanation_en": "English explanation"
    }
  ],
  "suggested_rewrite": "A polished professional version of the full text"
}

Be strict but encouraging. Focus on:
- Grammar accuracy (tenses, articles, prepositions)
- IT vocabulary usage (correct technical terms)
- Structure (clear, logical flow)
- Professionalism (appropriate tone for work communication)

Award XP: 5-10 for effort, 11-18 for good work, 19-25 for excellent.`

const speakingSystemPrompt = `You are an IT English speaking tutor for Chinese-speaking IT professionals.
Your job is to evaluate their spoken English (transcribed) in IT work scenarios.

You MUST respond with valid JSON in this exact format (no markdown, no code blocks):
{
  "grammar_score": 0-100,
  "vocabulary_score": 0-100,
  "fluency_score": 0-100,
  "pronunciation_score": 0-100,
  "xp_awarded": 5-25,
  "summary_zh": "用繁體中文寫嘅簡短評語",
  "summary_en": "Brief English summary",
  "corrections": [
    {
      "original": "the wrong text",
      "corrected": "the correct text",
      "explanation_zh": "繁體中文解釋點解錯",
      "explanation_en": "English explanation"
    }
  ],
  "suggested_response": "A model answer they could practice"
}

Focus on:
- Grammar accuracy
- IT vocabulary
- Fluency (natural flow, not word-by-word translation)
- Note: pronunciation score is estimated from transcript quality

Award XP: 5-10 for effort, 11-18 for good work, 19-25 for excellent.`

// GenerateWritingFeedback generates feedback for a writing submission
func (fs *FeedbackService) GenerateWritingFeedback(scenarioTitle, userText string) (*WritingFeedbackResult, error) {
	prompt := fmt.Sprintf("Scenario: %s\n\nUser's writing:\n%s", scenarioTitle, userText)

	raw, err := fs.llm.GenerateFeedback(writingSystemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM feedback failed: %w", err)
	}

	var result WritingFeedbackResult
	if err := parseFeedbackJSON(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse feedback: %w (raw: %s)", err, truncate(raw, 200))
	}

	// Clamp scores
	result.GrammarScore = clampScore(result.GrammarScore)
	result.VocabularyScore = clampScore(result.VocabularyScore)
	result.StructureScore = clampScore(result.StructureScore)
	result.Professionalism = clampScore(result.Professionalism)
	result.XPAwarded = clampXP(result.XPAwarded)

	return &result, nil
}

// GenerateSpeakingFeedback generates feedback for a speaking submission
func (fs *FeedbackService) GenerateSpeakingFeedback(scenarioTitle, transcript string) (*SpeakingFeedbackResult, error) {
	prompt := fmt.Sprintf("Scenario: %s\n\nTranscript of user's speech:\n%s", scenarioTitle, transcript)

	raw, err := fs.llm.GenerateFeedback(speakingSystemPrompt, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM feedback failed: %w", err)
	}

	var result SpeakingFeedbackResult
	if err := parseFeedbackJSON(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse feedback: %w (raw: %s)", err, truncate(raw, 200))
	}

	// Clamp scores
	result.GrammarScore = clampScore(result.GrammarScore)
	result.VocabularyScore = clampScore(result.VocabularyScore)
	result.FluencyScore = clampScore(result.FluencyScore)
	result.PronunciationScore = clampScore(result.PronunciationScore)
	result.XPAwarded = clampXP(result.XPAwarded)

	return &result, nil
}

// parseFeedbackJSON extracts JSON from LLM response (handles markdown code blocks)
func parseFeedbackJSON(raw string, v interface{}) error {
	// Strip markdown code blocks if present
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		start, end := 0, len(lines)
		for i, line := range lines {
			if strings.HasPrefix(line, "```") && i == 0 {
				start = 1
			}
			if strings.HasPrefix(line, "```") && i > 0 {
				end = i
			}
		}
		cleaned = strings.Join(lines[start:end], "\n")
	}

	return json.Unmarshal([]byte(cleaned), v)
}

func clampScore(s int) int {
	if s < 0 {
		return 0
	}
	if s > 100 {
		return 100
	}
	return s
}

func clampXP(x int) int {
	if x < 5 {
		return 5
	}
	if x > 25 {
		return 25
	}
	return x
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
