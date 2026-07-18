package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestImageStudioInputStoreMaterializesLegacyInputsInOrderWithMask(t *testing.T) {
	first := imageStudioTestPNG(t, 4, 3, false)
	second := imageStudioTestJPEG(t, 5, 4)
	third := imageStudioTestPNG(t, 2, 2, false)
	fourth := imageStudioTestJPEG(t, 3, 3)
	mask := imageStudioTestPNG(t, 4, 3, true)
	tests := []struct {
		name   string
		images [][]byte
		mimes  []string
	}{
		{name: "one reference", images: [][]byte{first}, mimes: []string{"image/png"}},
		{name: "four references", images: [][]byte{first, second, third, fourth}, mimes: []string{"image/png", "image/jpeg", "image/png", "image/jpeg"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			urls := make([]string, len(tt.images))
			for i := range tt.images {
				urls[i] = imageStudioLegacyDataURL(tt.mimes[i], tt.images[i])
			}
			var maskURL *string
			if len(tt.images) == 1 {
				value := imageStudioLegacyDataURL("image/png", mask)
				maskURL = &value
			}

			staged, err := store.MaterializeLegacy(context.Background(), urls, maskURL)

			require.NoError(t, err)
			require.Len(t, staged.ImagePaths, len(tt.images))
			for i, path := range staged.ImagePaths {
				stored, readErr := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(path)))
				require.NoError(t, readErr)
				require.Equal(t, tt.images[i], stored)
			}
			if maskURL != nil {
				require.NotNil(t, staged.MaskPath)
				stored, readErr := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(*staged.MaskPath)))
				require.NoError(t, readErr)
				require.Equal(t, mask, stored)
			}
		})
	}
}

