package service

import (
	"context"
	"errors"
)

func ImageStudioGroupAllowed(groupID *int64, allowedIDs []int64) bool {
	if groupID == nil || *groupID <= 0 || len(allowedIDs) == 0 {
		return false
	}
	for _, allowedID := range allowedIDs {
		if allowedID == *groupID {
			return true
		}
	}
	return false
}

func (s *SettingService) GetImageStudioAvailableGroupIDs(ctx context.Context) []int64 {
	if s == nil || s.settingRepo == nil {
		return []int64{}
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyImageStudioAvailableGroupIDs)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			return []int64{}
		}
		return []int64{}
	}
	return parseInt64ListSetting(value)
}

func (s *ImageStudioJobService) ValidateAPIKeyAvailableForImageStudio(ctx context.Context, apiKey *APIKey) error {
	var groupID *int64
	if apiKey != nil {
		groupID = apiKey.GroupID
		if groupID == nil && apiKey.Group != nil && apiKey.Group.ID > 0 {
			id := apiKey.Group.ID
			groupID = &id
		}
	}
	if !ImageStudioGroupAllowed(groupID, s.imageStudioAvailableGroupIDs(ctx)) {
		return ErrImageStudioGroupNotAvailable
	}
	return nil
}

func (s *ImageStudioJobService) imageStudioAvailableGroupIDs(ctx context.Context) []int64 {
	if s == nil || s.settingService == nil {
		return []int64{}
	}
	return s.settingService.GetImageStudioAvailableGroupIDs(ctx)
}
