package service

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	ImageStudioInputCodeInvalid            = "input_invalid"
	ImageStudioInputCodePathInvalid        = "input_path_invalid"
	ImageStudioInputCodeMissing            = "input_missing"
	ImageStudioInputCodeStorageUnavailable = "input_storage_unavailable"

	defaultImageStudioInputMaxFileBytes = int64(20 << 20)
	// These limits comfortably cover current GPT images and high-resolution uploads
	// while bounding a single decoded RGBA image to about 160 MiB.
	maxImageStudioInputDimension = 16_384
	maxImageStudioInputPixels    = 40_000_000
)

var (
	ErrImageStudioInputInvalid            = errors.New("image studio input is invalid")
	ErrImageStudioInputTooLarge           = fmt.Errorf("%w: file exceeds size limit", ErrImageStudioInputInvalid)
	ErrImageStudioInputDimensionsTooLarge = fmt.Errorf("%w: pixel dimensions exceed limit", ErrImageStudioInputInvalid)
	ErrImageStudioInputPathInvalid        = errors.New("image studio input path is invalid")
	ErrImageStudioInputMissing            = errors.New("image studio input is missing")
	ErrImageStudioInputStorageUnavailable = errors.New("image studio input storage is unavailable")
)

// ImageStudioInputError exposes a stable code without coupling storage errors to HTTP.
type ImageStudioInputError struct {
	Code string
	Err  error
}

func (e *ImageStudioInputError) Error() string {
	if e == nil || e.Err == nil {
		return "image studio input error"
	}
	return e.Err.Error()
}

func (e *ImageStudioInputError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type UploadedFile struct {
	Reader      io.Reader
	ContentType string
}

type StagedEditInputs struct {
	UploadID   string
	ImagePaths []string
	MaskPath   *string
}

type OpenedEditInput struct {
	File        *os.File
	Path        string
	ContentType string
}

type OpenedEditInputs struct {
	Images []OpenedEditInput
	Mask   *OpenedEditInput
}

type ImageStudioInputStorage interface {
	StageEditInputs(ctx context.Context, images []UploadedFile, mask *UploadedFile) (*StagedEditInputs, error)
	OpenInputs(paths []string, maskPath *string) (*OpenedEditInputs, error)
	RemoveInputs(paths []string, maskPath *string) error
}

func (o *OpenedEditInputs) Close() error {
	if o == nil {
		return nil
	}
	errs := make([]error, 0, len(o.Images)+1)
	for i := range o.Images {
		if o.Images[i].File != nil {
			errs = append(errs, o.Images[i].File.Close())
		}
	}
	if o.Mask != nil && o.Mask.File != nil {
		errs = append(errs, o.Mask.File.Close())
	}
	return errors.Join(errs...)
}

type ImageStudioInputStore struct {
	root            string
	maxFileBytes    int64
	syncTempFile    func(*os.File) error
	closeTempFile   func(*os.File) error
	renameTempFile  func(string, string) error
	removeAllInRoot func(*os.Root, string) error
}

func NewImageStudioInputStore(dataDir string, maxFileBytes int64) *ImageStudioInputStore {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		dataDir = "/app/data"
	}
	root, err := filepath.Abs(filepath.Join(dataDir, "image-studio"))
	if err != nil {
		root = filepath.Clean(filepath.Join(dataDir, "image-studio"))
	}
	if maxFileBytes <= 0 {
		maxFileBytes = defaultImageStudioInputMaxFileBytes
	}
	return &ImageStudioInputStore{
		root:            root,
		maxFileBytes:    maxFileBytes,
		syncTempFile:    (*os.File).Sync,
		closeTempFile:   (*os.File).Close,
		renameTempFile:  os.Rename,
		removeAllInRoot: (*os.Root).RemoveAll,
	}
}