func TestImageStudioInputStoreMaterializeLegacyRejectsInvalidDataURLs(t *testing.T) {
	pngBytes := imageStudioTestPNG(t, 3, 2, false)
	jpegBytes := imageStudioTestJPEG(t, 3, 2)
	valid := imageStudioLegacyDataURL("image/png", pngBytes)
	encoded := base64.StdEncoding.EncodeToString(pngBytes)
	tests := []struct {
		name   string
		images []string
	}{
		{name: "zero references"},
		{name: "five references", images: []string{valid, valid, valid, valid, valid}},
		{name: "not a data URL", images: []string{base64.StdEncoding.EncodeToString(pngBytes)}},
		{name: "bad base64", images: []string{"data:image/png;base64,%%%"}},
		{name: "base64 with newline", images: []string{"data:image/png;base64," + encoded[:4] + "\n" + encoded[4:]}},
		{name: "unsupported MIME", images: []string{imageStudioLegacyDataURL("image/gif", pngBytes)}},
		{name: "extra media parameter", images: []string{"data:image/png;charset=utf-8;base64," + base64.StdEncoding.EncodeToString(pngBytes)}},
		{name: "MIME spoof", images: []string{imageStudioLegacyDataURL("image/png", jpegBytes)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)

			staged, err := store.MaterializeLegacy(context.Background(), tt.images, nil)

			require.Nil(t, staged)
			require.ErrorIs(t, err, ErrImageStudioLegacyInputInvalid)
			require.ErrorIs(t, err, ErrImageStudioInputInvalid)
			var inputErr *ImageStudioInputError
			require.ErrorAs(t, err, &inputErr)
			require.Equal(t, ImageStudioInputCodeLegacyInvalid, inputErr.Code)
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioInputStoreMaterializeLegacyRejectsOversizedEncodingBeforeDecode(t *testing.T) {
	pngBytes := imageStudioTestPNG(t, 3, 2, false)
	store := NewImageStudioInputStore(t.TempDir(), int64(len(pngBytes)-1))

	staged, err := store.MaterializeLegacy(context.Background(), []string{
		imageStudioLegacyDataURL("image/png", pngBytes),
	}, nil)

	require.Nil(t, staged)
	require.ErrorIs(t, err, ErrImageStudioLegacyInputInvalid)
	require.ErrorIs(t, err, ErrImageStudioInputTooLarge)
	require.Empty(t, imageStudioInputDirs(t, store.Root()))
}

func TestImageStudioInputStoreMaterializeLegacyHonorsContextAndRollsBack(t *testing.T) {
	pngBytes := imageStudioTestPNG(t, 3, 2, false)
	valid := imageStudioLegacyDataURL("image/png", pngBytes)

	t.Run("canceled before decode", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		store := NewImageStudioInputStore(t.TempDir(), 1<<20)

		staged, err := store.MaterializeLegacy(ctx, []string{valid}, nil)

		require.Nil(t, staged)
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
		require.Empty(t, imageStudioInputDirs(t, store.Root()))
	})

	t.Run("bad second reference", func(t *testing.T) {
		store := NewImageStudioInputStore(t.TempDir(), 1<<20)

		staged, err := store.MaterializeLegacy(context.Background(), []string{
			valid,
			"data:image/png;base64,%%%",
		}, nil)

		require.Nil(t, staged)
		require.ErrorIs(t, err, ErrImageStudioLegacyInputInvalid)
		require.Empty(t, imageStudioInputDirs(t, store.Root()))
	})
}

func imageStudioLegacyDataURL(contentType string, data []byte) string {
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func TestImageStudioInputStoreStagesReferenceCardinality(t *testing.T) {
	validPNG := imageStudioTestPNG(t, 3, 2, false)
	tests := []struct {
		name      string
		count     int
		wantError bool
	}{
		{name: "rejects zero images", count: 0, wantError: true},
		{name: "accepts one image", count: 1},
		{name: "accepts four images", count: 4},
		{name: "rejects five images", count: 5, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			images := make([]UploadedFile, tt.count)
			for i := range images {
				images[i] = UploadedFile{Reader: bytes.NewReader(validPNG), ContentType: "image/png"}
			}

			staged, err := store.StageEditInputs(context.Background(), images, nil)
			if tt.wantError {
				require.ErrorIs(t, err, ErrImageStudioInputInvalid)
				require.Nil(t, staged)
				require.Empty(t, imageStudioInputDirs(t, store.Root()))
				return
			}

			require.NoError(t, err)
			require.Len(t, staged.ImagePaths, tt.count)
			for i, path := range staged.ImagePaths {
				require.Equal(t, filepath.ToSlash(filepath.Join("inputs", staged.UploadID, "image-0"+string(rune('1'+i))+".png")), path)
				require.False(t, filepath.IsAbs(path))
			}
		})
	}
}

func TestImageStudioInputStoreRejectsOversizedFileAndRollsBack(t *testing.T) {
	validPNG := imageStudioTestPNG(t, 4, 4, false)
	store := NewImageStudioInputStore(t.TempDir(), int64(len(validPNG)-1))

	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
		Reader: bytes.NewReader(validPNG), ContentType: "image/png",
	}}, nil)

	require.ErrorIs(t, err, ErrImageStudioInputTooLarge)
	require.Nil(t, staged)
	require.Empty(t, imageStudioInputDirs(t, store.Root()))
}

