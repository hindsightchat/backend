package websocket

import (
	"context"
	"encoding/json"
	"time"

	valkeydb "github.com/hindsightchat/backend/src/lib/dbs/valkey"
	"github.com/hindsightchat/backend/src/types"
	uuid "github.com/satori/go.uuid"
)

const presenceTTL = 5 * time.Minute

type PresenceData struct {
	Status    string          `json:"status"`
	Activity  *types.Activity `json:"activity,omitempty"`
	UpdatedAt int64           `json:"updated_at"`
}

type PresenceManager struct{}

func NewPresenceManager() *PresenceManager {
	return &PresenceManager{}
}

func (p *PresenceManager) key(userID uuid.UUID) string {
	return valkeydb.PRESENCE_PREFIX + userID.String()
}

func (p *PresenceManager) SetOnline(userID uuid.UUID, status string, activity *types.Activity) error {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()

	data := PresenceData{
		Status:    status,
		Activity:  activity,
		UpdatedAt: time.Now().Unix(),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	return rdb.Set(ctx, p.key(userID), jsonData, presenceTTL).Err()
}

func (p *PresenceManager) SetOffline(userID uuid.UUID) error {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()
	return rdb.Del(ctx, p.key(userID)).Err()
}

func (p *PresenceManager) GetPresence(userID uuid.UUID) (*PresenceData, error) {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()

	data, err := rdb.Get(ctx, p.key(userID)).Bytes()
	if err != nil {
		return nil, err
	}

	var presence PresenceData
	if err := json.Unmarshal(data, &presence); err != nil {
		return nil, err
	}

	return &presence, nil
}

func (p *PresenceManager) GetMultiplePresences(userIDs []uuid.UUID) map[uuid.UUID]*PresenceData {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()

	result := make(map[uuid.UUID]*PresenceData)

	if len(userIDs) == 0 {
		return result
	}

	keys := make([]string, len(userIDs))
	for i, id := range userIDs {
		keys[i] = p.key(id)
	}

	values, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return result
	}

	for i, val := range values {
		if val == nil {
			continue
		}

		str, ok := val.(string)
		if !ok {
			continue
		}

		var presence PresenceData
		if err := json.Unmarshal([]byte(str), &presence); err != nil {
			continue
		}

		result[userIDs[i]] = &presence
	}

	return result
}

func (p *PresenceManager) IsOnline(userID uuid.UUID) bool {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()
	exists, _ := rdb.Exists(ctx, p.key(userID)).Result()
	return exists > 0
}

func (p *PresenceManager) RefreshPresence(userID uuid.UUID) error {
	ctx := context.Background()
	rdb := valkeydb.GetValkeyClient()
	return rdb.Expire(ctx, p.key(userID), presenceTTL).Err()
}

func (p *PresenceManager) UpdateActivity(userID uuid.UUID, activity *types.Activity) error {
	presence, err := p.GetPresence(userID)
	if err != nil {
		return nil
	}

	presence.Activity = activity
	presence.UpdatedAt = time.Now().Unix()

	return p.SetOnline(userID, presence.Status, activity)
}
