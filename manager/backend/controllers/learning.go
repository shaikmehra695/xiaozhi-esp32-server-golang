package controllers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"xiaozhi/manager/backend/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type LearningController struct {
	DB *gorm.DB
}

// === Scenarios ===

// ListScenarios GET /api/learning/scenarios
func (lc *LearningController) ListScenarios(c *gin.Context) {
	var scenarios []models.LearningScenario
	query := lc.DB.Where("is_active = ?", true)

	// Optional filters
	if category := c.Query("category"); category != "" {
		query = query.Where("category = ?", category)
	}
	if difficulty := c.Query("difficulty"); difficulty != "" {
		query = query.Where("difficulty = ?", difficulty)
	}

	if err := query.Order("sort_order asc, id asc").Find(&scenarios).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch scenarios"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": scenarios})
}

// GetScenario GET /api/learning/scenarios/:slug
func (lc *LearningController) GetScenario(c *gin.Context) {
	slug := c.Param("slug")
	var scenario models.LearningScenario
	if err := lc.DB.Where("slug = ? AND is_active = ?", slug, true).First(&scenario).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Scenario not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": scenario})
}

// === Writing ===

// SubmitWriting POST /api/learning/writing/submit
func (lc *LearningController) SubmitWriting(c *gin.Context) {
	userID := c.GetUint("userID")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	var req struct {
		ScenarioID uint   `json:"scenario_id" binding:"required"`
		Text       string `json:"text" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scenario_id and text are required"})
		return
	}

	// Validate scenario exists
	var scenario models.LearningScenario
	if err := lc.DB.First(&scenario, req.ScenarioID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scenario_id"})
		return
	}

	// Create submission
	submission := models.WritingSubmission{
		UserID:     userID,
		ScenarioID: req.ScenarioID,
		Text:       req.Text,
	}
	if err := lc.DB.Create(&submission).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save submission"})
		return
	}

	// TODO: Call LLM for feedback (Phase 2+)
	// For now, generate mock feedback
	feedback := generateMockWritingFeedback(submission.ID, req.Text)
	lc.DB.Create(&feedback)

	// Update learner profile
	updateLearnerXP(lc.DB, userID, feedback.XPAwarded, "writing")

	c.JSON(http.StatusOK, gin.H{
		"submission_id": submission.ID,
		"feedback":      feedback,
	})
}

// GetWritingHistory GET /api/learning/writing/history
func (lc *LearningController) GetWritingHistory(c *gin.Context) {
	userID := c.GetUint("userID")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 50 { pageSize = 10 }

	var submissions []models.WritingSubmission
	var total int64

	lc.DB.Model(&models.WritingSubmission{}).Where("user_id = ?", userID).Count(&total)
	lc.DB.Where("user_id = ?", userID).
		Order("submitted_at desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&submissions)

	c.JSON(http.StatusOK, gin.H{
		"data":      submissions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetWritingFeedback GET /api/learning/writing/:id/feedback
func (lc *LearningController) GetWritingFeedback(c *gin.Context) {
	submissionID := c.Param("id")
	var feedback models.WritingFeedback
	if err := lc.DB.Where("submission_id = ?", submissionID).First(&feedback).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Feedback not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": feedback})
}

// === Speaking ===

// SubmitSpeaking POST /api/learning/speaking/submit
func (lc *LearningController) SubmitSpeaking(c *gin.Context) {
	userID := c.GetUint("userID")
	if userID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	// Handle multipart form with audio file
	contentType := c.ContentType()

	var scenarioID uint
	var transcriptDraft string
	var audioPath string

	if contentType == "multipart/form-data" {
		// Audio upload mode
		sid := c.PostForm("scenario_id")
		parsedID, err := strconv.ParseUint(sid, 10, 32)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid scenario_id"})
			return
		}
		scenarioID = uint(parsedID)
		transcriptDraft = c.PostForm("transcript_draft")

		file, err := c.FormFile("audio")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Audio file is required"})
			return
		}

		// Save audio file
		filename := fmt.Sprintf("speaking_%d_%d%s", userID, time.Now().UnixNano(), filepath.Ext(file.Filename))
		savePath := filepath.Join("uploads", "audio", filename)
		os.MkdirAll(filepath.Dir(savePath), 0755)
		if err := c.SaveUploadedFile(file, savePath); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save audio"})
			return
		}
		audioPath = savePath
	} else {
		// JSON mode (no audio, transcript only)
		var req struct {
			ScenarioID      uint   `json:"scenario_id" binding:"required"`
			TranscriptDraft string `json:"transcript_draft"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "scenario_id is required"})
			return
		}
		scenarioID = req.ScenarioID
		transcriptDraft = req.TranscriptDraft
	}

	submission := models.SpeakingSubmission{
		UserID:          userID,
		ScenarioID:      scenarioID,
		TranscriptDraft: transcriptDraft,
		AudioPath:       audioPath,
	}
	if err := lc.DB.Create(&submission).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save submission"})
		return
	}

	// Mock feedback for now (Phase 4: real LLM + TTS)
	feedback := generateMockSpeakingFeedback(submission.ID, transcriptDraft)
	lc.DB.Create(&feedback)

	updateLearnerXP(lc.DB, userID, feedback.XPAwarded, "speaking")

	c.JSON(http.StatusOK, gin.H{
		"submission_id": submission.ID,
		"feedback":      feedback,
	})
}