func TestImageStudioInputStoreValidatesDeclaredAndDetectedMIME(t *testing.T) {
	pngBytes := imageStudioTestPNG(t, 3, 2, false)
	jpegBytes := imageStudioTestJPEG(t, 3, 2)
	corruptPNG := append([]byte(nil), pngBytes[:20]...)
	tests := []struct {
		name        string
		contentType string
		data        []byte
	}{
		{name: "declared MIME spoof", contentType: "image/png", data: jpegBytes},
		{name: "unsupported declared MIME", contentType: "application/octet-stream", data: pngBytes},
		{name: "detected image is not decodable", contentType: "image/png", data: corruptPNG},
		{name: "non image bytes", contentType: "image/png", data: []byte("not an image")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
				Reader: bytes.NewReader(tt.data), ContentType: tt.contentType,
			}}, nil)
			require.ErrorIs(t, err, ErrImageStudioInputInvalid)
			require.Nil(t, staged)
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioInputStoreClassifiesEmptyAndShortFilesAsInvalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "empty", data: nil},
		{name: "short", data: []byte("not an image")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)

			staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
				Reader: bytes.NewReader(tt.data), ContentType: "image/png",
			}}, nil)

			require.Nil(t, staged)
			var inputErr *ImageStudioInputError
			require.ErrorAs(t, err, &inputErr)
			require.Equal(t, ImageStudioInputCodeInvalid, inputErr.Code)
			require.ErrorIs(t, err, ErrImageStudioInputInvalid)
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioInputStoreRejectsOversizedPixelDimensionsBeforeDecode(t *testing.T) {
	tests := []struct {
		name   string
		width  uint32
		height uint32
	}{
		{name: "width", width: maxImageStudioInputDimension + 1, height: 1},
		{name: "height", width: 1, height: maxImageStudioInputDimension + 1},
		{name: "total pixels", width: 10_000, height: maxImageStudioInputPixels/10_000 + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			configOnlyPNG := imageStudioTestPNGConfigOnly(tt.width, tt.height)

			staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
				Reader: bytes.NewReader(configOnlyPNG), ContentType: "image/png",
			}}, nil)

			require.Nil(t, staged)
			require.ErrorIs(t, err, ErrImageStudioInputDimensionsTooLarge)
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioInputStoreValidatesMask(t *testing.T) {
	reference := imageStudioTestPNG(t, 4, 3, false)
	tests := []struct {
		name        string
		mask        []byte
		contentType string
	}{
		{name: "rejects opaque mask", mask: imageStudioTestPNG(t, 4, 3, false), contentType: "image/png"},
		{name: "rejects mismatched dimensions", mask: imageStudioTestPNG(t, 3, 3, true), contentType: "image/png"},
		{name: "rejects transparency incapable format", mask: imageStudioTestJPEG(t, 4, 3), contentType: "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
				Reader: bytes.NewReader(reference), ContentType: "image/png",
			}}, &UploadedFile{Reader: bytes.NewReader(tt.mask), ContentType: tt.contentType})
			require.ErrorIs(t, err, ErrImageStudioInputInvalid)
			require.Nil(t, staged)
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}

	t.Run("accepts transparent same-size PNG without conversion", func(t *testing.T) {
		store := NewImageStudioInputStore(t.TempDir(), 1<<20)
		mask := imageStudioTestPNG(t, 4, 3, true)
		staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
			Reader: bytes.NewReader(reference), ContentType: "image/png",
		}}, &UploadedFile{Reader: bytes.NewReader(mask), ContentType: "image/png"})
		require.NoError(t, err)
		require.NotNil(t, staged.MaskPath)
		stored, readErr := os.ReadFile(filepath.Join(store.Root(), filepath.FromSlash(*staged.MaskPath)))
		require.NoError(t, readErr)
		require.Equal(t, mask, stored)
	})
}

func TestImageStudioInputStoreStagesWithAtomicFinalize(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	reader := newImageStudioBlockingReader(imageStudioTestPNG(t, 8, 8, false))
	result := make(chan *StagedEditInputs, 1)
	errCh := make(chan error, 1)
	go func() {
		staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
			Reader: reader, ContentType: "image/png",
		}}, nil)
		result <- staged
		errCh <- err
	}()

	<-reader.firstRead
	dirs := imageStudioInputDirs(t, store.Root())
	require.Len(t, dirs, 1)
	entries, err := os.ReadDir(dirs[0])
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.True(t, strings.HasPrefix(entries[0].Name(), "."))
	require.True(t, strings.HasSuffix(entries[0].Name(), ".tmp"))
	close(reader.release)

	staged := <-result
	require.NoError(t, <-errCh)
	require.NotNil(t, staged)
	entries, err = os.ReadDir(dirs[0])
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "image-01.png", entries[0].Name())
}

