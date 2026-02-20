package controllers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"xiaozhi/manager/backend/models"

	"gorm.io/gorm"
)

const (
	knowledgeSyncStatusPending      = "pending"
	knowledgeSyncStatusUploading    = "uploading"
	knowledgeSyncStatusUploaded     = "uploaded"
	knowledgeSyncStatusParsing      = "parsing"
	knowledgeSyncStatusSynced       = "synced"
	knowledgeSyncStatusUploadFailed = "upload_failed"
	knowledgeSyncStatusParseFailed  = "parse_failed"
	knowledgeSyncStatusFailed       = "failed"
	difyHTTPTimeout                 = 15 * time.Second
	difyFileUploadHTTPTimeout       = 90 * time.Second
	difyFileUploadMaxAttempts       = 3
	difyFileUploadRetryStep         = 2 * time.Second
)

type knowledgeProviderSyncResult struct {
	DatasetID    string
	DocumentID   string
	AutoDataset  bool
	SyncProvider string
	LastSyncedAt *time.Time
}

type difyKnowledgeSyncConfig struct {
	BaseURL                  string
	APIKey                   string
	DatasetPermission        string
	DatasetProvider          string
	DatasetIndexingTechnique string
}

type ragflowKnowledgeSyncConfig struct {
	BaseURL            string
	APIKey             string
	DatasetPermission  string
	DatasetChunkMethod string
}

const defaultKnowledgeSyncProvider = "dify"

func loadDefaultKnowledgeProviderConfig(db *gorm.DB) (*models.Config, map[string]interface{}, error) {
	var cfg models.Config
	err := db.Where("type = ? AND enabled = ?", "knowledge_search", true).Order("is_default DESC, id DESC").First(&cfg).Error
	if err != nil {
		return nil, nil, err
	}

	return parseKnowledgeProviderConfigPayload(&cfg)
}

func loadKnowledgeProviderConfigByProvider(db *gorm.DB, provider string) (*models.Config, map[string]interface{}, error) {
	p := strings.ToLower(strings.TrimSpace(provider))
	if p == "" {
		return loadDefaultKnowledgeProviderConfig(db)
	}

	var cfg models.Config
	err := db.Where("type = ? AND enabled = ? AND LOWER(provider) = ?", "knowledge_search", true, p).Order("is_default DESC, id DESC").First(&cfg).Error
	if err != nil {
		return nil, nil, err
	}
	return parseKnowledgeProviderConfigPayload(&cfg)
}

func parseKnowledgeProviderConfigPayload(cfg *models.Config) (*models.Config, map[string]interface{}, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("知识库provider配置为空")
	}

	providerData := map[string]interface{}{}
	if strings.TrimSpace(cfg.JsonData) != "" {
		if err := json.Unmarshal([]byte(cfg.JsonData), &providerData); err != nil {
			return nil, nil, fmt.Errorf("解析knowledge_search配置失败: %w", err)
		}
	}
	return cfg, providerData, nil
}

func resolveDefaultKnowledgeProviderName(db *gorm.DB) string {
	cfg, _, err := loadDefaultKnowledgeProviderConfig(db)
	if err != nil {
		return defaultKnowledgeSyncProvider
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return defaultKnowledgeSyncProvider
	}
	return provider
}

func resolveKnowledgeProviderForKB(db *gorm.DB, kb *models.KnowledgeBase) (string, *models.Config, map[string]interface{}, error) {
	if kb == nil {
		return "", nil, nil, fmt.Errorf("知识库数据为空")
	}
	provider := strings.ToLower(strings.TrimSpace(kb.SyncProvider))

	cfg, providerData, err := loadKnowledgeProviderConfigByProvider(db, provider)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			if provider == "" {
				return "", nil, nil, fmt.Errorf("未找到已启用的知识库provider配置")
			}
			return "", nil, nil, fmt.Errorf("未找到已启用的知识库provider配置: %s", provider)
		}
		return "", nil, nil, fmt.Errorf("获取知识库provider配置失败: %w", err)
	}

	resolvedProvider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if resolvedProvider == "" {
		if provider != "" {
			resolvedProvider = provider
		} else {
			resolvedProvider = defaultKnowledgeSyncProvider
		}
	}
	return resolvedProvider, cfg, providerData, nil
}

func syncKnowledgeBaseBestEffort(db *gorm.DB, kb *models.KnowledgeBase) error {
	result, syncErr := syncKnowledgeBaseWithProvider(db, kb)
	persistErr := persistKnowledgeSyncState(db, kb, result, syncErr)
	if persistErr != nil {
		if syncErr != nil {
			return fmt.Errorf("%v; 保存同步状态失败: %w", syncErr, persistErr)
		}
		return fmt.Errorf("保存同步状态失败: %w", persistErr)
	}
	return syncErr
}

func syncKnowledgeBaseDeleteBestEffort(db *gorm.DB, kb *models.KnowledgeBase) error {
	provider, _, providerData, err := resolveKnowledgeProviderForKB(db, kb)
	if err != nil {
		return err
	}

	switch provider {
	case "dify":
		difyCfg, err := parseDifyKnowledgeSyncConfig(providerData)
		if err != nil {
			return err
		}
		return deleteKnowledgeBaseFromDify(difyCfg, kb)
	case "ragflow":
		ragflowCfg, err := parseRagflowKnowledgeSyncConfig(providerData)
		if err != nil {
			return err
		}
		return deleteKnowledgeBaseFromRagflow(ragflowCfg, kb)
	default:
		return fmt.Errorf("知识库删除同步暂不支持provider: %s", provider)
	}
}

func syncKnowledgeBaseWithProvider(db *gorm.DB, kb *models.KnowledgeBase) (*knowledgeProviderSyncResult, error) {
	provider, _, providerData, err := resolveKnowledgeProviderForKB(db, kb)
	if err != nil {
		return nil, err
	}

	switch provider {
	case "dify":
		difyCfg, err := parseDifyKnowledgeSyncConfig(providerData)
		if err != nil {
			return nil, err
		}
		return syncKnowledgeBaseToDify(difyCfg, kb)
	case "ragflow":
		ragflowCfg, err := parseRagflowKnowledgeSyncConfig(providerData)
		if err != nil {
			return nil, err
		}
		return syncKnowledgeBaseToRagflow(ragflowCfg, kb)
	default:
		return nil, fmt.Errorf("知识库同步暂不支持provider: %s", provider)
	}
}

