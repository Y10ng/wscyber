package domain

import "context"

// MissionModelBinding 仅供持久层与研究路由；版本/环境指纹不作为用户操作。
type MissionModelBinding struct {
	MissionID              string
	Source                 string
	ConfigID               string
	ConfigRevision         int64
	EnvironmentFingerprint string
}

type MissionModelView struct {
	MissionID string `json:"mission_id"`
	Source    string `json:"source"`
	ConfigID  string `json:"config_id,omitempty"`
	Name      string `json:"name"`
	Model     string `json:"model"`
	Protocol  string `json:"protocol"`
	Enabled   bool   `json:"enabled"`
}

// ExplorationModelOptions 的校验器只在事务持锁期间本地解密，不能再次查询数据库或外发。
type ExplorationModelOptions struct {
	ContentRecordingAvailable bool
	EnvironmentConfigured     bool
	EnvironmentFingerprint    string
	Validate                  func(SealedModelConfig) error
}

type MissionModelRepository interface {
	StartExplorationWithModel(context.Context, string, ExplorationInput, ExplorationModelOptions) (ExplorationStart, error)
	// nil 表示确实存在但未绑定的历史Mission；不存在Mission返回ErrNotFound。
	GetMissionModelBinding(context.Context, string) (*MissionModelBinding, error)
}