// GetSpeakingHistory GET /api/learning/speaking/history
func (lc *LearningController) GetSpeakingHistory(c *gin.Context) {
	userID := c.GetUint("userID")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "10"))
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 50 { pageSize = 10 }

	var submissions []models.SpeakingSubmission
	var total int64

	lc.DB.Model(&models.SpeakingSubmission{}).Where("user_id = ?", userID).Count(&total)
	lc.DB.Where("user_id = ?", userID).
		Order("submitted_at desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&submissions)

	c.JSON(http.StatusOK, gin.H{
		"data":      submissions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// === Phrases ===

// SavePhrase POST /api/learning/phrases
func (lc *LearningController) SavePhrase(c *gin.Context) {
	userID := c.GetUint("userID")
	var req struct {
		Text          string `json:"text" binding:"required"`
		TranslationZh string `json:"translation_zh"`
		SourceID      uint   `json:"source_id" binding:"required"`
		SourceType    string `json:"source_type" binding:"required"`
		Note          string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text, source_id, source_type are required"})
		return
	}

	phrase := models.SavedPhrase{
		UserID:        userID,
		Text:          req.Text,
		TranslationZh: req.TranslationZh,
		SourceID:      req.SourceID,
		SourceType:    req.SourceType,
		Note:          req.Note,
	}
	if err := lc.DB.Create(&phrase).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save phrase"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": phrase})
}

// ListPhrases GET /api/learning/phrases
func (lc *LearningController) ListPhrases(c *gin.Context) {
	userID := c.GetUint("userID")
	var phrases []models.SavedPhrase
	lc.DB.Where("user_id = ?", userID).Order("created_at desc").Find(&phrases)
	c.JSON(http.StatusOK, gin.H{"data": phrases})
}

// DeletePhrase DELETE /api/learning/phrases/:id
func (lc *LearningController) DeletePhrase(c *gin.Context) {
	userID := c.GetUint("userID")
	phraseID := c.Param("id")
	result := lc.DB.Where("id = ? AND user_id = ?", phraseID, userID).Delete(&models.SavedPhrase{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Phrase not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Deleted"})
}

// === Progress ===

// GetProgress GET /api/learning/progress
func (lc *LearningController) GetProgress(c *gin.Context) {
	userID := c.GetUint("userID")
	var profile models.LearnerProfile
	if err := lc.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		// Create default profile
		profile = models.LearnerProfile{UserID: userID}
		lc.DB.Create(&profile)
	}
	c.JSON(http.StatusOK, gin.H{"data": profile})
}

// === Helpers ===

func generateMockWritingFeedback(submissionID uint, text string) models.WritingFeedback {
	wordCount := len(text)
	baseScore := int(math.Min(float64(wordCount/2+50), 90))

	corrections := []map[string]string{
		{
			"original":       "I am work on the project",
			"corrected":      "I am working on the project",
			"explanation_en": "Use present continuous (am + verb-ing) for ongoing actions.",
			"explanation_zh": "用 present continuous（am + verb-ing）表示正在進行嘅動作。",
		},
	}

	corrJSON, _ := json.Marshal(corrections)

	return models.WritingFeedback{
		SubmissionID:    submissionID,
		OverallScore:    baseScore,
		GrammarScore:    baseScore - 5,
		VocabularyScore: baseScore + 3,
		ClarityScore:    baseScore - 2,
		Corrections:     string(corrJSON),
		SummaryZh:       fmt.Sprintf("整體表現唔錯，有 %d 個需要改善嘅地方。繼續加油！", len(corrections)),
		SummaryEn:       fmt.Sprintf("Good effort! There are %d areas to improve. Keep going!", len(corrections)),
		XPAwarded:       baseScore / 10,
	}
}

func generateMockSpeakingFeedback(submissionID uint, transcript string) models.SpeakingFeedback {
	wordCount := len(transcript)
	baseScore := int(math.Min(float64(wordCount/2+45), 85))

	corrections := []map[string]string{
		{
			"original":       "the deploy was success",
			"corrected":      "the deployment was successful",
			"explanation_en": "Use 'deployment' (noun) and 'successful' (adjective).",
			"explanation_zh": "用 'deployment'（名詞）同 'successful'（形容詞）。",
		},
	}

	corrJSON, _ := json.Marshal(corrections)

	return models.SpeakingFeedback{
		SubmissionID:       submissionID,
		TranscriptFinal:    transcript,
		GrammarScore:       baseScore - 3,
		VocabularyScore:    baseScore + 2,
		FluencyScore:       baseScore - 5,
		PronunciationScore: baseScore - 8,
		Corrections:        string(corrJSON),
		SummaryZh:          "口語表達整體流暢，發音有少少需要改善。",
		SummaryEn:          "Overall fluent expression. Minor pronunciation improvements needed.",
		XPAwarded:          baseScore / 10,
	}
}

func updateLearnerXP(db *gorm.DB, userID uint, xp int, exerciseType string) {
	var profile models.LearnerProfile
	if err := db.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		profile = models.LearnerProfile{UserID: userID}
		db.Create(&profile)
	}

	now := time.Now()
	profile.TotalXP += xp
	profile.Level = profile.TotalXP/100 + 1

	// Update streak
	if profile.LastPracticeDate != nil {
		lastDate := profile.LastPracticeDate.Format("2006-01-02")
		today := now.Format("2006-01-02")
		yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")
		if lastDate == today {
			// Same day, no streak change
		} else if lastDate == yesterday {
			profile.Streak++
		} else {
			profile.Streak = 1
		}
	} else {
		profile.Streak = 1
	}

	switch exerciseType {
	case "writing":
		profile.WritingExercises++
	case "speaking":
		profile.SpeakingExercises++
	}

	profile.LastPracticeDate = &now
	db.Save(&profile)
}