func (s *ImageStudioInputStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *ImageStudioInputStore) StageEditInputs(ctx context.Context, images []UploadedFile, mask *UploadedFile) (_ *StagedEditInputs, retErr error) {
	if s == nil || len(images) < 1 || len(images) > 4 {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, inputStorageError(err)
	}
	inputsRoot := filepath.Join(s.root, "inputs")
	if err := os.MkdirAll(inputsRoot, 0o700); err != nil {
		return nil, inputStorageError(err)
	}
	uploadDir, err := os.MkdirTemp(inputsRoot, "upload-")
	if err != nil {
		return nil, inputStorageError(err)
	}
	uploadRelativeDir := filepath.ToSlash(filepath.Join("inputs", filepath.Base(uploadDir)))
	defer func() {
		if retErr != nil {
			if cleanupErr := s.removeRootPath(uploadRelativeDir); cleanupErr != nil {
				retErr = inputStorageError(errors.Join(retErr, cleanupErr))
			}
		}
	}()

	result := &StagedEditInputs{
		UploadID:   filepath.Base(uploadDir),
		ImagePaths: make([]string, 0, len(images)),
	}
	var firstBounds image.Rectangle
	for i := range images {
		validated, err := s.stageOne(ctx, uploadDir, fmt.Sprintf("image-%02d", i+1), images[i], false, image.Rectangle{})
		if err != nil {
			return nil, err
		}
		if i == 0 {
			firstBounds = validated.bounds
		}
		result.ImagePaths = append(result.ImagePaths, filepath.ToSlash(filepath.Join("inputs", result.UploadID, validated.finalName)))
	}
	if mask != nil {
		validated, err := s.stageOne(ctx, uploadDir, "mask", *mask, true, firstBounds)
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Join("inputs", result.UploadID, validated.finalName))
		result.MaskPath = &path
	}
	return result, nil
}

type validatedImageStudioInput struct {
	finalName   string
	contentType string
	bounds      image.Rectangle
}

func (s *ImageStudioInputStore) stageOne(ctx context.Context, uploadDir, baseName string, upload UploadedFile, mask bool, expectedBounds image.Rectangle) (*validatedImageStudioInput, error) {
	if upload.Reader == nil {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, inputStorageError(err)
	}
	declaredType, _, err := mime.ParseMediaType(strings.TrimSpace(upload.ContentType))
	if err != nil || !supportedImageStudioContentType(declaredType) {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	tempPath := filepath.Join(uploadDir, "."+baseName+".tmp")
	tempFile, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, inputStorageError(err)
	}
	written, copyErr := io.Copy(tempFile, io.LimitReader(upload.Reader, s.maxFileBytes+1))
	if copyErr != nil {
		_ = s.closeTempFile(tempFile)
		return nil, inputStorageError(copyErr)
	}
	if written > s.maxFileBytes {
		_ = s.closeTempFile(tempFile)
		return nil, inputInvalidError(ErrImageStudioInputTooLarge)
	}
	if err := ctx.Err(); err != nil {
		_ = s.closeTempFile(tempFile)
		return nil, inputStorageError(err)
	}

	validated, err := validateImageStudioFile(ctx, tempPath, declaredType, mask, expectedBounds)
	if err != nil {
		_ = s.closeTempFile(tempFile)
		return nil, err
	}
	syncErr := s.syncTempFile(tempFile)
	closeErr := s.closeTempFile(tempFile)
	if syncErr != nil {
		return nil, inputStorageError(syncErr)
	}
	if closeErr != nil {
		return nil, inputStorageError(closeErr)
	}
	validated.finalName = baseName + imageStudioExtension(validated.contentType)
	if err := s.renameTempFile(tempPath, filepath.Join(uploadDir, validated.finalName)); err != nil {
		return nil, inputStorageError(err)
	}
	return validated, nil
}

func (s *ImageStudioInputStore) OpenInputs(paths []string, maskPath *string) (retOpened *OpenedEditInputs, retErr error) {
	if s == nil || len(paths) < 1 || len(paths) > 4 {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, inputStorageError(err)
	}
	opened := &OpenedEditInputs{Images: make([]OpenedEditInput, 0, len(paths))}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			_ = opened.Close()
			retOpened = nil
			retErr = inputStorageError(errors.Join(retErr, closeErr))
		}
	}()
	fail := func(err error) (*OpenedEditInputs, error) {
		_ = opened.Close()
		return nil, err
	}

	var firstBounds image.Rectangle
	var uploadID string
	for i, path := range paths {
		parts, err := validImageStudioRelativePath(path, fmt.Sprintf("image-%02d", i+1))
		if err != nil {
			return fail(err)
		}
		if i == 0 {
			uploadID = parts[1]
		} else if parts[1] != uploadID {
			return fail(inputPathError(ErrImageStudioInputPathInvalid))
		}
		file, err := openImageStudioInput(root, path)
		if err != nil {
			return fail(err)
		}
		validated, err := validateOpenImageStudioFile(context.Background(), file, s.maxFileBytes, false, image.Rectangle{})
		if err != nil {
			_ = file.Close()
			return fail(err)
		}
		if i == 0 {
			firstBounds = validated.bounds
		}
		opened.Images = append(opened.Images, OpenedEditInput{File: file, Path: path, ContentType: validated.contentType})
	}
	if maskPath != nil {
		parts, err := validImageStudioRelativePath(*maskPath, "mask")
		if err != nil {
			return fail(err)
		}
		if parts[1] != uploadID {
			return fail(inputPathError(ErrImageStudioInputPathInvalid))
		}
		file, err := openImageStudioInput(root, *maskPath)
		if err != nil {
			return fail(err)
		}
		validated, err := validateOpenImageStudioFile(context.Background(), file, s.maxFileBytes, true, firstBounds)
		if err != nil {
			_ = file.Close()
			return fail(err)
		}
		opened.Mask = &OpenedEditInput{File: file, Path: *maskPath, ContentType: validated.contentType}
	}
	return opened, nil
}

