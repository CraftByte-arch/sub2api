package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
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
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

const (
	ImageStudioInputCodeInvalid            = "input_invalid"
	ImageStudioInputCodeExpired            = "input_expired"
	ImageStudioInputCodeLegacyInvalid      = "legacy_input_invalid"
	ImageStudioInputCodePathInvalid        = "input_path_invalid"
	ImageStudioInputCodeMissing            = "input_missing"
	ImageStudioInputCodeStorageUnavailable = "input_storage_unavailable"

	defaultImageStudioInputMaxFileBytes = int64(20 << 20)
	// These limits comfortably cover current GPT images and high-resolution uploads
	// while bounding a single decoded RGBA image to about 160 MiB.
	maxImageStudioInputDimension   = 16_384
	maxImageStudioInputPixels      = 40_000_000
	defaultImageStudioCleanupLimit = 50
	maxImageStudioCleanupLimit     = 500
	maxImageStudioSpoolsPerDirScan = 10
)

var (
	ErrImageStudioInputInvalid            = errors.New("image studio input is invalid")
	ErrImageStudioInputExpired            = errors.New("image studio input has expired")
	ErrImageStudioLegacyInputInvalid      = errors.New("legacy image studio input is invalid")
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
	MaterializeLegacy(ctx context.Context, images []string, mask *string) (*StagedEditInputs, error)
	OpenInputs(paths []string, maskPath *string) (*OpenedEditInputs, error)
	RemoveInputs(paths []string, maskPath *string) error
}

type ImageStudioInputStorageProber interface {
	Probe(ctx context.Context) error
}

type ImageStudioInputCleanupOptions struct {
	Now            time.Time
	OrphanGrace    time.Duration
	SpoolGrace     time.Duration
	Limit          int
	ReferencedDirs map[string]struct{}
	RunningDirs    map[string]struct{}
}

type ImageStudioInputCleanupResult struct {
	Scanned            int
	OrphanDirsDeleted  int
	StaleSpoolsDeleted int
}

type imageStudioInputOrphanCleaner interface {
	CleanupOrphans(options ImageStudioInputCleanupOptions) (ImageStudioInputCleanupResult, error)
}