func persistKnowledgeSyncState(db *gorm.DB, kb *models.KnowledgeBase, result *knowledgeProviderSyncResult, syncErr error) error {
	if kb == nil || kb.ID == 0 {
		return fmt.Errorf("知识库实体无效")
	}

	updates := map[string]interface{}{}
	if result != nil {
		if strings.TrimSpace(result.DatasetID) != "" {
			updates["external_kb_id"] = strings.TrimSpace(result.DatasetID)
		}
		if strings.TrimSpace(result.DocumentID) != "" {
			updates["external_doc_id"] = strings.TrimSpace(result.DocumentID)
		}
		updates["auto_dataset"] = result.AutoDataset
		if strings.TrimSpace(result.SyncProvider) != "" {
			updates["sync_provider"] = strings.TrimSpace(result.SyncProvider)
		}
		if result.LastSyncedAt != nil {
			updates["last_synced_at"] = result.LastSyncedAt
		}
	}

	if syncErr != nil {
		updates["sync_status"] = knowledgeSyncStatusFailed
		updates["sync_error"] = truncateSyncError(syncErr.Error())
	} else {
		updates["sync_status"] = knowledgeSyncStatusSynced
		updates["sync_error"] = ""
		if _, ok := updates["last_synced_at"]; !ok {
			now := time.Now()
			updates["last_synced_at"] = &now
		}
	}

	if err := db.Model(&models.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(updates).Error; err != nil {
		return err
	}
	return db.Where("id = ?", kb.ID).First(kb).Error
}

func truncateSyncError(errMsg string) string {
	msg := strings.TrimSpace(errMsg)
	if len(msg) <= 800 {
		return msg
	}
	return msg[:800] + "..."
}

func parseDifyKnowledgeSyncConfig(providerData map[string]interface{}) (*difyKnowledgeSyncConfig, error) {
	baseURL, _ := providerData["base_url"].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("dify base_url 不能为空")
	}

	apiKey, _ := providerData["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("dify api_key 不能为空")
	}

	cfg := &difyKnowledgeSyncConfig{
		BaseURL:                  baseURL,
		APIKey:                   apiKey,
		DatasetPermission:        "only_me",
		DatasetProvider:          "vendor",
		DatasetIndexingTechnique: "high_quality",
	}
	if v, ok := providerData["dataset_permission"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.DatasetPermission = strings.TrimSpace(v)
	}
	if v, ok := providerData["dataset_provider"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.DatasetProvider = strings.TrimSpace(v)
	}
	if v, ok := providerData["dataset_indexing_technique"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.DatasetIndexingTechnique = strings.TrimSpace(v)
	}
	return cfg, nil
}

func parseRagflowKnowledgeSyncConfig(providerData map[string]interface{}) (*ragflowKnowledgeSyncConfig, error) {
	baseURL, _ := providerData["base_url"].(string)
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("ragflow base_url 不能为空")
	}

	apiKey, _ := providerData["api_key"].(string)
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("ragflow api_key 不能为空")
	}

	cfg := &ragflowKnowledgeSyncConfig{
		BaseURL:            baseURL,
		APIKey:             apiKey,
		DatasetPermission:  "me",
		DatasetChunkMethod: "naive",
	}
	if v, ok := providerData["dataset_permission"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.DatasetPermission = strings.TrimSpace(v)
	}
	if v, ok := providerData["dataset_chunk_method"].(string); ok && strings.TrimSpace(v) != "" {
		cfg.DatasetChunkMethod = strings.TrimSpace(v)
	}
	return cfg, nil
}

func syncKnowledgeBaseToDify(cfg *difyKnowledgeSyncConfig, kb *models.KnowledgeBase) (*knowledgeProviderSyncResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("知识库数据为空")
	}
	content := strings.TrimSpace(kb.Content)
	result := &knowledgeProviderSyncResult{
		DatasetID:    strings.TrimSpace(kb.ExternalKBID),
		DocumentID:   strings.TrimSpace(kb.ExternalDocID),
		AutoDataset:  kb.AutoDataset,
		SyncProvider: "dify",
	}
	client := &http.Client{Timeout: difyHTTPTimeout}

	if result.DatasetID == "" {
		datasetID, err := createDifyDataset(client, cfg, kb)
		if err != nil {
			return result, err
		}
		result.DatasetID = datasetID
		result.AutoDataset = true
	}

	// 允许空知识库同步：仅确保 dataset 存在，不创建/更新文档。
	if content == "" {
		now := time.Now()
		result.LastSyncedAt = &now
		return result, nil
	}

	if result.DocumentID == "" {
		docID, err := createDifyDocumentByText(client, cfg, result.DatasetID, kb)
		if err != nil {
			return result, err
		}
		result.DocumentID = docID
	} else {
		if err := updateDifyDocumentByText(client, cfg, result.DatasetID, result.DocumentID, kb); err != nil {
			return result, err
		}
	}

	now := time.Now()
	result.LastSyncedAt = &now
	return result, nil
}

func deleteKnowledgeBaseFromDify(cfg *difyKnowledgeSyncConfig, kb *models.KnowledgeBase) error {
	if kb == nil {
		return fmt.Errorf("知识库数据为空")
	}
	datasetID := strings.TrimSpace(kb.ExternalKBID)
	documentID := strings.TrimSpace(kb.ExternalDocID)
	if datasetID == "" {
		return nil
	}

	client := &http.Client{Timeout: difyHTTPTimeout}

	if documentID != "" {
		if err := deleteDifyDocument(client, cfg, datasetID, documentID); err != nil {
			return err
		}
	}

	if !kb.AutoDataset {
		return nil
	}

	empty, err := isDifyDatasetEmpty(client, cfg, datasetID)
	if err != nil {
		return err
	}
	if empty {
		if err := deleteDifyDataset(client, cfg, datasetID); err != nil {
			return err
		}
	}
	return nil
}

func syncKnowledgeBaseToRagflow(cfg *ragflowKnowledgeSyncConfig, kb *models.KnowledgeBase) (*knowledgeProviderSyncResult, error) {
	if kb == nil {
		return nil, fmt.Errorf("知识库数据为空")
	}
	content := strings.TrimSpace(kb.Content)
	result := &knowledgeProviderSyncResult{
		DatasetID:    strings.TrimSpace(kb.ExternalKBID),
		DocumentID:   strings.TrimSpace(kb.ExternalDocID),
		AutoDataset:  kb.AutoDataset,
		SyncProvider: "ragflow",
	}
	client := &http.Client{Timeout: 20 * time.Second}

	if result.DatasetID == "" {
		datasetID, err := createRagflowDataset(client, cfg, kb)
		if err != nil {
			return result, err
		}
		result.DatasetID = datasetID
		result.AutoDataset = true
	}

	// 允许空知识库同步：仅确保 dataset 存在，不创建/更新文档。
	if content == "" {
		now := time.Now()
		result.LastSyncedAt = &now
		return result, nil
	}

	if result.DocumentID == "" {
		docID, err := createAndParseRagflowDocumentByText(client, cfg, result.DatasetID, buildAutoDocumentName(kb), kb.Content)
		if err != nil {
			return result, err
		}
		result.DocumentID = docID
	} else {
		newDocID, err := replaceRagflowDocumentByText(client, cfg, result.DatasetID, result.DocumentID, buildAutoDocumentName(kb), kb.Content)
		if err != nil {
			return result, err
		}
		result.DocumentID = newDocID
	}

	now := time.Now()
	result.LastSyncedAt = &now
	return result, nil
}