func (s *ImageStudioInputStore) RemoveInputs(paths []string, maskPath *string) error {
	if s == nil {
		return inputPathError(ErrImageStudioInputPathInvalid)
	}
	if len(paths) == 0 && maskPath == nil {
		return nil
	}
	if len(paths) > 4 {
		return inputPathError(ErrImageStudioInputPathInvalid)
	}
	var uploadID string
	for i, path := range paths {
		parts, err := validImageStudioRelativePath(path, fmt.Sprintf("image-%02d", i+1))
		if err != nil {
			return err
		}
		if uploadID == "" {
			uploadID = parts[1]
		} else if parts[1] != uploadID {
			return inputPathError(ErrImageStudioInputPathInvalid)
		}
	}
	if maskPath != nil {
		parts, err := validImageStudioRelativePath(*maskPath, "mask")
		if err != nil {
			return err
		}
		if uploadID == "" {
			uploadID = parts[1]
		} else if parts[1] != uploadID {
			return inputPathError(ErrImageStudioInputPathInvalid)
		}
	}
	if uploadID == "" {
		return inputPathError(ErrImageStudioInputPathInvalid)
	}
	uploadDir := filepath.ToSlash(filepath.Join("inputs", uploadID))
	root, err := s.openRoot()
	if err != nil {
		return inputStorageError(err)
	}
	hasSymlink, err := rootPathContainsSymlink(root, uploadDir)
	if errors.Is(err, os.ErrNotExist) {
		return root.Close()
	}
	if err != nil {
		_ = root.Close()
		return inputStorageError(err)
	}
	if hasSymlink {
		_ = root.Close()
		return inputPathError(ErrImageStudioInputPathInvalid)
	}
	info, err := root.Lstat(filepath.FromSlash(uploadDir))
	if errors.Is(err, os.ErrNotExist) {
		return root.Close()
	}
	if err != nil {
		_ = root.Close()
		return inputStorageError(err)
	}
	if !info.IsDir() {
		_ = root.Close()
		return inputPathError(ErrImageStudioInputPathInvalid)
	}
	removeErr := s.removeAllInRoot(root, filepath.FromSlash(uploadDir))
	closeErr := root.Close()
	if removeErr != nil || closeErr != nil {
		return inputStorageError(errors.Join(removeErr, closeErr))
	}
	return nil
}

