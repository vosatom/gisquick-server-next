package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gisquick/gisquick-server/internal/domain"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

type Notification struct {
	ID         string    `json:"id,omitempty"`
	Title      string    `json:"title"`
	App        string    `json:"app"`
	Users      string    `json:"users"`
	Expiration time.Time `json:"expiration"`
	Projects   string    `json:"projects"`
	Message    string    `json:"msg"`
}

type RedisNotificationStore struct {
	log *zap.SugaredLogger
	rdb *redis.Client
}

// const (
// 	Day = 24 * time.Hour
// )
var (
	ErrInvalidDuration = errors.New("invalid duration value")
)

func NewRedisNotificationStore(log *zap.SugaredLogger, rdb *redis.Client) *RedisNotificationStore {
	return &RedisNotificationStore{log: log, rdb: rdb}
}

func (s *RedisNotificationStore) SaveNotification(ctx context.Context, notification Notification) error {
	key := fmt.Sprintf("notification:%s", notification.ID)
	duration := time.Duration(0)
	if !notification.Expiration.IsZero() {
		duration = notification.Expiration.Sub(time.Now())
	}
	if duration < 0 {
		return ErrInvalidDuration
	}
	value, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	if err := s.rdb.Set(ctx, key, string(value), duration).Err(); err != nil {
		return fmt.Errorf("redis save notification: %v", err)
	}
	return nil
}

func (s *RedisNotificationStore) DeleteNotification(ctx context.Context, id string) error {
	key := fmt.Sprintf("notification:%s", id)
	return s.rdb.Del(ctx, key).Err()
}

func (s *RedisNotificationStore) GetNotifications() ([]Notification, error) {
	// Retrieve all keys matching a pattern (e.g., all notifications)
	keys := s.rdb.Keys(context.Background(), "notification:*").Val()
	s.log.Debugw("GetNotifications", "keys", keys)

	notifications := []Notification{}
	if len(keys) > 0 {
		// Retrieve values for the matching keys
		result, err := s.rdb.MGet(context.Background(), keys...).Result()
		if err != nil {
			return nil, err
		}
		// Convert the result to a string slice
		for i, value := range result {
			if value != nil {
				var notification Notification
				if err := json.Unmarshal([]byte(value.(string)), &notification); err == nil {
					notification.ID = strings.TrimPrefix(keys[i], "notification:")
					notifications = append(notifications, notification)
				} else {
					s.log.Warnw("GetNotifications", zap.Error(err))
				}
			}
		}
	}
	return notifications, nil
}

func (s *RedisNotificationStore) GetMapProjectNotifications(projectName string, user domain.User) ([]Notification, error) {
	allNotifications, err := s.GetNotifications()
	if err != nil {
		return nil, err
	}
	notifications := []Notification{}
	for _, n := range allNotifications {
		if n.App != "map" ||
			(n.Users == "authenticated" && !user.IsAuthenticated) ||
			(n.Users == "not_authenticated" && user.IsAuthenticated) ||
			(n.Users == "owner" && !strings.HasPrefix(projectName, user.Username+"/")) {
			continue
		}
		for _, p := range strings.Split(n.Projects, ",") {
			if strings.HasPrefix(projectName, p) {
				notifications = append(notifications, n)
				break
			}
		}
	}
	return notifications, nil
}

func (s *RedisNotificationStore) GetSettingsNotifications(projectName string, user domain.User) ([]Notification, error) {
	allNotifications, err := s.GetNotifications()
	if err != nil {
		return nil, err
	}
	notifications := []Notification{}
	for _, n := range allNotifications {
		if n.App == "settings" {
			notifications = append(notifications, n)
		}
	}
	return notifications, nil
}
