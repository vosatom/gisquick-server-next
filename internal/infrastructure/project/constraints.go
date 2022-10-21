package project

import (
	"github.com/gisquick/gisquick-server/internal/domain"
)

type SimpleProjectsLimiter struct {
	MaxProjectSize   int64
	MaxProjectsCount int
	StorageLimit     int64
	repo             domain.ProjectsRepository
}

func (s *SimpleProjectsLimiter) CheckProjectsLimit(projectName string, count int) (bool, error) {
	return s.MaxProjectsCount == -1 || count <= s.MaxProjectsCount, nil
}

func (s *SimpleProjectsLimiter) CheckProjectSizeLimit(projectName string, size int64) (bool, error) {
	return s.MaxProjectSize == -1 || size <= s.MaxProjectSize, nil
}

func (s *SimpleProjectsLimiter) CheckStorageLimit(username string, size int64) (bool, error) {
	return s.StorageLimit == -1 || size <= s.StorageLimit, nil
}

func (s *SimpleProjectsLimiter) HasProjectSizeLimit(username string) bool {
	return s.MaxProjectSize != -1
}

func (s *SimpleProjectsLimiter) HasUserStorageLimit(username string) bool {
	return s.StorageLimit != -1
}
