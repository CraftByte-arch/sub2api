package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestApplyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(t *testing.T) {
	passiveBody := []byte(`{"model":"gpt-5.4","input":"hello","tools":[{"type":"function","name":"shell"},{"type":"image_generation","output_format":"png"}],"tool_choice":"auto"}`)

	t.Run("strip removes passive declaration and keeps request allowed", func(t *testing.T) {
		updated, changed, rejected, err := applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(openAIResponsesEndpoint, "gpt-5.4", passiveBody, ImageGenerationToolDeclarationPolicyStrip)
		require.NoError(t, err)
		require.True(t, changed)
		require.False(t, rejected)
		require.False(t, gjson.GetBytes(updated, `tools.#(type=="image_generation")`).Exists())
		require.True(t, gjson.GetBytes(updated, `tools.#(type=="function")`).Exists())
	})

	t.Run("allow keeps passive declaration", func(t *testing.T) {
		updated, changed, rejected, err := applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(openAIResponsesEndpoint, "gpt-5.4", passiveBody, ImageGenerationToolDeclarationPolicyAllow)
		require.NoError(t, err)
		require.False(t, changed)
		require.False(t, rejected)
		require.Equal(t, string(passiveBody), string(updated))
		require.True(t, gjson.GetBytes(updated, `tools.#(type=="image_generation")`).Exists())
	})

	t.Run("reject rejects passive declaration", func(t *testing.T) {
		updated, changed, rejected, err := applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(openAIResponsesEndpoint, "gpt-5.4", passiveBody, ImageGenerationToolDeclarationPolicyReject)
		require.NoError(t, err)
		require.False(t, changed)
		require.True(t, rejected)
		require.Equal(t, string(passiveBody), string(updated))
	})

	t.Run("explicit tool choice remains actual image intent", func(t *testing.T) {
		body := []byte(`{"model":"gpt-5.4","tools":[{"type":"image_generation"}],"tool_choice":{"type":"image_generation"}}`)
		updated, changed, rejected, err := applyOpenAIImageGenerationToolDeclarationPolicyToRawPayload(openAIResponsesEndpoint, "gpt-5.4", body, ImageGenerationToolDeclarationPolicyStrip)
		require.NoError(t, err)
		require.False(t, changed)
		require.False(t, rejected)
		require.Equal(t, string(body), string(updated))
		require.True(t, IsActualImageGenerationIntent(openAIResponsesEndpoint, "gpt-5.4", updated))
	})
}
