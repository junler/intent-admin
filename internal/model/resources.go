package model

import (
	"encoding/json"
	"time"
)

// AIProvider never serializes stored credentials. Display views add a mask.
type AIProvider struct {
	ProviderID          int64           `gorm:"column:provider_id;primaryKey" json:"providerId"`
	Name                string          `json:"name"`
	Vendor              string          `json:"vendor"`
	Adapter             string          `json:"adapter"`
	BaseURL             string          `gorm:"column:base_url" json:"baseUrl"`
	APIKeyEnc           string          `gorm:"column:api_key_enc" json:"-"`
	DefaultModel        string          `json:"defaultModel"`
	Models              json.RawMessage `gorm:"type:jsonb" json:"models"`
	SupportsToolCalling bool            `json:"supportsToolCalling"`
	Temperature         *float64        `json:"temperature"`
	MaxTokens           *int            `json:"maxTokens"`
	TimeoutSec          int             `json:"timeoutSec"`
	IsDefault           bool            `json:"isDefault"`
	LastTestedAt        *time.Time      `json:"lastTestedAt"`
	LastTestResult      json.RawMessage `gorm:"type:jsonb" json:"lastTestResult"`
	Sort                int             `json:"sort"`
	Status              string          `json:"status"`
	DelFlag             string          `json:"-"`
	IdentityAudit
}

func (AIProvider) TableName() string { return "sys_ai_provider" }

type StorageConfig struct {
	StorageID        int64           `gorm:"column:storage_id;primaryKey" json:"storageId"`
	Name             string          `json:"name"`
	Driver           string          `json:"driver"`
	Preset           string          `json:"preset"`
	Endpoint         string          `json:"endpoint"`
	Region           string          `json:"region"`
	Bucket           string          `json:"bucket"`
	AccessKeyID      string          `gorm:"column:access_key_id" json:"accessKeyId"`
	SecretKeyEnc     string          `gorm:"column:secret_key_enc" json:"-"`
	UseSSL           bool            `gorm:"column:use_ssl" json:"useSsl"`
	PathStyle        bool            `json:"pathStyle"`
	BaseURL          string          `gorm:"column:base_url" json:"baseUrl"`
	URLMode          string          `gorm:"column:url_mode" json:"urlMode"`
	PresignExpireSec int             `json:"presignExpireSec"`
	LocalDir         string          `json:"localDir"`
	MaxSize          int64           `json:"maxSize"`
	AllowExt         json.RawMessage `gorm:"type:jsonb" json:"allowExt"`
	IsDefault        bool            `json:"isDefault"`
	LastTestedAt     *time.Time      `json:"lastTestedAt"`
	LastTestResult   json.RawMessage `gorm:"type:jsonb" json:"lastTestResult"`
	Status           string          `json:"status"`
	DelFlag          string          `json:"-"`
	IdentityAudit
}

func (StorageConfig) TableName() string { return "sys_storage_config" }

type Upload struct {
	UploadID  string `gorm:"column:upload_id;primaryKey" json:"uploadId"`
	FileName  string `json:"fileName"`
	ObjectKey string `json:"objectKey"`
	URL       string `gorm:"column:url" json:"url"`
	Ext       string `json:"ext"`
	Size      int64  `json:"size"`
	Mime      string `json:"mime"`
	BizType   string `json:"bizType"`
	StorageID int64  `json:"storageId"`
	Status    string `json:"status"`
	DelFlag   string `json:"-"`
	IdentityAudit
}

func (Upload) TableName() string { return "sys_upload" }