func TestImageStudioInputStoreSyncsBeforeCloseAndRename(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	events := make([]string, 0, 3)
	store.syncTempFile = func(file *os.File) error {
		events = append(events, "sync")
		return file.Sync()
	}
	store.closeTempFile = func(file *os.File) error {
		events = append(events, "close")
		return file.Close()
	}
	store.renameTempFile = func(oldPath, newPath string) error {
		events = append(events, "rename")
		return os.Rename(oldPath, newPath)
	}

	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
		Reader: bytes.NewReader(imageStudioTestPNG(t, 2, 2, false)), ContentType: "image/png",
	}}, nil)

	require.NoError(t, err)
	require.NotNil(t, staged)
	require.Equal(t, []string{"sync", "close", "rename"}, events)
}

func TestImageStudioInputStoreRollsBackOnFinalizeFailure(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ImageStudioInputStore, error)
	}{
		{
			name: "sync",
			configure: func(store *ImageStudioInputStore, failure error) {
				store.syncTempFile = func(*os.File) error { return failure }
			},
		},
		{
			name: "close",
			configure: func(store *ImageStudioInputStore, failure error) {
				store.closeTempFile = func(file *os.File) error {
					if err := file.Close(); err != nil {
						return err
					}
					return failure
				}
			},
		},
		{
			name: "rename",
			configure: func(store *ImageStudioInputStore, failure error) {
				store.renameTempFile = func(string, string) error { return failure }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewImageStudioInputStore(t.TempDir(), 1<<20)
			failure := errors.New(tt.name + " failed")
			tt.configure(store, failure)

			staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
				Reader: bytes.NewReader(imageStudioTestPNG(t, 2, 2, false)), ContentType: "image/png",
			}}, nil)

			require.Nil(t, staged)
			var inputErr *ImageStudioInputError
			require.ErrorAs(t, err, &inputErr)
			require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
			require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
			require.ErrorContains(t, err, failure.Error())
			require.Empty(t, imageStudioInputDirs(t, store.Root()))
		})
	}
}

func TestImageStudioInputStoreRollsBackDirectoryOnMidStreamFailure(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	validPNG := imageStudioTestPNG(t, 3, 2, false)

	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{
		{Reader: bytes.NewReader(validPNG), ContentType: "image/png"},
		{Reader: io.MultiReader(bytes.NewReader(validPNG[:16]), &imageStudioFailingReader{}), ContentType: "image/png"},
	}, nil)

	require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
	var inputErr *ImageStudioInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
	require.Nil(t, staged)
	require.Empty(t, imageStudioInputDirs(t, store.Root()))
}

func TestImageStudioInputStoreReportsRollbackFailureAndPreservesBothCauses(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	stageFailure := errors.New("rename failed")
	cleanupFailure := errors.New("cleanup failed")
	store.renameTempFile = func(string, string) error { return stageFailure }
	store.removeAllInRoot = func(*os.Root, string) error { return cleanupFailure }

	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
		Reader: bytes.NewReader(imageStudioTestPNG(t, 2, 2, false)), ContentType: "image/png",
	}}, nil)

	require.Nil(t, staged)
	require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
	require.ErrorIs(t, err, stageFailure)
	require.ErrorIs(t, err, cleanupFailure)
	var inputErr *ImageStudioInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, ImageStudioInputCodeStorageUnavailable, inputErr.Code)
}

func TestImageStudioInputStoreChecksContextBeforeEachFileCopy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	validPNG := imageStudioTestPNG(t, 2, 2, false)
	secondReader := &imageStudioReadTrackingReader{err: errors.New("second reader must not be read")}
	store.renameTempFile = func(oldPath, newPath string) error {
		if err := os.Rename(oldPath, newPath); err != nil {
			return err
		}
		cancel()
		return nil
	}

	staged, err := store.StageEditInputs(ctx, []UploadedFile{
		{Reader: bytes.NewReader(validPNG), ContentType: "image/png"},
		{Reader: secondReader, ContentType: "image/png"},
	}, nil)

	require.Nil(t, staged)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, secondReader.read)
}