func validImageStudioRelativePath(path, expectedBase string) ([]string, error) {
	if path == "" || strings.TrimSpace(path) != path || filepath.IsAbs(path) || filepath.VolumeName(path) != "" || strings.Contains(path, "\\") {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "inputs" || !strings.HasPrefix(parts[1], "upload-") || len(parts[1]) == len("upload-") || parts[2] == "" {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	for _, part := range parts {
		if part == "." || part == ".." {
			return nil, inputPathError(ErrImageStudioInputPathInvalid)
		}
	}
	base := strings.TrimSuffix(parts[2], filepath.Ext(parts[2]))
	if base != expectedBase {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	ext := strings.ToLower(filepath.Ext(parts[2]))
	if expectedBase == "mask" {
		if ext != ".png" && ext != ".webp" {
			return nil, inputPathError(ErrImageStudioInputPathInvalid)
		}
	} else if ext != ".png" && ext != ".jpg" && ext != ".webp" {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	return parts, nil
}

func (s *ImageStudioInputStore) openRoot() (*os.Root, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(s.root)
}

func (s *ImageStudioInputStore) removeRootPath(relativePath string) (retErr error) {
	root, err := s.openRoot()
	if err != nil {
		return err
	}
	defer func() {
		retErr = errors.Join(retErr, root.Close())
	}()
	return s.removeAllInRoot(root, filepath.FromSlash(relativePath))
}

func openImageStudioInput(root *os.Root, relativePath string) (*os.File, error) {
	hasSymlink, err := rootPathContainsSymlink(root, relativePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, inputMissingError(err)
		}
		return nil, inputStorageError(err)
	}
	if hasSymlink {
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	file, err := root.Open(filepath.FromSlash(relativePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, inputMissingError(err)
		}
		return nil, inputStorageError(err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, inputStorageError(err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, inputPathError(ErrImageStudioInputPathInvalid)
	}
	return file, nil
}

func rootPathContainsSymlink(root *os.Root, relativePath string) (bool, error) {
	parts := strings.Split(filepath.ToSlash(relativePath), "/")
	for i := range parts {
		info, err := root.Lstat(filepath.FromSlash(strings.Join(parts[:i+1], "/")))
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func validateImageStudioFile(ctx context.Context, path, declaredType string, mask bool, expectedBounds image.Rectangle) (*validatedImageStudioInput, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, inputStorageError(err)
	}
	defer file.Close()
	validated, err := validateOpenImageStudioFile(ctx, file, 0, mask, expectedBounds)
	if err != nil {
		return nil, err
	}
	if validated.contentType != declaredType {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	return validated, nil
}

func validateOpenImageStudioFile(ctx context.Context, file *os.File, maxFileBytes int64, mask bool, expectedBounds image.Rectangle) (*validatedImageStudioInput, error) {
	if maxFileBytes > 0 {
		info, err := file.Stat()
		if err != nil {
			return nil, inputStorageError(err)
		}
		if info.Size() > maxFileBytes {
			return nil, inputInvalidError(ErrImageStudioInputTooLarge)
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, inputStorageError(err)
	}
	header := make([]byte, 512)
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, inputStorageError(err)
	}
	contentType := http.DetectContentType(header[:n])
	if !supportedImageStudioContentType(contentType) || (mask && contentType != "image/png" && contentType != "image/webp") {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, inputStorageError(err)
	}
	config, format, err := image.DecodeConfig(file)
	if err != nil || imageStudioContentTypeForFormat(format) != contentType {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxImageStudioInputDimension || config.Height > maxImageStudioInputDimension || config.Width > maxImageStudioInputPixels/config.Height {
		return nil, inputInvalidError(ErrImageStudioInputDimensionsTooLarge)
	}
	if err := ctx.Err(); err != nil {
		return nil, inputStorageError(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, inputStorageError(err)
	}
	decoded, format, err := image.Decode(file)
	if err != nil || imageStudioContentTypeForFormat(format) != contentType {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height || bounds.Empty() {
		return nil, inputInvalidError(ErrImageStudioInputInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, inputStorageError(err)
	}
	if mask {
		hasAlpha, err := imageStudioHasUsableAlpha(ctx, decoded)
		if err != nil {
			return nil, inputStorageError(err)
		}
		if bounds.Dx() != expectedBounds.Dx() || bounds.Dy() != expectedBounds.Dy() || !hasAlpha {
			return nil, inputInvalidError(ErrImageStudioInputInvalid)
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, inputStorageError(err)
	}
	return &validatedImageStudioInput{contentType: contentType, bounds: bounds}, nil
}

func imageStudioHasUsableAlpha(ctx context.Context, img image.Image) (bool, error) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := img.At(x, y).RGBA()
			if alpha < 0xffff {
				return true, nil
			}
		}
	}
	return false, nil
}

func supportedImageStudioContentType(contentType string) bool {
	switch contentType {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func imageStudioContentTypeForFormat(format string) string {
	switch format {
	case "png":
		return "image/png"
	case "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func imageStudioExtension(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

func inputInvalidError(err error) error {
	return &ImageStudioInputError{Code: ImageStudioInputCodeInvalid, Err: err}
}

func inputPathError(err error) error {
	return &ImageStudioInputError{Code: ImageStudioInputCodePathInvalid, Err: err}
}

func inputMissingError(err error) error {
	return &ImageStudioInputError{Code: ImageStudioInputCodeMissing, Err: fmt.Errorf("%w: %w", ErrImageStudioInputMissing, err)}
}

func inputStorageError(err error) error {
	return &ImageStudioInputError{Code: ImageStudioInputCodeStorageUnavailable, Err: fmt.Errorf("%w: %w", ErrImageStudioInputStorageUnavailable, err)}
}
