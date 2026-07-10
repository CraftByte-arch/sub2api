package service

import (
	"context"
	"errors"
)

type openAIImageGenerationPreflightResult struct {
	Body                []byte
	DeclarationStripped bool
	DeclarationRejected bool
	ImageIntent         bool
	ActualImageIntent   bool
}

func evaluateOpenAIImageGenerationPreflight(endpoint string, requestedModel string, body []byte, policy string) (openAIImageGenerationPreflightResult, error) {
	updated, changed, rejected, err := applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(endpoint, requestedModel, body, policy)
	if err != nil {
		return openAIImageGenerationPreflightResult{}, err
	}
	result := openAIImageGenerationPreflightResult{
		Body:                updated,
		DeclarationStripped: changed,
		DeclarationRejected: rejected,
	}
	if rejected {
		return result, nil
	}
	result.ImageIntent = IsImageGenerationIntent(endpoint, requestedModel, updated)
	result.ActualImageIntent = IsActualImageGenerationIntent(endpoint, requestedModel, updated)
	return result, nil
}

func applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(endpoint string, requestedModel string, body []byte, policy string) ([]byte, bool, bool, error) {
	if !HasPassiveImageGenerationToolDeclaration(endpoint, requestedModel, body) {
		return body, false, false, nil
	}
	switch NormalizeImageGenerationToolDeclarationPolicy(policy) {
	case ImageGenerationToolDeclarationPolicyAllow:
		return body, false, false, nil
	case ImageGenerationToolDeclarationPolicyReject:
		return body, false, true, nil
	default:
		updated, changed, err := stripOpenAIImageGenerationToolFromRawPayload(body)
		return updated, changed, false, err
	}
}

func (s *SettingService) GetImageGenerationToolDeclarationPolicy(ctx context.Context) string {
	if s == nil || s.settingRepo == nil {
		return ImageGenerationToolDeclarationPolicyStrip
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyImageGenerationToolDeclarationPolicy)
	if err != nil {
		if !errors.Is(err, ErrSettingNotFound) {
			return ImageGenerationToolDeclarationPolicyStrip
		}
		return ImageGenerationToolDeclarationPolicyStrip
	}
	return NormalizeImageGenerationToolDeclarationPolicy(value)
}

func (s *OpenAIGatewayService) imageGenerationToolDeclarationPolicy(ctx context.Context) string {
	if s == nil || s.settingService == nil {
		return ImageGenerationToolDeclarationPolicyStrip
	}
	return s.settingService.GetImageGenerationToolDeclarationPolicy(ctx)
}