func TestImageStudioInputStoreErrorWrappersPreserveUnderlyingCause(t *testing.T) {
	cause := errors.New("underlying failure")

	require.ErrorIs(t, inputMissingError(cause), ErrImageStudioInputMissing)
	require.ErrorIs(t, inputMissingError(cause), cause)
	require.ErrorIs(t, inputStorageError(cause), ErrImageStudioInputStorageUnavailable)
	require.ErrorIs(t, inputStorageError(cause), cause)
}

func TestImageStudioInputStoreOpenRejectsUnsafePaths(t *testing.T) {
	dataDir := t.TempDir()
	store := NewImageStudioInputStore(dataDir, 1<<20)
	root := store.Root()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "inputs", "upload-static"), 0o700))
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, imageStudioTestPNG(t, 2, 2, false), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "inputs", "upload-static", "image-01.png")))

	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "absolute", path: outside},
		{name: "parent traversal", path: "inputs/../outside.png"},
		{name: "symlink escape", path: "inputs/upload-static/image-01.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opened, err := store.OpenInputs([]string{tt.path}, nil)
			require.ErrorIs(t, err, ErrImageStudioInputPathInvalid)
			require.Nil(t, opened)
		})
	}
}

func TestImageStudioInputStoreOpensStagedInputsInOrder(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	first := imageStudioTestPNG(t, 3, 2, false)
	second := imageStudioTestJPEG(t, 5, 4)
	mask := imageStudioTestPNG(t, 3, 2, true)
	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{
		{Reader: bytes.NewReader(first), ContentType: "image/png"},
		{Reader: bytes.NewReader(second), ContentType: "image/jpeg"},
	}, &UploadedFile{Reader: bytes.NewReader(mask), ContentType: "image/png"})
	require.NoError(t, err)

	opened, err := store.OpenInputs(staged.ImagePaths, staged.MaskPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, opened.Close()) })
	require.Len(t, opened.Images, 2)
	require.Equal(t, "image/png", opened.Images[0].ContentType)
	require.Equal(t, "image/jpeg", opened.Images[1].ContentType)
	require.NotNil(t, opened.Mask)
	actualFirst, err := io.ReadAll(opened.Images[0].File)
	require.NoError(t, err)
	require.Equal(t, first, actualFirst)
}

func TestImageStudioInputStoreOpenRejectsFileThatGrewPastLimit(t *testing.T) {
	validPNG := imageStudioTestPNG(t, 3, 2, false)
	store := NewImageStudioInputStore(t.TempDir(), int64(len(validPNG)))
	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
		Reader: bytes.NewReader(validPNG), ContentType: "image/png",
	}}, nil)
	require.NoError(t, err)
	storedPath := filepath.Join(store.Root(), filepath.FromSlash(staged.ImagePaths[0]))
	require.NoError(t, os.WriteFile(storedPath, append(validPNG, 0), 0o600))

	opened, err := store.OpenInputs(staged.ImagePaths, nil)

	require.ErrorIs(t, err, ErrImageStudioInputTooLarge)
	require.Nil(t, opened)
}

func TestImageStudioInputStoreRemoveIsRootConfinedAndIdempotent(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{{
		Reader: bytes.NewReader(imageStudioTestPNG(t, 2, 2, false)), ContentType: "image/png",
	}}, nil)
	require.NoError(t, err)
	uploadDir := filepath.Join(store.Root(), "inputs", staged.UploadID)

	require.NoError(t, store.RemoveInputs(staged.ImagePaths, staged.MaskPath))
	_, err = os.Stat(uploadDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.NoError(t, store.RemoveInputs(staged.ImagePaths, staged.MaskPath))

	neighbor := filepath.Join(filepath.Dir(store.Root()), "neighbor")
	require.NoError(t, os.MkdirAll(neighbor, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(neighbor, "keep"), []byte("keep"), 0o600))
	unsafePaths := []string{"", neighbor, "inputs/../neighbor/keep"}
	for _, path := range unsafePaths {
		err = store.RemoveInputs([]string{path}, nil)
		require.ErrorIs(t, err, ErrImageStudioInputPathInvalid)
	}
	_, err = os.Stat(filepath.Join(neighbor, "keep"))
	require.NoError(t, err)
}