func deleteKnowledgeBaseFromRagflow(cfg *ragflowKnowledgeSyncConfig, kb *models.KnowledgeBase) error {
	if kb == nil {
		return fmt.Errorf("知识库数据为空")
	}
	datasetID := strings.TrimSpace(kb.ExternalKBID)
	documentID := strings.TrimSpace(kb.ExternalDocID)
	if datasetID == "" {
		return nil
	}

	client := &http.Client{Timeout: 20 * time.Second}

	if documentID != "" {
		if err := deleteRagflowDocument(client, cfg, datasetID, documentID); err != nil {
			return err
		}
	}

	if !kb.AutoDataset {
		return nil
	}

	empty, err := isRagflowDatasetEmpty(client, cfg, datasetID)
	if err != nil {
		return err
	}
	if empty {
		if err := deleteRagflowDataset(client, cfg, datasetID); err != nil {
			return err
		}
	}
	return nil
}

func syncKnowledgeDocumentBestEffort(db *gorm.DB, kbID, docID uint) error {
	if kbID == 0 || docID == 0 {
		return fmt.Errorf("无效的知识库或文档ID")
	}
	var kb models.KnowledgeBase
	if err := db.Where("id = ?", kbID).First(&kb).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return fmt.Errorf("加载知识库失败: %w", err)
	}
	var doc models.KnowledgeBaseDocument
	if err := db.Where("id = ? AND knowledge_base_id = ?", docID, kbID).First(&doc).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return fmt.Errorf("加载知识库文档失败: %w", err)
	}

	persistBestEffort := func(externalDocID, status string, syncErr error) {
		if err := persistKnowledgeDocumentSyncState(db, &doc, externalDocID, status, syncErr); err != nil {
			log.Printf(
				"[KnowledgeSync][Doc] persist status failed kb_id=%d doc_id=%d status=%s external_doc_id=%s err=%v",
				kbID,
				docID,
				status,
				strings.TrimSpace(externalDocID),
				err,
			)
		}
	}
	failUpload := func(externalDocID string, err error) error {
		persistBestEffort(externalDocID, knowledgeSyncStatusUploadFailed, err)
		return err
	}
	failParse := func(externalDocID string, err error) error {
		persistBestEffort(externalDocID, knowledgeSyncStatusParseFailed, err)
		return err
	}
	markProgress := func(externalDocID, status string) {
		persistBestEffort(externalDocID, status, nil)
	}
	syncSuccess := func(externalDocID string) error {
		return persistKnowledgeDocumentSyncState(db, &doc, externalDocID, knowledgeSyncStatusSynced, nil)
	}

	provider, _, providerData, err := resolveKnowledgeProviderForKB(db, &kb)
	if err != nil {
		return failUpload("", err)
	}
	uploadFileName, uploadFileData, isUploadFile, uploadDecodeErr := decodeKnowledgeUploadContent(doc.Content)
	if uploadDecodeErr != nil {
		return failUpload("", uploadDecodeErr)
	}
	content := strings.TrimSpace(doc.Content)
	markProgress(strings.TrimSpace(doc.ExternalDocID), knowledgeSyncStatusUploading)

	switch provider {
	case "dify":
		difyCfg, err := parseDifyKnowledgeSyncConfig(providerData)
		if err != nil {
			return failUpload("", err)
		}

		clientTimeout := difyHTTPTimeout
		if isUploadFile {
			clientTimeout = difyFileUploadHTTPTimeout
		}
		client := &http.Client{Timeout: clientTimeout}
		datasetID, err := ensureDifyDatasetForKnowledgeBase(db, &kb, client, difyCfg)
		if err != nil {
			return failUpload("", err)
		}

		documentID := strings.TrimSpace(doc.ExternalDocID)
		if isUploadFile {
			if documentID == "" {
				documentID, err = createDifyDocumentByFile(client, difyCfg, datasetID, uploadFileName, uploadFileData)
				if err != nil {
					return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
				}
			} else {
				documentID, err = replaceDifyDocumentByFile(client, difyCfg, datasetID, documentID, uploadFileName, uploadFileData)
				if err != nil {
					return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
				}
			}
		} else {
			if content == "" {
				err := fmt.Errorf("文档内容为空，无法同步")
				return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
			}
			if documentID == "" {
				documentID, err = createDifyDocumentByText(client, difyCfg, datasetID, &models.KnowledgeBase{
					ID:      kb.ID,
					Name:    doc.Name,
					Content: doc.Content,
				})
				if err != nil {
					return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
				}
			} else {
				if err := updateDifyDocumentByText(client, difyCfg, datasetID, documentID, &models.KnowledgeBase{
					ID:      kb.ID,
					Name:    doc.Name,
					Content: doc.Content,
				}); err != nil {
					return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
				}
			}
		}
		markProgress(documentID, knowledgeSyncStatusUploaded)
		markProgress(documentID, knowledgeSyncStatusParsing)
		return syncSuccess(documentID)

	case "ragflow":
		if !isUploadFile && content == "" {
			err := fmt.Errorf("文档内容为空，无法同步")
			return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
		}

		ragflowCfg, err := parseRagflowKnowledgeSyncConfig(providerData)
		if err != nil {
			return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
		}

		client := &http.Client{Timeout: 20 * time.Second}
		datasetID, err := ensureRagflowDatasetForKnowledgeBase(db, &kb, client, ragflowCfg)
		if err != nil {
			return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
		}

		oldDocumentID := strings.TrimSpace(doc.ExternalDocID)
		documentID := oldDocumentID
		if isUploadFile {
			documentID, err = uploadRagflowDocumentByBytes(client, ragflowCfg, datasetID, uploadFileName, uploadFileData)
			if err != nil {
				return failUpload(oldDocumentID, err)
			}
		} else {
			fileName := buildRagflowUploadFileNameForText(doc.Name)
			documentID, err = uploadRagflowDocumentByBytes(client, ragflowCfg, datasetID, fileName, []byte(doc.Content))
			if err != nil {
				return failUpload(oldDocumentID, err)
			}
		}
		markProgress(documentID, knowledgeSyncStatusUploaded)
		markProgress(documentID, knowledgeSyncStatusParsing)
		if err := parseRagflowDocuments(client, ragflowCfg, datasetID, []string{documentID}); err != nil {
			return failParse(documentID, err)
		}
		if oldDocumentID != "" && oldDocumentID != documentID {
			if err := deleteRagflowDocument(client, ragflowCfg, datasetID, oldDocumentID); err != nil {
				log.Printf("[KnowledgeSync][Ragflow] delete old document warning dataset_id=%s old_document_id=%s err=%v", datasetID, oldDocumentID, err)
			}
		}
		return syncSuccess(documentID)

	default:
		err := fmt.Errorf("知识库文档同步暂不支持provider: %s", provider)
		return failUpload(strings.TrimSpace(doc.ExternalDocID), err)
	}
}

