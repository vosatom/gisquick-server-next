package project

import (
	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/gisquick/gisquick-server/internal/infrastructure/cache"
	"go.uber.org/zap"
)

type SimpleProjectsLimiter struct {
	config domain.AccountConfig
}

func NewSimpleProjectsLimiter(defaultConfig domain.AccountConfig) *SimpleProjectsLimiter {
	return &SimpleProjectsLimiter{config: defaultConfig}
}

func (s *SimpleProjectsLimiter) GetAccountLimits(username string) (domain.AccountConfig, error) {
	return s.config, nil
}

type ConfigurableProjectsLimiter struct {
	reader *cache.FilesConfigReader[domain.AccountConfig]
}

func NewConfigurableProjectsLimiter(log *zap.SugaredLogger, configPath string, defaultConfig domain.AccountConfig) *ConfigurableProjectsLimiter {
	reader := cache.NewFilesConfigReader(log, configPath, defaultConfig)
	return &ConfigurableProjectsLimiter{reader: reader}
}

func (l *ConfigurableProjectsLimiter) GetAccountLimits(username string) (domain.AccountConfig, error) {
	c, err := l.reader.GetConfig(username)
	if err != nil {
		return domain.AccountConfig{}, err
	}
	return c, nil
}
