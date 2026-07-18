package service

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/stretchr/testify/require"
)

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

func TestImageStudioInputStoreRollsBackDirectoryOnMidStreamFailure(t *testing.T) {
	store := NewImageStudioInputStore(t.TempDir(), 1<<20)
	validPNG := imageStudioTestPNG(t, 3, 2, false)

	staged, err := store.StageEditInputs(context.Background(), []UploadedFile{
		{Reader: bytes.NewReader(validPNG), ContentType: "image/png"},
		{Reader: io.MultiReader(bytes.NewReader(validPNG[:16]), &imageStudioFailingReader{}), ContentType: "image/png"},
	}, nil)

	require.ErrorIs(t, err, ErrImageStudioInputStorageUnavailable)
	require.Nil(t, staged)
	require.Empty(t, imageStudioInputDirs(t, store.Root()))
}

func TestImageStudioInputStoreOpenRejectsUnsafePaths(t *testing.T) {
	dataDir := t.TempDir()
	store := NewImageStudioInputStore(dataDir, 1<<20)
	root := store.Root()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "inputs", "upload"), 0o700))
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, imageStudioTestPNG(t, 2, 2, false), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "inputs", "upload", "image-01.png")))

	tests := []struct {
		name string
		path string
	}{
		{name: "empty", path: ""},
		{name: "absolute", path: outside},
		{name: "parent traversal", path: "inputs/../outside.png"},
		{name: "symlink escape", path: "inputs/upload/image-01.png"},
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