func syncKnowledgeDocumentDeleteBestEffort(db *gorm.DB, kb models.KnowledgeBase, doc models.KnowledgeBaseDocument) error {
	provider, _, providerData, err := resolveKnowledgeProviderForKB(db, &kb)
	if err != nil {
		return err
	}

	datasetID := strings.TrimSpace(kb.ExternalKBID)
	if datasetID == "" {
		return nil
	}

	switch provider {
	case "dify":
		difyCfg, err := parseDifyKnowledgeSyncConfig(providerData)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: difyHTTPTimeout}
		if docID := strings.TrimSpace(doc.ExternalDocID); docID != "" {
			if err := deleteDifyDocument(client, difyCfg, datasetID, docID); err != nil {
				return err
			}
		}
		if !kb.AutoDataset {
			return nil
		}
		empty, err := isDifyDatasetEmpty(client, difyCfg, datasetID)
		if err != nil {
			return err
		}
		if empty {
			if err := deleteDifyDataset(client, difyCfg, datasetID); err != nil {
				return err
			}
			now := time.Now()
			_ = db.Model(&models.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(map[string]interface{}{
				"external_kb_id": "",
				"auto_dataset":   false,
				"sync_status":    knowledgeSyncStatusSynced,
				"sync_error":     "",
				"last_synced_at": &now,
			}).Error
		}
		return nil

	case "ragflow":
		ragflowCfg, err := parseRagflowKnowledgeSyncConfig(providerData)
		if err != nil {
			return err
		}
		client := &http.Client{Timeout: 20 * time.Second}
		if docID := strings.TrimSpace(doc.ExternalDocID); docID != "" {
			if err := deleteRagflowDocument(client, ragflowCfg, datasetID, docID); err != nil {
				return err
			}
		}
		if !kb.AutoDataset {
			return nil
		}
		empty, err := isRagflowDatasetEmpty(client, ragflowCfg, datasetID)
		if err != nil {
			return err
		}
		if empty {
			if err := deleteRagflowDataset(client, ragflowCfg, datasetID); err != nil {
				return err
			}
			now := time.Now()
			_ = db.Model(&models.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(map[string]interface{}{
				"external_kb_id": "",
				"auto_dataset":   false,
				"sync_status":    knowledgeSyncStatusSynced,
				"sync_error":     "",
				"last_synced_at": &now,
			}).Error
		}
		return nil
	default:
		return fmt.Errorf("知识库文档删除同步暂不支持provider: %s", provider)
	}
}

func ensureDifyDatasetForKnowledgeBase(db *gorm.DB, kb *models.KnowledgeBase, client *http.Client, cfg *difyKnowledgeSyncConfig) (string, error) {
	if kb == nil {
		return "", fmt.Errorf("知识库为空")
	}
	datasetID := strings.TrimSpace(kb.ExternalKBID)
	if datasetID != "" {
		return datasetID, nil
	}
	datasetID, err := createDifyDataset(client, cfg, kb)
	if err != nil {
		return "", err
	}

	now := time.Now()
	updates := map[string]interface{}{
		"external_kb_id": datasetID,
		"auto_dataset":   true,
		"sync_provider":  "dify",
		"sync_status":    knowledgeSyncStatusSynced,
		"sync_error":     "",
		"last_synced_at": &now,
	}
	if err := db.Model(&models.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(updates).Error; err != nil {
		return "", fmt.Errorf("更新知识库dataset_id失败: %w", err)
	}
	kb.ExternalKBID = datasetID
	kb.AutoDataset = true
	kb.SyncProvider = "dify"
	kb.SyncStatus = knowledgeSyncStatusSynced
	kb.SyncError = ""
	kb.LastSyncedAt = &now
	return datasetID, nil
}

func ensureRagflowDatasetForKnowledgeBase(db *gorm.DB, kb *models.KnowledgeBase, client *http.Client, cfg *ragflowKnowledgeSyncConfig) (string, error) {
	if kb == nil {
		return "", fmt.Errorf("知识库为空")
	}
	datasetID := strings.TrimSpace(kb.ExternalKBID)
	if datasetID != "" {
		return datasetID, nil
	}
	datasetID, err := createRagflowDataset(client, cfg, kb)
	if err != nil {
		return "", err
	}

	now := time.Now()
	updates := map[string]interface{}{
		"external_kb_id": datasetID,
		"auto_dataset":   true,
		"sync_provider":  "ragflow",
		"sync_status":    knowledgeSyncStatusSynced,
		"sync_error":     "",
		"last_synced_at": &now,
	}
	if err := db.Model(&models.KnowledgeBase{}).Where("id = ?", kb.ID).Updates(updates).Error; err != nil {
		return "", fmt.Errorf("更新知识库dataset_id失败: %w", err)
	}
	kb.ExternalKBID = datasetID
	kb.AutoDataset = true
	kb.SyncProvider = "ragflow"
	kb.SyncStatus = knowledgeSyncStatusSynced
	kb.SyncError = ""
	kb.LastSyncedAt = &now
	return datasetID, nil
}

func persistKnowledgeDocumentSyncState(db *gorm.DB, doc *models.KnowledgeBaseDocument, externalDocID, syncStatus string, syncErr error) error {
	if doc == nil || doc.ID == 0 {
		return fmt.Errorf("文档实体无效")
	}
	updates := map[string]interface{}{}
	if strings.TrimSpace(externalDocID) != "" {
		updates["external_doc_id"] = strings.TrimSpace(externalDocID)
	}

	status := strings.TrimSpace(syncStatus)
	if status == "" {
		if syncErr != nil {
			status = knowledgeSyncStatusFailed
		} else {
			status = knowledgeSyncStatusSynced
		}
	}
	updates["sync_status"] = status

	if syncErr != nil {
		updates["sync_error"] = truncateSyncError(syncErr.Error())
	} else {
		updates["sync_error"] = ""
		if status == knowledgeSyncStatusSynced {
			now := time.Now()
			updates["last_synced_at"] = &now
		}
	}
	if err := db.Model(&models.KnowledgeBaseDocument{}).Where("id = ?", doc.ID).Updates(updates).Error; err != nil {
		return err
	}
	return db.Where("id = ?", doc.ID).First(doc).Error
}

func buildDifyURL(baseURL, path string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(strings.ToLower(trimmed), "/v1") {
		return trimmed + path
	}
	return trimmed + "/v1" + path
}

func buildRagflowURL(baseURL, path string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(trimmed)
	if strings.HasSuffix(lower, "/api/v1") {
		return trimmed + path
	}
	if strings.HasSuffix(lower, "/api") {
		return trimmed + "/v1" + path
	}
	return trimmed + "/api/v1" + path
}

func buildAutoDatasetName(kb *models.KnowledgeBase) string {
	name := strings.TrimSpace(kb.Name)
	name = strings.ReplaceAll(name, "\n", " ")
	name = strings.ReplaceAll(name, "\r", " ")
	if name == "" {
		name = "knowledge-base"
	}
	ret := fmt.Sprintf("kb-%d-%s", kb.ID, name)
	if len(ret) > 100 {
		return ret[:100]
	}
	return ret
}

func buildAutoDocumentName(kb *models.KnowledgeBase) string {
	name := strings.TrimSpace(kb.Name)
	if name == "" {
		return fmt.Sprintf("kb-%d-doc", kb.ID)
	}
	if len(name) > 100 {
		return name[:100]
	}
	return name
}

func createDifyDataset(client *http.Client, cfg *difyKnowledgeSyncConfig, kb *models.KnowledgeBase) (string, error) {
	payload := map[string]interface{}{
		"name":        buildAutoDatasetName(kb),
		"description": strings.TrimSpace(kb.Description),
	}
	if cfg.DatasetPermission != "" {
		payload["permission"] = cfg.DatasetPermission
	}
	if cfg.DatasetProvider != "" {
		payload["provider"] = cfg.DatasetProvider
	}
	if cfg.DatasetIndexingTechnique != "" {
		payload["indexing_technique"] = cfg.DatasetIndexingTechnique
	}

	var resp struct {
		ID   string `json:"id"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, body, err := doDifyJSONRequest(client, http.MethodPost, buildDifyURL(cfg.BaseURL, "/datasets"), cfg.APIKey, payload, &resp)
	if err != nil {
		return "", fmt.Errorf("创建Dify dataset失败: %w", err)
	}

	datasetID := strings.TrimSpace(resp.ID)
	if datasetID == "" {
		datasetID = strings.TrimSpace(resp.Data.ID)
	}
	if datasetID == "" {
		return "", fmt.Errorf("创建Dify dataset失败: 返回缺少id, body=%s", string(body))
	}
	return datasetID, nil
}

func createDifyDocumentByText(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID string, kb *models.KnowledgeBase) (string, error) {
	payload := map[string]interface{}{
		"name": buildAutoDocumentName(kb),
		"text": strings.TrimSpace(kb.Content),
	}
	if cfg.DatasetIndexingTechnique != "" {
		payload["indexing_technique"] = cfg.DatasetIndexingTechnique
	}

	var resp struct {
		Document struct {
			ID string `json:"id"`
		} `json:"document"`
		DocumentID string `json:"document_id"`
		Data       struct {
			Document struct {
				ID string `json:"id"`
			} `json:"document"`
			DocumentID string `json:"document_id"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/datasets/%s/document/create-by-text", url.PathEscape(datasetID))
	_, body, err := doDifyJSONRequest(client, http.MethodPost, buildDifyURL(cfg.BaseURL, path), cfg.APIKey, payload, &resp)
	if err != nil {
		return "", fmt.Errorf("创建Dify文档失败(dataset_id=%s): %w", datasetID, err)
	}

	docID := strings.TrimSpace(resp.Document.ID)
	if docID == "" {
		docID = strings.TrimSpace(resp.DocumentID)
	}
	if docID == "" {
		docID = strings.TrimSpace(resp.Data.Document.ID)
	}
	if docID == "" {
		docID = strings.TrimSpace(resp.Data.DocumentID)
	}
	if docID == "" {
		return "", fmt.Errorf("创建Dify文档失败: 返回缺少document_id, body=%s", string(body))
	}
	return docID, nil
}

func createDifyDocumentByFile(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID, fileName string, fileData []byte) (string, error) {
	fileName = sanitizeKnowledgeUploadFileName(fileName)
	if fileName == "" {
		fileName = "document.bin"
	}
	fields := map[string]string{}
	meta := map[string]interface{}{
		"name": fileName,
		// Dify create-by-file 要求必须携带 process_rule。
		"process_rule": map[string]interface{}{
			"mode":  "automatic",
			"rules": map[string]interface{}{},
		},
	}
	if cfg.DatasetIndexingTechnique != "" {
		meta["indexing_technique"] = cfg.DatasetIndexingTechnique
	}
	if metaBytes, err := json.Marshal(meta); err == nil {
		fields["data"] = string(metaBytes)
	}

	path := fmt.Sprintf("/datasets/%s/document/create-by-file", url.PathEscape(datasetID))
	endpoint := buildDifyURL(cfg.BaseURL, path)

	for attempt := 1; attempt <= difyFileUploadMaxAttempts; attempt++ {
		var resp struct {
			Document struct {
				ID string `json:"id"`
			} `json:"document"`
			DocumentID string `json:"document_id"`
			Data       struct {
				Document struct {
					ID string `json:"id"`
				} `json:"document"`
				DocumentID string `json:"document_id"`
			} `json:"data"`
		}
		_, body, err := doDifyMultipartFileRequest(client, http.MethodPost, endpoint, cfg.APIKey, fields, "file", fileName, fileData, &resp)
		if err != nil {
			if attempt == difyFileUploadMaxAttempts || !shouldRetryDifyRequest(err) {
				return "", fmt.Errorf("创建Dify文件文档失败(dataset_id=%s): %w", datasetID, err)
			}
			waitDuration := time.Duration(attempt) * difyFileUploadRetryStep
			log.Printf(
				"[KnowledgeSync][Dify] create-by-file retry dataset_id=%s attempt=%d/%d wait_ms=%d err=%v",
				datasetID,
				attempt,
				difyFileUploadMaxAttempts,
				waitDuration.Milliseconds(),
				err,
			)
			time.Sleep(waitDuration)
			continue
		}

		docID := strings.TrimSpace(resp.Document.ID)
		if docID == "" {
			docID = strings.TrimSpace(resp.DocumentID)
		}
		if docID == "" {
			docID = strings.TrimSpace(resp.Data.Document.ID)
		}
		if docID == "" {
			docID = strings.TrimSpace(resp.Data.DocumentID)
		}
		if docID == "" {
			return "", fmt.Errorf("创建Dify文件文档失败: 返回缺少document_id, body=%s", string(body))
		}
		return docID, nil
	}

	return "", fmt.Errorf("创建Dify文件文档失败(dataset_id=%s): 未知错误", datasetID)
}

func replaceDifyDocumentByFile(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID, oldDocumentID, fileName string, fileData []byte) (string, error) {
	newDocumentID, err := createDifyDocumentByFile(client, cfg, datasetID, fileName, fileData)
	if err != nil {
		return "", err
	}
	oldDocumentID = strings.TrimSpace(oldDocumentID)
	if oldDocumentID != "" && oldDocumentID != newDocumentID {
		if err := deleteDifyDocument(client, cfg, datasetID, oldDocumentID); err != nil {
			log.Printf("[KnowledgeSync][Dify] delete old file document warning dataset_id=%s old_document_id=%s err=%v", datasetID, oldDocumentID, err)
		}
	}
	return newDocumentID, nil
}

func updateDifyDocumentByText(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID, documentID string, kb *models.KnowledgeBase) error {
	payload := map[string]interface{}{
		"name": buildAutoDocumentName(kb),
		"text": strings.TrimSpace(kb.Content),
	}
	if cfg.DatasetIndexingTechnique != "" {
		payload["indexing_technique"] = cfg.DatasetIndexingTechnique
	}
	path := fmt.Sprintf("/datasets/%s/documents/%s/update-by-text", url.PathEscape(datasetID), url.PathEscape(documentID))
	_, _, err := doDifyJSONRequest(client, http.MethodPost, buildDifyURL(cfg.BaseURL, path), cfg.APIKey, payload, nil)
	if err != nil {
		return fmt.Errorf("更新Dify文档失败(dataset_id=%s, document_id=%s): %w", datasetID, documentID, err)
	}
	return nil
}

func deleteDifyDocument(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID, documentID string) error {
	path := fmt.Sprintf("/datasets/%s/documents/%s", url.PathEscape(datasetID), url.PathEscape(documentID))
	status, _, err := doDifyJSONRequest(client, http.MethodDelete, buildDifyURL(cfg.BaseURL, path), cfg.APIKey, nil, nil)
	if err != nil {
		if status == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("删除Dify文档失败(dataset_id=%s, document_id=%s): %w", datasetID, documentID, err)
	}
	return nil
}

func isDifyDatasetEmpty(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID string) (bool, error) {
	path := fmt.Sprintf("/datasets/%s/documents?page=1&limit=1", url.PathEscape(datasetID))
	var resp struct {
		Total int `json:"total"`
		Data  []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, body, err := doDifyJSONRequest(client, http.MethodGet, buildDifyURL(cfg.BaseURL, path), cfg.APIKey, nil, &resp)
	if err != nil {
		return false, fmt.Errorf("获取Dify文档列表失败(dataset_id=%s): %w", datasetID, err)
	}

	if resp.Total > 0 || len(resp.Data) > 0 {
		return false, nil
	}

	// 某些部署可能返回结构不同，做一次兜底解析
	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err == nil {
		if data, ok := generic["data"].([]interface{}); ok && len(data) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func deleteDifyDataset(client *http.Client, cfg *difyKnowledgeSyncConfig, datasetID string) error {
	path := fmt.Sprintf("/datasets/%s", url.PathEscape(datasetID))
	status, _, err := doDifyJSONRequest(client, http.MethodDelete, buildDifyURL(cfg.BaseURL, path), cfg.APIKey, nil, nil)
	if err != nil {
		if status == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("删除Dify dataset失败(dataset_id=%s): %w", datasetID, err)
	}
	return nil
}

func createRagflowDataset(client *http.Client, cfg *ragflowKnowledgeSyncConfig, kb *models.KnowledgeBase) (string, error) {
	payload := map[string]interface{}{
		"name":        buildAutoDatasetName(kb),
		"description": strings.TrimSpace(kb.Description),
	}
	if cfg.DatasetPermission != "" {
		payload["permission"] = cfg.DatasetPermission
	}
	if cfg.DatasetChunkMethod != "" {
		payload["chunk_method"] = cfg.DatasetChunkMethod
	}

	var resp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, body, err := doRagflowJSONRequest(client, http.MethodPost, buildRagflowURL(cfg.BaseURL, "/datasets"), cfg.APIKey, payload, &resp)
	if err != nil {
		return "", fmt.Errorf("创建RAGFlow dataset失败: %w", err)
	}

	datasetID := strings.TrimSpace(resp.Data.ID)
	if datasetID == "" {
		var generic map[string]interface{}
		if jsonErr := json.Unmarshal(body, &generic); jsonErr == nil {
			if data, ok := generic["data"].(map[string]interface{}); ok {
				if id, ok := data["id"].(string); ok {
					datasetID = strings.TrimSpace(id)
				}
			}
		}
	}
	if datasetID == "" {
		return "", fmt.Errorf("创建RAGFlow dataset失败: 返回缺少id, body=%s", string(body))
	}
	return datasetID, nil
}

func createAndParseRagflowDocumentByText(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, name, content string) (string, error) {
	fileName := buildRagflowUploadFileNameForText(name)
	documentID, err := uploadRagflowDocumentByBytes(client, cfg, datasetID, fileName, []byte(content))
	if err != nil {
		return "", err
	}
	if err := parseRagflowDocuments(client, cfg, datasetID, []string{documentID}); err != nil {
		return "", err
	}
	return documentID, nil
}

func replaceRagflowDocumentByText(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, oldDocumentID, name, content string) (string, error) {
	newDocumentID, err := createAndParseRagflowDocumentByText(client, cfg, datasetID, name, content)
	if err != nil {
		return "", err
	}
	oldDocumentID = strings.TrimSpace(oldDocumentID)
	if oldDocumentID != "" && oldDocumentID != newDocumentID {
		if err := deleteRagflowDocument(client, cfg, datasetID, oldDocumentID); err != nil {
			log.Printf("[KnowledgeSync][Ragflow] delete old document warning dataset_id=%s old_document_id=%s err=%v", datasetID, oldDocumentID, err)
		}
	}
	return newDocumentID, nil
}

func buildRagflowUploadFileNameForText(name string) string {
	fileName := sanitizeKnowledgeUploadFileName(strings.TrimSpace(name))
	if fileName == "" {
		fileName = "document"
	}
	if filepath.Ext(fileName) == "" {
		fileName = fileName + ".txt"
	}
	return fileName
}

func createAndParseRagflowDocumentByFile(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, fileName string, fileData []byte) (string, error) {
	fileName = sanitizeKnowledgeUploadFileName(fileName)
	if fileName == "" {
		fileName = "document.bin"
	}
	documentID, err := uploadRagflowDocumentByBytes(client, cfg, datasetID, fileName, fileData)
	if err != nil {
		return "", err
	}
	if err := parseRagflowDocuments(client, cfg, datasetID, []string{documentID}); err != nil {
		return "", err
	}
	return documentID, nil
}

func replaceRagflowDocumentByFile(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, oldDocumentID, fileName string, fileData []byte) (string, error) {
	newDocumentID, err := createAndParseRagflowDocumentByFile(client, cfg, datasetID, fileName, fileData)
	if err != nil {
		return "", err
	}
	oldDocumentID = strings.TrimSpace(oldDocumentID)
	if oldDocumentID != "" && oldDocumentID != newDocumentID {
		if err := deleteRagflowDocument(client, cfg, datasetID, oldDocumentID); err != nil {
			log.Printf("[KnowledgeSync][Ragflow] delete old document warning dataset_id=%s old_document_id=%s err=%v", datasetID, oldDocumentID, err)
		}
	}
	return newDocumentID, nil
}

func uploadRagflowDocumentByBytes(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, fileName string, fileData []byte) (string, error) {
	fileName = sanitizeKnowledgeUploadFileName(fileName)
	if fileName == "" {
		fileName = "document.bin"
	}

	endpoint := buildRagflowURL(cfg.BaseURL, fmt.Sprintf("/datasets/%s/documents", url.PathEscape(datasetID)))
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, body, err := doRagflowMultipartFileRequest(
		client,
		http.MethodPost,
		endpoint,
		cfg.APIKey,
		nil,
		"file",
		fileName,
		fileData,
		&resp,
	)
	if err != nil {
		return "", fmt.Errorf("上传RAGFlow文档失败(dataset_id=%s): %w", datasetID, err)
	}

	documentID := ""
	if len(resp.Data) > 0 {
		documentID = strings.TrimSpace(resp.Data[0].ID)
	}
	if documentID == "" {
		var generic map[string]interface{}
		if jsonErr := json.Unmarshal(body, &generic); jsonErr == nil {
			if dataArr, ok := generic["data"].([]interface{}); ok && len(dataArr) > 0 {
				if first, ok := dataArr[0].(map[string]interface{}); ok {
					if id, ok := first["id"].(string); ok {
						documentID = strings.TrimSpace(id)
					}
				}
			}
		}
	}
	if documentID == "" {
		return "", fmt.Errorf("上传RAGFlow文档失败: 返回缺少document_id, body=%s", string(body))
	}
	return documentID, nil
}

func parseRagflowDocuments(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID string, documentIDs []string) error {
	validIDs := make([]string, 0, len(documentIDs))
	for _, id := range documentIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return nil
	}

	payload := map[string]interface{}{
		"document_ids": validIDs,
	}
	endpoint := buildRagflowURL(cfg.BaseURL, fmt.Sprintf("/datasets/%s/chunks", url.PathEscape(datasetID)))
	if _, _, err := doRagflowJSONRequest(client, http.MethodPost, endpoint, cfg.APIKey, payload, nil); err != nil {
		return fmt.Errorf("触发RAGFlow文档解析失败(dataset_id=%s): %w", datasetID, err)
	}
	return nil
}

func deleteRagflowDocument(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID, documentID string) error {
	payload := map[string]interface{}{
		"ids": []string{strings.TrimSpace(documentID)},
	}
	endpoint := buildRagflowURL(cfg.BaseURL, fmt.Sprintf("/datasets/%s/documents", url.PathEscape(datasetID)))
	status, _, err := doRagflowJSONRequest(client, http.MethodDelete, endpoint, cfg.APIKey, payload, nil)
	if err != nil {
		if status == http.StatusNotFound || isRagflowNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("删除RAGFlow文档失败(dataset_id=%s, document_id=%s): %w", datasetID, documentID, err)
	}
	return nil
}

func isRagflowDatasetEmpty(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID string) (bool, error) {
	endpoint := buildRagflowURL(cfg.BaseURL, fmt.Sprintf("/datasets/%s/documents?page=1&page_size=1", url.PathEscape(datasetID)))
	var resp struct {
		Total int `json:"total"`
		Data  []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_, body, err := doRagflowJSONRequest(client, http.MethodGet, endpoint, cfg.APIKey, nil, &resp)
	if err != nil {
		return false, fmt.Errorf("获取RAGFlow文档列表失败(dataset_id=%s): %w", datasetID, err)
	}

	if resp.Total > 0 || len(resp.Data) > 0 {
		return false, nil
	}

	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err == nil {
		if data, ok := generic["data"].([]interface{}); ok && len(data) > 0 {
			return false, nil
		}
	}
	return true, nil
}

func deleteRagflowDataset(client *http.Client, cfg *ragflowKnowledgeSyncConfig, datasetID string) error {
	payload := map[string]interface{}{
		"ids": []string{strings.TrimSpace(datasetID)},
	}
	endpoint := buildRagflowURL(cfg.BaseURL, "/datasets")
	status, _, err := doRagflowJSONRequest(client, http.MethodDelete, endpoint, cfg.APIKey, payload, nil)
	if err != nil {
		if status == http.StatusNotFound || isRagflowNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("删除RAGFlow dataset失败(dataset_id=%s): %w", datasetID, err)
	}
	return nil
}

func isRagflowNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "not exist") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "does not have")
}

func doRagflowJSONRequest(client *http.Client, method, endpoint, apiKey string, payload interface{}, out interface{}) (int, []byte, error) {
	var bodyReader io.Reader
	var payloadBytes []byte
	if payload != nil {
		payloadBytesLocal, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("编码请求体失败: %w", err)
		}
		payloadBytes = payloadBytesLocal
		bodyReader = bytes.NewReader(payloadBytes)
	}
	log.Printf("[KnowledgeSync][Ragflow] Request method=%s url=%s payload=%s", method, endpoint, serializePayloadForLog(payloadBytes))

	startAt := time.Now()
	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[KnowledgeSync][Ragflow] Response method=%s url=%s elapsed_ms=%d error=%v", method, endpoint, time.Since(startAt).Milliseconds(), err)
		return 0, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf(
		"[KnowledgeSync][Ragflow] Response method=%s url=%s status=%d elapsed_ms=%d body=%s",
		method,
		endpoint,
		resp.StatusCode,
		time.Since(startAt).Milliseconds(),
		truncateForLog(string(bodyBytes), 4000),
	)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, bodyBytes, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	if len(bodyBytes) > 0 {
		var envelope struct {
			Code    interface{} `json:"code"`
			Message string      `json:"message"`
		}
		if err := json.Unmarshal(bodyBytes, &envelope); err == nil && envelope.Code != nil {
			if code, ok := parseInt(envelope.Code); ok && code != 0 {
				return resp.StatusCode, bodyBytes, fmt.Errorf("code=%d message=%s body=%s", code, strings.TrimSpace(envelope.Message), string(bodyBytes))
			}
		}
	}

	if out != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			log.Printf("解析RAGFlow响应失败: %v, body: %s", err, string(bodyBytes))
			return resp.StatusCode, bodyBytes, fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return resp.StatusCode, bodyBytes, nil
}

func doRagflowMultipartFileRequest(
	client *http.Client,
	method, endpoint, apiKey string,
	fields map[string]string,
	fileField, fileName string,
	fileContent []byte,
	out interface{},
) (int, []byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return 0, nil, fmt.Errorf("写入表单字段失败: %w", err)
		}
	}
	fileWriter, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return 0, nil, fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err := fileWriter.Write(fileContent); err != nil {
		return 0, nil, fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	log.Printf("[KnowledgeSync][Ragflow] Request method=%s url=%s multipart_file=%s size=%d", method, endpoint, fileName, len(fileContent))
	startAt := time.Now()
	req, err := http.NewRequest(method, endpoint, &body)
	if err != nil {
		return 0, nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[KnowledgeSync][Ragflow] Response method=%s url=%s elapsed_ms=%d error=%v", method, endpoint, time.Since(startAt).Milliseconds(), err)
		return 0, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf(
		"[KnowledgeSync][Ragflow] Response method=%s url=%s status=%d elapsed_ms=%d body=%s",
		method,
		endpoint,
		resp.StatusCode,
		time.Since(startAt).Milliseconds(),
		truncateForLog(string(bodyBytes), 4000),
	)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, bodyBytes, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	if len(bodyBytes) > 0 {
		var envelope struct {
			Code    interface{} `json:"code"`
			Message string      `json:"message"`
		}
		if err := json.Unmarshal(bodyBytes, &envelope); err == nil && envelope.Code != nil {
			if code, ok := parseInt(envelope.Code); ok && code != 0 {
				return resp.StatusCode, bodyBytes, fmt.Errorf("code=%d message=%s body=%s", code, strings.TrimSpace(envelope.Message), string(bodyBytes))
			}
		}
	}

	if out != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			log.Printf("解析RAGFlow响应失败: %v, body: %s", err, string(bodyBytes))
			return resp.StatusCode, bodyBytes, fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return resp.StatusCode, bodyBytes, nil
}

func doDifyJSONRequest(client *http.Client, method, endpoint, apiKey string, payload interface{}, out interface{}) (int, []byte, error) {
	var bodyReader io.Reader
	var payloadBytes []byte
	if payload != nil {
		payloadBytesLocal, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, fmt.Errorf("编码请求体失败: %w", err)
		}
		payloadBytes = payloadBytesLocal
		bodyReader = bytes.NewReader(payloadBytes)
	}
	log.Printf("[KnowledgeSync][Dify] Request method=%s url=%s payload=%s", method, endpoint, serializePayloadForLog(payloadBytes))

	startAt := time.Now()
	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return 0, nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[KnowledgeSync][Dify] Response method=%s url=%s elapsed_ms=%d error=%v", method, endpoint, time.Since(startAt).Milliseconds(), err)
		return 0, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf(
		"[KnowledgeSync][Dify] Response method=%s url=%s status=%d elapsed_ms=%d body=%s",
		method,
		endpoint,
		resp.StatusCode,
		time.Since(startAt).Milliseconds(),
		truncateForLog(string(bodyBytes), 4000),
	)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, bodyBytes, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	if out != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			log.Printf("解析Dify响应失败: %v, body: %s", err, string(bodyBytes))
			return resp.StatusCode, bodyBytes, fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return resp.StatusCode, bodyBytes, nil
}

func doDifyMultipartFileRequest(
	client *http.Client,
	method, endpoint, apiKey string,
	fields map[string]string,
	fileField, fileName string,
	fileContent []byte,
	out interface{},
) (int, []byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return 0, nil, fmt.Errorf("写入表单字段失败: %w", err)
		}
	}
	fileWriter, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return 0, nil, fmt.Errorf("创建文件字段失败: %w", err)
	}
	if _, err := fileWriter.Write(fileContent); err != nil {
		return 0, nil, fmt.Errorf("写入文件内容失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return 0, nil, fmt.Errorf("关闭表单写入器失败: %w", err)
	}

	fieldsBytes, _ := json.Marshal(fields)
	log.Printf(
		"[KnowledgeSync][Dify] Request method=%s url=%s multipart_file=%s size=%d fields=%s",
		method,
		endpoint,
		fileName,
		len(fileContent),
		serializePayloadForLog(fieldsBytes),
	)
	startAt := time.Now()
	req, err := http.NewRequest(method, endpoint, &body)
	if err != nil {
		return 0, nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[KnowledgeSync][Dify] Response method=%s url=%s elapsed_ms=%d error=%v", method, endpoint, time.Since(startAt).Milliseconds(), err)
		return 0, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	log.Printf(
		"[KnowledgeSync][Dify] Response method=%s url=%s status=%d elapsed_ms=%d body=%s",
		method,
		endpoint,
		resp.StatusCode,
		time.Since(startAt).Milliseconds(),
		truncateForLog(string(bodyBytes), 4000),
	)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, bodyBytes, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	if out != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, out); err != nil {
			log.Printf("解析Dify响应失败: %v, body: %s", err, string(bodyBytes))
			return resp.StatusCode, bodyBytes, fmt.Errorf("解析响应失败: %w", err)
		}
	}
	return resp.StatusCode, bodyBytes, nil
}

func serializePayloadForLog(payloadBytes []byte) string {
	if len(payloadBytes) == 0 {
		return "-"
	}
	return truncateForLog(string(payloadBytes), 4000)
}

func shouldRetryDifyRequest(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || netErr.Temporary() {
			return true
		}
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "client.timeout exceeded") ||
		strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "server closed idle connection") ||
		strings.Contains(msg, "no such host") {
		return true
	}
	if strings.Contains(msg, "status=408") ||
		strings.Contains(msg, "status=429") ||
		strings.Contains(msg, "status=500") ||
		strings.Contains(msg, "status=502") ||
		strings.Contains(msg, "status=503") ||
		strings.Contains(msg, "status=504") {
		return true
	}
	return false
}

func parseInt(input interface{}) (int, bool) {
	switch v := input.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return int(i), true
		}
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return 0, false
		}
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err == nil {
			return i, true
		}
	}
	return 0, false
}

func truncateForLog(input string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 4000
	}
	if len(input) <= maxLen {
		return input
	}
	return input[:maxLen] + "...(truncated)"
}
