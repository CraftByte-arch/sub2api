package handler

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBuildImageStudioJobPayload_UsesImagesGenerationFormat(t *testing.T) {
	payload, err := buildImageStudioJobPayload(imageStudioCreateJobRequest{
		Prompt:       "draw a red fox",
		Model:        "gpt-image-2",
		Size:         "1024x1024",
		OutputFormat: "webp",
		Quality:      "high",
		Background:   "transparent",
		Moderation:   "low",
	}, service.ImageStudioJobModeGenerate)
	require.NoError(t, err)
	require.True(t, json.Valid(payload))

	require.Equal(t, "gpt-image-2", gjson.GetBytes(payload, "model").String())
	require.Equal(t, "draw a red fox", gjson.GetBytes(payload, "prompt").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(payload, "size").String())
	require.Equal(t, "b64_json", gjson.GetBytes(payload, "response_format").String())
	require.Equal(t, "webp", gjson.GetBytes(payload, "output_format").String())
	require.Equal(t, "high", gjson.GetBytes(payload, "quality").String())
	require.Equal(t, "transparent", gjson.GetBytes(payload, "background").String())
	require.Equal(t, "low", gjson.GetBytes(payload, "moderation").String())
	require.False(t, gjson.GetBytes(payload, "input").Exists())
	require.False(t, gjson.GetBytes(payload, "tools").Exists())
	require.False(t, gjson.GetBytes(payload, "tool_choice").Exists())
}

func TestBuildImageStudioJobPayload_UsesImagesEditFormat(t *testing.T) {
	payload, err := buildImageStudioJobPayload(imageStudioCreateJobRequest{
		Prompt:        "replace the sky",
		Model:         "gpt-image-2",
		OutputFormat:  "png",
		ImageDataURLs: []string{"data:image/png;base64,aW1hZ2U="},
		MaskDataURL:   "data:image/png;base64,bWFzaw==",
	}, service.ImageStudioJobModeEdit)
	require.NoError(t, err)
	require.True(t, json.Valid(payload))

	require.Equal(t, "gpt-image-2", gjson.GetBytes(payload, "model").String())
	require.Equal(t, "replace the sky", gjson.GetBytes(payload, "prompt").String())
	require.Equal(t, "data:image/png;base64,aW1hZ2U=", gjson.GetBytes(payload, "images.0.image_url").String())
	require.Equal(t, "data:image/png;base64,bWFzaw==", gjson.GetBytes(payload, "mask.image_url").String())
	require.Equal(t, "b64_json", gjson.GetBytes(payload, "response_format").String())
	require.Equal(t, "png", gjson.GetBytes(payload, "output_format").String())
	require.False(t, gjson.GetBytes(payload, "input").Exists())
	require.False(t, gjson.GetBytes(payload, "tools").Exists())
	require.False(t, gjson.GetBytes(payload, "tool_choice").Exists())
}