func (s *ImageStudioInputStore) MaterializeLegacy(ctx context.Context, images []string, mask *string) (*StagedEditInputs, error) {
	if s == nil || len(images) < 1 || len(images) > 4 {
		return nil, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	if err := ctx.Err(); err != nil {
		return nil, inputStorageError(err)
	}
	uploads := make([]UploadedFile, len(images))
	for i := range images {
		upload, err := s.legacyDataURLUpload(images[i])
		if err != nil {
			return nil, err
		}
		uploads[i] = upload
	}
	var maskUpload *UploadedFile
	if mask != nil {
		upload, err := s.legacyDataURLUpload(*mask)
		if err != nil {
			return nil, err
		}
		maskUpload = &upload
	}
	staged, err := s.StageEditInputs(ctx, uploads, maskUpload)
	if err != nil && errors.Is(err, ErrImageStudioInputInvalid) {
		return nil, legacyInputInvalidError(err)
	}
	return staged, err
}

var errImageStudioLegacyBase64Invalid = errors.New("legacy image studio base64 is invalid")

func (s *ImageStudioInputStore) legacyDataURLUpload(value string) (UploadedFile, error) {
	header, encoded, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(header, "data:") || !strings.HasSuffix(header, ";base64") {
		return UploadedFile{}, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	contentType := strings.TrimPrefix(strings.TrimSuffix(header, ";base64"), "data:")
	if !supportedImageStudioContentType(contentType) || header != "data:"+contentType+";base64" {
		return UploadedFile{}, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	if int64(len(encoded)) > maxImageStudioLegacyEncodedBytes(s.maxFileBytes) {
		return UploadedFile{}, legacyInputInvalidError(ErrImageStudioInputTooLarge)
	}
	if strings.ContainsAny(encoded, "\r\n") {
		return UploadedFile{}, legacyInputInvalidError(ErrImageStudioInputInvalid)
	}
	return UploadedFile{
		Reader:      &imageStudioLegacyBase64Reader{reader: base64.NewDecoder(base64.StdEncoding.Strict(), strings.NewReader(encoded))},
		ContentType: contentType,
	}, nil
}

func maxImageStudioLegacyEncodedBytes(decodedBytes int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if decodedBytes > maxInt64-2 {
		return maxInt64
	}
	groups := (decodedBytes + 2) / 3
	if groups > maxInt64/4 {
		return maxInt64
	}
	return groups * 4
}

type imageStudioLegacyBase64Reader struct {
	reader io.Reader
}

func (r *imageStudioLegacyBase64Reader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		return n, fmt.Errorf("%w: %v", errImageStudioLegacyBase64Invalid, err)
	}
	return n, err
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
	removeInRoot    func(*os.Root, string) error
	openProbeFile   func(*os.Root, string, int, os.FileMode) (*os.File, error)
	writeProbeFile  func(*os.File, []byte) (int, error)
	syncProbeFile   func(*os.File) error
	closeProbeFile  func(*os.File) error
	openProbeRead   func(*os.Root, string) (*os.File, error)
	readProbeFile   func(*os.File) ([]byte, error)
	removeProbeFile func(*os.Root, string) error
	probeMu         sync.Mutex
	cleanupMu       sync.Mutex
	cleanupDir      *os.File
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
		removeInRoot:    (*os.Root).Remove,
		openProbeFile:   (*os.Root).OpenFile,
		writeProbeFile:  (*os.File).Write,
		syncProbeFile:   (*os.File).Sync,
		closeProbeFile:  (*os.File).Close,
		openProbeRead:   (*os.Root).Open,
		readProbeFile:   func(file *os.File) ([]byte, error) { return io.ReadAll(file) },
		removeProbeFile: (*os.Root).Remove,
	}
}

func (s *ImageStudioInputStore) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *ImageStudioInputStore) Probe(ctx context.Context) (retErr error) {
	if s == nil {
		return inputStorageError(errors.New("image studio input store is nil"))
	}
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return inputStorageError(err)
	}

	root, err := s.openRoot()
	if err != nil {
		return inputStorageError(err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = inputStorageError(errors.Join(retErr, closeErr))
		}
	}()

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return inputStorageError(err)
	}
	name := ".storage-probe-" + hex.EncodeToString(random)
	payload := []byte("image-studio-input-storage-probe-v1")
	file, err := s.openProbeFile(root, name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return inputStorageError(err)
	}
	created := true
	defer func() {
		if file != nil {
			retErr = errors.Join(retErr, s.closeProbeFile(file))
		}
		if created {
			retErr = errors.Join(retErr, s.removeProbeFile(root, name))
		}
		if retErr != nil && !errors.Is(retErr, ErrImageStudioInputStorageUnavailable) {
			retErr = inputStorageError(retErr)
		}
	}()

	written, err := s.writeProbeFile(file, payload)
	if err != nil {
		return err
	}
	if written != len(payload) {
		return io.ErrShortWrite
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.syncProbeFile(file); err != nil {
		return err
	}
	if err := s.closeProbeFile(file); err != nil {
		file = nil
		return err
	}
	file = nil
	if err := ctx.Err(); err != nil {
		return err
	}

	file, err = s.openProbeRead(root, name)
	if err != nil {
		return err
	}
	read, err := s.readProbeFile(file)
	if err != nil {
		return err
	}
	if !bytes.Equal(read, payload) {
		return errors.New("probe content mismatch")
	}
	if err := s.closeProbeFile(file); err != nil {
		file = nil
		return err
	}
	file = nil
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.removeProbeFile(root, name); err != nil {
		return err
	}
	created = false
	return nil
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
		if errors.Is(copyErr, errImageStudioLegacyBase64Invalid) {
			return nil, inputInvalidError(errors.Join(ErrImageStudioInputInvalid, copyErr))
		}
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
		if imageStudioExtension(validated.contentType) != strings.ToLower(filepath.Ext(path)) {
			_ = file.Close()
			return fail(inputInvalidError(ErrImageStudioInputInvalid))
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
		if imageStudioExtension(validated.contentType) != strings.ToLower(filepath.Ext(*maskPath)) {
			_ = file.Close()
			return fail(inputInvalidError(ErrImageStudioInputInvalid))
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

func (s *ImageStudioInputStore) CleanupOrphans(options ImageStudioInputCleanupOptions) (result ImageStudioInputCleanupResult, retErr error) {
	if s == nil {
		return result, inputStorageError(errors.New("image studio input store is nil"))
	}
	if options.Now.IsZero() {
		options.Now = time.Now()
	}
	if options.OrphanGrace <= 0 {
		options.OrphanGrace = time.Hour
	}
	if options.SpoolGrace <= 0 {
		options.SpoolGrace = 10 * time.Minute
	}
	options.Limit = normalizeImageStudioCleanupLimit(options.Limit)
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()

	root, err := s.openRoot()
	if err != nil {
		return result, inputStorageError(err)
	}
	defer func() { retErr = errors.Join(retErr, root.Close()) }()
	entries, readErr := s.nextImageStudioCleanupEntries(root, options.Limit)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return result, inputStorageError(readErr)
	}

	errs := make([]error, 0)
	for _, entry := range entries {
		result.Scanned++
		name := entry.Name()
		if !validImageStudioUploadDirName(name) {
			continue
		}
		relativeDir := filepath.ToSlash(filepath.Join("inputs", name))
		info, err := root.Lstat(filepath.FromSlash(relativeDir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		if _, running := options.RunningDirs[relativeDir]; running {
			continue
		}
		if _, referenced := options.ReferencedDirs[relativeDir]; !referenced && !info.ModTime().After(options.Now.Add(-options.OrphanGrace)) {
			if err := s.removeAllInRoot(root, filepath.FromSlash(relativeDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			result.OrphanDirsDeleted++
			continue
		}
		spoolResult, spoolErr := s.cleanupStaleImageStudioSpools(root, relativeDir, options, maxImageStudioSpoolsPerDirScan)
		result.Scanned += spoolResult.Scanned
		result.StaleSpoolsDeleted += spoolResult.StaleSpoolsDeleted
		if spoolErr != nil {
			errs = append(errs, spoolErr)
		}
	}
	if err := errors.Join(errs...); err != nil {
		return result, inputStorageError(err)
	}
	return result, nil
}

func (s *ImageStudioInputStore) nextImageStudioCleanupEntries(root *os.Root, limit int) ([]os.DirEntry, error) {
	if s.cleanupDir == nil {
		dir, err := root.Open("inputs")
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		s.cleanupDir = dir
	}
	entries, err := s.cleanupDir.ReadDir(limit)
	if errors.Is(err, io.EOF) || len(entries) < limit {
		closeErr := s.cleanupDir.Close()
		s.cleanupDir = nil
		if errors.Is(err, io.EOF) {
			err = nil
		}
		return entries, errors.Join(err, closeErr)
	}
	if err != nil {
		closeErr := s.cleanupDir.Close()
		s.cleanupDir = nil
		return nil, errors.Join(err, closeErr)
	}
	return entries, nil
}

func (s *ImageStudioInputStore) cleanupStaleImageStudioSpools(root *os.Root, relativeDir string, options ImageStudioInputCleanupOptions, limit int) (result ImageStudioInputCleanupResult, retErr error) {
	if limit <= 0 {
		return result, nil
	}
	dir, err := root.Open(filepath.FromSlash(relativeDir))
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer func() { retErr = errors.Join(retErr, dir.Close()) }()
	entries, readErr := dir.ReadDir(limit)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return result, readErr
	}
	errs := make([]error, 0)
	for _, entry := range entries {
		result.Scanned++
		if !validImageStudioSpoolName(entry.Name()) {
			continue
		}
		relativePath := filepath.ToSlash(filepath.Join(relativeDir, entry.Name()))
		info, err := root.Lstat(filepath.FromSlash(relativePath))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.ModTime().After(options.Now.Add(-options.SpoolGrace)) {
			continue
		}
		if err := s.removeInRoot(root, filepath.FromSlash(relativePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		result.StaleSpoolsDeleted++
	}
	return result, errors.Join(errs...)
}

func normalizeImageStudioCleanupLimit(limit int) int {
	if limit <= 0 {
		return defaultImageStudioCleanupLimit
	}
	if limit > maxImageStudioCleanupLimit {
		return maxImageStudioCleanupLimit
	}
	return limit
}

func validImageStudioUploadDirName(name string) bool {
	const prefix = "upload-"
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	for _, char := range name[len(prefix):] {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func validImageStudioSpoolName(name string) bool {
	const prefix = ".spool-"
	const suffix = ".multipart"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if len(id) != 32 {
		return false
	}
	for _, char := range id {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
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

func inputExpiredError() error {
	return &ImageStudioInputError{Code: ImageStudioInputCodeExpired, Err: ErrImageStudioInputExpired}
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

func legacyInputInvalidError(err error) error {
	return &ImageStudioInputError{Code: ImageStudioInputCodeLegacyInvalid, Err: errors.Join(ErrImageStudioLegacyInputInvalid, err)}
}
