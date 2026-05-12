package models

import (
	"time"
)

// === IT English Learning Models ===

// Scenario IT場景（standup, code review, incident update 等）
type LearningScenario struct {
	ID              uint   `json:"id" gorm:"primarykey"`
	Slug            string `json:"slug" gorm:"type:varchar(100);uniqueIndex;not null"`
	TitleEn         string `json:"title_en" gorm:"type:varchar(200);not null"`
	TitleZh         string `json:"title_zh" gorm:"type:varchar(200);not null"`
	DescriptionEn   string `json:"description_en" gorm:"type:text"`
	DescriptionZh   string `json:"description_zh" gorm:"type:text"`
	Difficulty      string `json:"difficulty" gorm:"type:varchar(20);not null;default:'beginner'"` // beginner, intermediate, advanced
	Category        string `json:"category" gorm:"type:varchar(50);not null"`                     // meetings, development, operations, communication
	PromptTemplate  string `json:"prompt_template" gorm:"type:text;not null"`                     // LLM prompt template for this scenario
	IsActive        bool   `json:"is_active" gorm:"default:true;index"`
	SortOrder       int    `json:"sort_order" gorm:"default:0"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// WritingSubmission 用戶寫作提交
type WritingSubmission struct {
	ID          uint      `json:"id" gorm:"primarykey"`
	UserID      uint      `json:"user_id" gorm:"not null;index"`
	ScenarioID  uint      `json:"scenario_id" gorm:"not null;index"`
	Text        string    `json:"text" gorm:"type:text;not null"`
	SubmittedAt time.Time `json:"submitted_at" gorm:"autoCreateTime"`
}

// WritingFeedback LLM 反饋結果
type WritingFeedback struct {
	ID              uint   `json:"id" gorm:"primarykey"`
	SubmissionID    uint   `json:"submission_id" gorm:"uniqueIndex;not null"`
	OverallScore    int    `json:"overall_score" gorm:"not null"`    // 0-100
	GrammarScore    int    `json:"grammar_score" gorm:"not null"`    // 0-100
	VocabularyScore int    `json:"vocabulary_score" gorm:"not null"` // 0-100
	ClarityScore    int    `json:"clarity_score" gorm:"not null"`    // 0-100
	Corrections     string `json:"corrections" gorm:"type:text"`     // JSON array of corrections
	SummaryZh       string `json:"summary_zh" gorm:"type:text"`
	SummaryEn       string `json:"summary_en" gorm:"type:text"`
	XPAwarded       int    `json:"xp_awarded" gorm:"not null;default:0"`
	CreatedAt       time.Time `json:"created_at"`
}

// SpeakingSubmission 用戶口語提交
type SpeakingSubmission struct {
	ID              uint   `json:"id" gorm:"primarykey"`
	UserID          uint   `json:"user_id" gorm:"not null;index"`
	ScenarioID      uint   `json:"scenario_id" gorm:"not null;index"`
	TranscriptDraft string `json:"transcript_draft" gorm:"type:text"` // ASR 結果
	AudioPath       string `json:"audio_path" gorm:"type:varchar(500)"`
	SubmittedAt     time.Time `json:"submitted_at" gorm:"autoCreateTime"`
}

// SpeakingFeedback LLM 口語反饋
type SpeakingFeedback struct {
	ID               uint   `json:"id" gorm:"primarykey"`
	SubmissionID     uint   `json:"submission_id" gorm:"uniqueIndex;not null"`
	TranscriptFinal  string `json:"transcript_final" gorm:"type:text"`
	GrammarScore     int    `json:"grammar_score" gorm:"not null"`
	VocabularyScore  int    `json:"vocabulary_score" gorm:"not null"`
	FluencyScore     int    `json:"fluency_score" gorm:"not null"`
	PronunciationScore int  `json:"pronunciation_score" gorm:"not null"`
	Corrections      string `json:"corrections" gorm:"type:text"` // JSON array
	SummaryZh        string `json:"summary_zh" gorm:"type:text"`
	SummaryEn        string `json:"summary_en" gorm:"type:text"`
	TTSAudioPath     string `json:"tts_audio_path" gorm:"type:varchar(500)"`
	XPAwarded        int    `json:"xp_awarded" gorm:"not null;default:0"`
	CreatedAt        time.Time `json:"created_at"`
}

// SavedPhrase 用戶收藏短語
type SavedPhrase struct {
	ID           uint   `json:"id" gorm:"primarykey"`
	UserID       uint   `json:"user_id" gorm:"not null;index"`
	Text         string `json:"text" gorm:"type:text;not null"`
	TranslationZh string `json:"translation_zh" gorm:"type:text"`
	SourceID     uint   `json:"source_id" gorm:"not null"`
	SourceType   string `json:"source_type" gorm:"type:varchar(20);not null"` // writing, speaking
	Note         string `json:"note" gorm:"type:text"`
	CreatedAt    time.Time `json:"created_at"`
}

// LearnerProfile 學習者進度（每用戶一筆）
type LearnerProfile struct {
	ID                 uint `json:"id" gorm:"primarykey"`
	UserID             uint `json:"user_id" gorm:"uniqueIndex;not null"`
	TotalXP            int  `json:"total_xp" gorm:"default:0"`
	Level              int  `json:"level" gorm:"default:1"`
	Streak             int  `json:"streak" gorm:"default:0"`
	WritingExercises   int  `json:"writing_exercises" gorm:"default:0"`
	SpeakingExercises  int  `json:"speaking_exercises" gorm:"default:0"`
	SavedPhrases       int  `json:"saved_phrases" gorm:"default:0"`
	LastPracticeDate   *time.Time `json:"last_practice_date"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