func TestImageStudioInputStoreRemoveRejectsNonGeneratedUploadDirectory(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	uncontrolledDir := filepath.Join(store.Root(), "inputs", "not-server-generated")
	require.NoError(t, os.MkdirAll(uncontrolledDir, 0o700))
	keepPath := filepath.Join(uncontrolledDir, "image-01.png")
	require.NoError(t, os.WriteFile(keepPath, imageStudioTestPNG(t, 2, 2, false), 0o600))

	err := store.RemoveInputs([]string{"inputs/not-server-generated/image-01.png"}, nil)

	require.ErrorIs(t, err, ErrImageStudioInputPathInvalid)
	_, statErr := os.Stat(keepPath)
	require.NoError(t, statErr)
}

func TestImageStudioInputStoreRemoveRejectsUploadDirectorySymlink(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	inputsRoot := filepath.Join(store.Root(), "inputs")
	require.NoError(t, os.MkdirAll(inputsRoot, 0o700))
	targetDir := filepath.Join(t.TempDir(), "upload-target")
	require.NoError(t, os.MkdirAll(targetDir, 0o700))
	targetPath := filepath.Join(targetDir, "image-01.png")
	require.NoError(t, os.WriteFile(targetPath, imageStudioTestPNG(t, 2, 2, false), 0o600))
	symlinkDir := filepath.Join(inputsRoot, "upload-a")
	require.NoError(t, os.Symlink(targetDir, symlinkDir))

	for range 2 {
		err := store.RemoveInputs([]string{"inputs/upload-a/image-01.png"}, nil)

		require.ErrorIs(t, err, ErrImageStudioInputPathInvalid)
		info, statErr := os.Lstat(symlinkDir)
		require.NoError(t, statErr)
		require.NotZero(t, info.Mode()&os.ModeSymlink)
		require.FileExists(t, targetPath)
	}
}

func imageStudioTestPNGConfigOnly(width, height uint32) []byte {
	data := make([]byte, 8+4+4+13+4)
	copy(data, []byte("\x89PNG\r\n\x1a\n"))
	binary.BigEndian.PutUint32(data[8:12], 13)
	copy(data[12:16], "IHDR")
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	data[24] = 8
	data[25] = 6
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func imageStudioTestPNG(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent && x == 0 && y == 0 {
				alpha = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: 20, G: 80, B: 160, A: alpha})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func imageStudioTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 30, B: 10, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func imageStudioInputDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "inputs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	require.NoError(t, err)
	dirs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(root, "inputs", entry.Name()))
		}
	}
	return dirs
}

type imageStudioFailingReader struct{}

