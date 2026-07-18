package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var errImageStudioMultipartSpool = errors.New("image studio multipart spool failed")

type ImageStudioEditMultipartSpool struct {
	Path          string
	Reader        io.ReadSeeker
	ContentType   string
	ContentLength int64

	file         *os.File
	root         *os.Root
	relativePath string
	cleanupOnce  sync.Once
	cleanupErr   error
}

type imageStudioEditMultipartSpoolBuilder interface {
	BuildEditMultipartSpool(inputs *OpenedEditInputs, payload []byte, upstreamModel string) (*ImageStudioEditMultipartSpool, error)
}

func imageStudioMultipartCleanupLogValue(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "path_error"
	}
	return "cleanup_error"
}

func (s *ImageStudioEditMultipartSpool) Cleanup() error {
	if s == nil {
		return nil
	}
	s.cleanupOnce.Do(func() {
		var errs []error
		if s.file != nil {
			errs = append(errs, s.file.Close())
		}
		if s.root != nil && s.relativePath != "" {
			if err := s.root.Remove(filepath.FromSlash(s.relativePath)); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
		if s.root != nil {
			errs = append(errs, s.root.Close())
		}
		s.cleanupErr = errors.Join(errs...)
	})
	return s.cleanupErr
}

func (s *ImageStudioInputStore) BuildEditMultipartSpool(inputs *OpenedEditInputs, payload []byte, upstreamModel string) (_ *ImageStudioEditMultipartSpool, retErr error) {
	if s == nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, errors.New("input store is not configured")))
	}
	uploadDir, err := imageStudioMultipartUploadDir(inputs)
	if err != nil {
		return nil, err
	}
	fields, err := imageStudioEditMultipartFields(payload, upstreamModel)
	if err != nil {
		return nil, err
	}
	root, err := s.openRoot()
	if err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, err))
	}
	cleanupRoot := true
	defer func() {
		if cleanupRoot {
			retErr = errors.Join(retErr, root.Close())
		}
	}()

	hasSymlink, err := rootPathContainsSymlink(root, uploadDir)
	if err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, err))
	}
	if hasSymlink {
		return nil, inputPathError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputPathInvalid))
	}
	info, err := root.Lstat(filepath.FromSlash(uploadDir))
	if err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, err))
	}
	if !info.IsDir() {
		return nil, inputPathError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputPathInvalid))
	}

	relativePath, writeFile, err := createImageStudioMultipartSpoolFile(root, uploadDir)
	if err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, err))
	}
	removeOnError := true
	defer func() {
		if !removeOnError {
			return
		}
		closeErr := writeFile.Close()
		removeErr := root.Remove(filepath.FromSlash(relativePath))
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		retErr = errors.Join(retErr, closeErr, removeErr)
	}()

	writer := multipart.NewWriter(writeFile)
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("write field %s: %w", field.name, err)))
		}
	}
	for i := range inputs.Images {
		if err := writeImageStudioMultipartFile(writer, "image", fmt.Sprintf("image-%02d%s", i+1, imageStudioExtension(inputs.Images[i].ContentType)), inputs.Images[i], i+1); err != nil {
			return nil, err
		}
	}
	if inputs.Mask != nil {
		if err := writeImageStudioMultipartFile(writer, "mask", "mask"+imageStudioExtension(inputs.Mask.ContentType), *inputs.Mask, 0); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("finalize multipart: %w", err)))
	}
	if err := writeFile.Sync(); err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("sync multipart: %w", err)))
	}
	if err := writeFile.Close(); err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("close multipart: %w", err)))
	}

	readFile, err := root.Open(filepath.FromSlash(relativePath))
	if err != nil {
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("reopen multipart: %w", err)))
	}
	readInfo, err := readFile.Stat()
	if err != nil {
		_ = readFile.Close()
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("stat multipart: %w", err)))
	}
	if !readInfo.Mode().IsRegular() {
		_ = readFile.Close()
		return nil, inputPathError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputPathInvalid))
	}
	if _, err := readFile.Seek(0, io.SeekStart); err != nil {
		_ = readFile.Close()
		return nil, inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("seek multipart: %w", err)))
	}

	removeOnError = false
	cleanupRoot = false
	return &ImageStudioEditMultipartSpool{
		Path:          filepath.Join(s.root, filepath.FromSlash(relativePath)),
		Reader:        readFile,
		ContentType:   writer.FormDataContentType(),
		ContentLength: readInfo.Size(),
		file:          readFile,
		root:          root,
		relativePath:  relativePath,
	}, nil
}

