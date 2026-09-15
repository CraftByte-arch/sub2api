package onlineusers

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api-account-auto-scheduler/internal/model"
)

const groupAccessQuery = `
SELECT users.id,
       COALESCE(users.username, ''),
       users.email,
       users.status,
       EXISTS (
         SELECT 1
         FROM user_allowed_groups target_access
         WHERE target_access.user_id = users.id
           AND target_access.group_id = $2
       ) AS authorized
FROM user_allowed_groups source_access
JOIN users ON users.id = source_access.user_id
WHERE source_access.group_id = $1
  AND users.deleted_at IS NULL
ORDER BY users.id`

// ListGroupAccessUsers reads only the explicit group membership relation. It
// intentionally avoids the main service's general user-list endpoint, whose
// response enrichment also reads usage and per-user rate data.
func (s *Service) ListGroupAccessUsers(ctx context.Context, targetID, sourceID int64) ([]model.GroupAccessEntry, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("group access read-only database is not configured")
	}
	if targetID <= 0 || sourceID <= 0 {
		return nil, errors.New("group IDs must be positive")
	}
	queryCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(queryCtx, groupAccessQuery, sourceID, targetID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	users := make([]model.GroupAccessEntry, 0)
	for rows.Next() {
		var user model.GroupAccessEntry
		if err := rows.Scan(&user.ID, &user.Username, &user.Email, &user.Status, &user.Authorized); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}