func (*imageStudioFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

type imageStudioReadTrackingReader struct {
	err  error
	read bool
}

func (r *imageStudioReadTrackingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, r.err
}

type imageStudioBlockingReader struct {
	data      []byte
	firstRead chan struct{}
	release   chan struct{}
	once      sync.Once
	position  int
}

func newImageStudioBlockingReader(data []byte) *imageStudioBlockingReader {
	return &imageStudioBlockingReader{
		data:      data,
		firstRead: make(chan struct{}),
		release:   make(chan struct{}),
	}
}

func (r *imageStudioBlockingReader) Read(p []byte) (int, error) {
	if r.position >= len(r.data) {
		return 0, io.EOF
	}
	if r.position > 0 {
		<-r.release
	}
	remaining := len(r.data) - r.position
	if r.position == 0 && remaining > 1 {
		remaining /= 2
	}
	if len(p) < remaining {
		remaining = len(p)
	}
	n := copy(p, r.data[r.position:r.position+remaining])
	r.position += n
	r.once.Do(func() { close(r.firstRead) })
	return n, nil
}

func TestImageStudioInputStoreCleanupOrphansAndStaleSpools(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	inputsRoot := filepath.Join(store.Root(), "inputs")
	require.NoError(t, os.MkdirAll(inputsRoot, 0o700))

	oldOrphan := "upload-aaaaaaaaaa"
	youngOrphan := "upload-bbbbbbbbbb"
	referenced := "upload-cccccccccc"
	running := "upload-dddddddddd"
	for _, name := range []string{oldOrphan, youngOrphan, referenced, running} {
		require.NoError(t, os.Mkdir(filepath.Join(inputsRoot, name), 0o700))
	}
	require.NoError(t, os.Chtimes(filepath.Join(inputsRoot, oldOrphan), now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(filepath.Join(inputsRoot, youngOrphan), now.Add(-10*time.Minute), now.Add(-10*time.Minute)))
	require.NoError(t, os.Chtimes(filepath.Join(inputsRoot, referenced), now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	require.NoError(t, os.Chtimes(filepath.Join(inputsRoot, running), now.Add(-2*time.Hour), now.Add(-2*time.Hour)))

	oldSpool := filepath.Join(inputsRoot, referenced, ".spool-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.multipart")
	youngSpool := filepath.Join(inputsRoot, referenced, ".spool-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.multipart")
	activeSpool := filepath.Join(inputsRoot, running, ".spool-cccccccccccccccccccccccccccccccc.multipart")
	for _, path := range []string{oldSpool, youngSpool, activeSpool} {
		require.NoError(t, os.WriteFile(path, []byte("spool"), 0o600))
	}
	require.NoError(t, os.Chtimes(oldSpool, now.Add(-20*time.Minute), now.Add(-20*time.Minute)))
	require.NoError(t, os.Chtimes(youngSpool, now.Add(-time.Minute), now.Add(-time.Minute)))
	require.NoError(t, os.Chtimes(activeSpool, now.Add(-20*time.Minute), now.Add(-20*time.Minute)))

	result, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{
		Now: now, OrphanGrace: time.Hour, SpoolGrace: 5 * time.Minute, Limit: 50,
		ReferencedDirs: map[string]struct{}{"inputs/" + referenced: {}, "inputs/" + running: {}},
		RunningDirs:    map[string]struct{}{"inputs/" + running: {}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.OrphanDirsDeleted)
	require.Equal(t, 1, result.StaleSpoolsDeleted)
	require.NoDirExists(t, filepath.Join(inputsRoot, oldOrphan))
	require.DirExists(t, filepath.Join(inputsRoot, youngOrphan))
	require.DirExists(t, filepath.Join(inputsRoot, referenced))
	require.DirExists(t, filepath.Join(inputsRoot, running))
	require.NoFileExists(t, oldSpool)
	require.FileExists(t, youngSpool)
	require.FileExists(t, activeSpool)
}

func TestImageStudioInputStoreCleanupOrphansSkipsSymlinksAndContinuesAfterFailure(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	inputsRoot := filepath.Join(store.Root(), "inputs")
	require.NoError(t, os.MkdirAll(inputsRoot, 0o700))
	first := "upload-aaaaaaaaaa"
	second := "upload-bbbbbbbbbb"
	for _, name := range []string{first, second} {
		require.NoError(t, os.Mkdir(filepath.Join(inputsRoot, name), 0o700))
		require.NoError(t, os.Chtimes(filepath.Join(inputsRoot, name), now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	}
	target := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.Mkdir(target, 0o700))
	require.NoError(t, os.Symlink(target, filepath.Join(inputsRoot, "upload-cccccccccc")))

	originalRemove := store.removeAllInRoot
	store.removeAllInRoot = func(root *os.Root, path string) error {
		if filepath.Base(path) == first {
			return errors.New("remove first failed")
		}
		return originalRemove(root, path)
	}

	result, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{Now: now, OrphanGrace: time.Hour, SpoolGrace: 5 * time.Minute, Limit: 50})

	require.ErrorContains(t, err, "remove first failed")
	require.Equal(t, 1, result.OrphanDirsDeleted)
	require.DirExists(t, filepath.Join(inputsRoot, first))
	require.NoDirExists(t, filepath.Join(inputsRoot, second))
	require.DirExists(t, target, "cleanup must never follow direct-child symlinks")
}

func TestImageStudioInputStoreCleanupStaleSpoolsContinuesAfterFailure(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	uploadDir := "upload-aaaaaaaaaa"
	absoluteDir := filepath.Join(store.Root(), "inputs", uploadDir)
	require.NoError(t, os.MkdirAll(absoluteDir, 0o700))
	firstName := ".spool-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.multipart"
	secondName := ".spool-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.multipart"
	for _, name := range []string{firstName, secondName} {
		path := filepath.Join(absoluteDir, name)
		require.NoError(t, os.WriteFile(path, []byte("spool"), 0o600))
		require.NoError(t, os.Chtimes(path, now.Add(-20*time.Minute), now.Add(-20*time.Minute)))
	}
	originalRemove := store.removeInRoot
	store.removeInRoot = func(root *os.Root, path string) error {
		if filepath.Base(path) == firstName {
			return errors.New("remove first spool failed")
		}
		return originalRemove(root, path)
	}

	result, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{
		Now: now, OrphanGrace: time.Hour, SpoolGrace: 5 * time.Minute, Limit: 50,
		ReferencedDirs: map[string]struct{}{"inputs/" + uploadDir: {}},
	})

	require.ErrorContains(t, err, "remove first spool failed")
	require.Equal(t, 1, result.StaleSpoolsDeleted)
	require.FileExists(t, filepath.Join(absoluteDir, firstName))
	require.NoFileExists(t, filepath.Join(absoluteDir, secondName))
}

func TestImageStudioInputStoreCleanupOrphansRotatesAcrossBoundedBatches(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	inputsRoot := filepath.Join(store.Root(), "inputs")
	require.NoError(t, os.MkdirAll(inputsRoot, 0o700))
	for _, name := range []string{"upload-aaaaaaaaaa", "upload-bbbbbbbbbb"} {
		path := filepath.Join(inputsRoot, name)
		require.NoError(t, os.Mkdir(path, 0o700))
		require.NoError(t, os.Chtimes(path, now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	}

	first, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{Now: now, OrphanGrace: time.Hour, Limit: 1})
	require.NoError(t, err)
	second, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{Now: now, OrphanGrace: time.Hour, Limit: 1})
	require.NoError(t, err)

	require.Equal(t, 1, first.Scanned)
	require.Equal(t, 1, second.Scanned)
	require.Equal(t, 2, first.OrphanDirsDeleted+second.OrphanDirsDeleted)
	require.Empty(t, imageStudioInputDirs(t, store.Root()))
}

func TestImageStudioInputStoreSpoolScanDoesNotSkipFetchedOrphan(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	inputsRoot := filepath.Join(store.Root(), "inputs")
	referenced := "upload-aaaaaaaaaa"
	orphan := "upload-bbbbbbbbbb"
	for _, name := range []string{referenced, orphan} {
		path := filepath.Join(inputsRoot, name)
		require.NoError(t, os.MkdirAll(path, 0o700))
		require.NoError(t, os.Chtimes(path, now.Add(-2*time.Hour), now.Add(-2*time.Hour)))
	}
	spool := filepath.Join(inputsRoot, referenced, ".spool-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.multipart")
	require.NoError(t, os.WriteFile(spool, []byte("spool"), 0o600))
	require.NoError(t, os.Chtimes(spool, now.Add(-20*time.Minute), now.Add(-20*time.Minute)))

	result, err := store.CleanupOrphans(ImageStudioInputCleanupOptions{
		Now: now, OrphanGrace: time.Hour, SpoolGrace: 5 * time.Minute, Limit: 2,
		ReferencedDirs: map[string]struct{}{"inputs/" + referenced: {}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.StaleSpoolsDeleted)
	require.Equal(t, 1, result.OrphanDirsDeleted)
	require.NoDirExists(t, filepath.Join(inputsRoot, orphan))
}