type imageStudioMultipartField struct {
	name  string
	value string
}

func imageStudioEditMultipartFields(payload []byte, upstreamModel string) ([]imageStudioMultipartField, error) {
	sanitized, err := sanitizeImageStudioEditPayload(payload)
	if err != nil {
		return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, err))
	}
	var source map[string]json.RawMessage
	if err := json.Unmarshal(sanitized, &source); err != nil {
		return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, err))
	}
	var originalModel string
	if raw, ok := source["model"]; ok {
		_ = json.Unmarshal(raw, &originalModel)
	}
	var prompt string
	if raw, ok := source["prompt"]; ok {
		_ = json.Unmarshal(raw, &prompt)
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	if strings.TrimSpace(originalModel) == "" || upstreamModel == "" {
		return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, errors.New("model is required")))
	}
	if strings.TrimSpace(prompt) == "" {
		return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, errors.New("prompt is required")))
	}

	fields := []imageStudioMultipartField{{name: "model", value: upstreamModel}}
	for _, name := range []string{
		"prompt", "size", "quality", "background", "style", "moderation",
		"input_fidelity", "output_format", "response_format",
	} {
		raw, ok := source[name]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("decode %s: %w", name, err)))
		}
		fields = append(fields, imageStudioMultipartField{name: name, value: value})
	}
	if raw, ok := source["output_compression"]; ok {
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, inputInvalidError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("decode output_compression: %w", err)))
		}
		fields = append(fields, imageStudioMultipartField{name: "output_compression", value: strconv.Itoa(value)})
	}
	return fields, nil
}

func imageStudioMultipartUploadDir(inputs *OpenedEditInputs) (string, error) {
	if inputs == nil || len(inputs.Images) < 1 || len(inputs.Images) > 4 {
		return "", inputInvalidError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputInvalid))
	}
	var uploadID string
	for i := range inputs.Images {
		if inputs.Images[i].File == nil || !supportedImageStudioContentType(inputs.Images[i].ContentType) {
			return "", inputInvalidError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputInvalid))
		}
		parts, err := validImageStudioRelativePath(inputs.Images[i].Path, fmt.Sprintf("image-%02d", i+1))
		if err != nil {
			return "", err
		}
		if uploadID == "" {
			uploadID = parts[1]
		} else if uploadID != parts[1] {
			return "", inputPathError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputPathInvalid))
		}
	}
	if inputs.Mask != nil {
		if inputs.Mask.File == nil || (inputs.Mask.ContentType != "image/png" && inputs.Mask.ContentType != "image/webp") {
			return "", inputInvalidError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputInvalid))
		}
		parts, err := validImageStudioRelativePath(inputs.Mask.Path, "mask")
		if err != nil {
			return "", err
		}
		if uploadID != parts[1] {
			return "", inputPathError(errors.Join(errImageStudioMultipartSpool, ErrImageStudioInputPathInvalid))
		}
	}
	return filepath.ToSlash(filepath.Join("inputs", uploadID)), nil
}

func createImageStudioMultipartSpoolFile(root *os.Root, uploadDir string) (string, *os.File, error) {
	for range 8 {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", nil, err
		}
		name := ".spool-" + hex.EncodeToString(random) + ".multipart"
		relativePath := filepath.ToSlash(filepath.Join(uploadDir, name))
		file, err := root.OpenFile(filepath.FromSlash(relativePath), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return relativePath, file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", nil, err
		}
	}
	return "", nil, errors.New("could not allocate multipart spool name")
}

func writeImageStudioMultipartFile(writer *multipart.Writer, fieldName, fileName string, input OpenedEditInput, imageIndex int) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": fieldName, "filename": fileName}))
	header.Set("Content-Type", input.ContentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("create %s part: %w", fieldName, err)))
	}
	if _, err := input.File.Seek(0, io.SeekStart); err != nil {
		label := fieldName
		if imageIndex > 0 {
			label = fmt.Sprintf("image %d", imageIndex)
		}
		return inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("seek %s: %w", label, err)))
	}
	if _, err := io.Copy(part, input.File); err != nil {
		label := fieldName
		if imageIndex > 0 {
			label = fmt.Sprintf("image %d", imageIndex)
		}
		return inputStorageError(errors.Join(errImageStudioMultipartSpool, fmt.Errorf("copy %s: %w", label, err)))
	}
	return nil
}
