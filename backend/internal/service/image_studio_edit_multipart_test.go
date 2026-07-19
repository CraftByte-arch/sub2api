package service

import (
	"io"
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageStudioEditMultipartSpoolPreservesInputsAndMetadata(t *testing.T) {
	maskBytes := imageStudioTestPNG(t, 2, 2, true)

	for _, tt := range []struct {
		name     string
		count    int
		withMask bool
	}{
		{name: "one image", count: 1},
		{name: "four images and mask", count: 4, withMask: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			images := make([][]byte, tt.count)
			for i := range images {
				images[i] = imageStudioTestPNG(t, 2+i, 2, false)
			}
			var mask []byte
			if tt.withMask {
				mask = maskBytes
			}
			store, staged := stageImageStudioWorkerInputs(t, images, mask, 1<<20)
			opened, err := store.OpenInputs(staged.ImagePaths, staged.MaskPath)
			require.NoError(t, err)
			t.Cleanup(func() { _ = opened.Close() })

			payload := []byte(`{
				"model":"image-alias",
				"prompt":"replace the background",
				"size":"1536x1024",
				"quality":"high",
				"background":"transparent",
				"style":"vivid",
				"moderation":"low",
				"input_fidelity":"high",
				"output_format":"webp",
				"response_format":"b64_json",
				"output_compression":73,
				"ignored":"must-not-forward"
			}`)
			spool, err := store.BuildEditMultipartSpool(opened, payload, "gpt-image-2")
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, spool.Cleanup()) })

			require.Contains(t, filepath.Base(spool.Path), ".spool-")
			require.Equal(t, ".multipart", filepath.Ext(spool.Path))
			require.Equal(t, filepath.Dir(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0]))), filepath.Dir(spool.Path))
			info, err := os.Stat(spool.Path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			require.Equal(t, info.Size(), spool.ContentLength)

			mediaType, params, err := mime.ParseMediaType(spool.ContentType)
			require.NoError(t, err)
			require.Equal(t, "multipart/form-data", mediaType)
			require.NotEmpty(t, params["boundary"])
			parts := multipart.NewReader(spool.Reader, params["boundary"])
			fields := map[string]string{}
			var gotImages [][]byte
			var gotMask []byte
			for {
				part, err := parts.NextPart()
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				data, err := io.ReadAll(part)
				require.NoError(t, err)
				switch part.FormName() {
				case "image":
					require.Equal(t, "image-"+leftPadImageStudioIndex(len(gotImages)+1)+".png", part.FileName())
					require.Equal(t, "image/png", part.Header.Get("Content-Type"))
					gotImages = append(gotImages, data)
				case "mask":
					require.Equal(t, "mask.png", part.FileName())
					require.Equal(t, "image/png", part.Header.Get("Content-Type"))
					gotMask = data
				default:
					require.Empty(t, part.FileName())
					fields[part.FormName()] = string(data)
				}
			}

			require.Equal(t, images, gotImages)
			if tt.withMask {
				require.Equal(t, maskBytes, gotMask)
			} else {
				require.Nil(t, gotMask)
			}
			require.Equal(t, map[string]string{
				"model":              "gpt-image-2",
				"prompt":             "replace the background",
				"size":               "1536x1024",
				"quality":            "high",
				"background":         "transparent",
				"style":              "vivid",
				"moderation":         "low",
				"input_fidelity":     "high",
				"output_format":      "webp",
				"response_format":    "b64_json",
				"output_compression": strconv.Itoa(73),
			}, fields)
		})
	}
}

func TestImageStudioEditMultipartSpoolCleansUpBuildFailure(t *testing.T) {
	imageBytes := imageStudioTestPNG(t, 2, 2, false)
	store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageBytes}, nil, 1<<20)
	opened, err := store.OpenInputs(staged.ImagePaths, nil)
	require.NoError(t, err)
	require.NoError(t, opened.Images[0].File.Close())

	spool, err := store.BuildEditMultipartSpool(opened, []byte(`{"model":"gpt-image-2","prompt":"edit"}`), "gpt-image-2")
	require.Error(t, err)
	require.Nil(t, spool)
	require.ErrorContains(t, err, "seek image 1")

	uploadDir := filepath.Dir(filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0])))
	matches, globErr := filepath.Glob(filepath.Join(uploadDir, ".spool-*.multipart"))
	require.NoError(t, globErr)
	require.Empty(t, matches)
}

func TestImageStudioEditMultipartSpoolRejectsPollutedUploadPath(t *testing.T) {
	imageBytes := imageStudioTestPNG(t, 2, 2, false)
	store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageBytes}, nil, 1<<20)
	opened, err := store.OpenInputs(staged.ImagePaths, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.Close() })
	opened.Images[0].Path = "../../outside/image-01.png"

	spool, err := store.BuildEditMultipartSpool(opened, []byte(`{"model":"gpt-image-2","prompt":"edit"}`), "gpt-image-2")
	require.Error(t, err)
	require.Nil(t, spool)
	require.ErrorIs(t, err, ErrImageStudioInputPathInvalid)
}

func leftPadImageStudioIndex(index int) string {
	if index < 10 {
		return "0" + strconv.Itoa(index)
	}
	return strconv.Itoa(index)
}

func TestImageStudioEditMultipartSpoolRequiresModelAndPrompt(t *testing.T) {
	imageBytes := imageStudioTestPNG(t, 2, 2, false)
	for _, payload := range [][]byte{
		[]byte(`{"prompt":"edit"}`),
		[]byte(`{"model":"gpt-image-2"}`),
	} {
		store, staged := stageImageStudioWorkerInputs(t, [][]byte{imageBytes}, nil, 1<<20)
		opened, err := store.OpenInputs(staged.ImagePaths, nil)
		require.NoError(t, err)
		spool, err := store.BuildEditMultipartSpool(opened, payload, "")
		require.Error(t, err)
		require.Nil(t, spool)
		require.NoError(t, opened.Close())
	}
}

func TestImageStudioMultipartCleanupLogValueDoesNotExposeSpoolPath(t *testing.T) {
	secretPath := "inputs/upload-secret/.spool-secret.multipart"
	value := imageStudioMultipartCleanupLogValue(&os.PathError{Op: "remove", Path: secretPath, Err: os.ErrPermission})
	require.NotContains(t, value, secretPath)
	require.NotContains(t, value, ".spool-secret.multipart")
	require.Equal(t, "path_error", value)
}
