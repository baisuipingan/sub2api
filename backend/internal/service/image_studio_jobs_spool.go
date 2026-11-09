package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func studioImageNeedsDownload(item map[string]json.RawMessage) bool {
	var b64, url string
	_ = json.Unmarshal(item["b64_json"], &b64)
	_ = json.Unmarshal(item["url"], &url)
	return strings.TrimSpace(b64) == "" && strings.TrimSpace(url) != ""
}

func (s *StudioJobService) studioImageSpoolPath(claim StudioJobClaim, index int) string {
	return s.studioSpoolPath(claim) + ".image-" + strconv.Itoa(index)
}

// URL-only responses are not durable images: the supplier link can expire
// while R2 is unavailable. Download every original into the private persistent
// spool before attempting any object-store write or post-processing.
func (s *StudioJobService) cacheStudioURLImages(ctx context.Context, claim StudioJobClaim, response []byte, uploader *ImageResultUploader) error {
	images, err := studioResponseImages(claim.Spec.Protocol, response)
	if err != nil {
		return err
	}
	for index, item := range images {
		if !studioImageNeedsDownload(item) {
			continue // Base64 originals are already durable in the response JSON.
		}
		path := s.studioImageSpoolPath(claim, index)
		if info, err := os.Stat(path); err == nil {
			if info.Size() <= 0 || info.Size() > StudioMaxOutputImageBytes {
				return ErrGalleryInvalid
			}
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, _, err := uploader.fetchImageBytes(ctx, item)
		if err != nil {
			return err
		}
		if len(data) == 0 || len(data) > StudioMaxOutputImageBytes {
			return ErrGalleryInvalid
		}
		if err := s.writeStudioImageSpool(path, int64(len(data)), data); err != nil {
			return err
		}
	}
	return nil
}

func (s *StudioJobService) writeStudioImageSpool(path string, size int64, data []byte) error {
	// One executor writes sidecars, while cleanup can run concurrently. Reserve
	// capacity before writing; include response files and other retained images.
	s.spoolMu.Lock()
	if s.spoolUsed+size > studioJobMaxSpoolBytes {
		s.spoolMu.Unlock()
		return errors.New("studio original image spool capacity exhausted")
	}
	entries, err := os.ReadDir(s.spoolDir)
	if err != nil {
		s.spoolMu.Unlock()
		return err
	}
	name := filepath.Base(path)
	last := strings.LastIndex(name, "__")
	if last < 0 {
		s.spoolMu.Unlock()
		return ErrStudioJobInvalid
	}
	var taskBytes int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), name[:last+2]) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			s.spoolMu.Unlock()
			return err
		}
		taskBytes += info.Size()
	}
	if taskBytes+size > studioJobMaxTaskSpoolBytes {
		s.spoolMu.Unlock()
		return errors.New("studio task original image spool capacity exhausted")
	}
	s.spoolUsed += size
	s.spoolMu.Unlock()
	committed := false
	defer func() {
		if !committed {
			s.spoolMu.Lock()
			s.spoolUsed -= size
			s.spoolMu.Unlock()
		}
	}()
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	committed = true
	return syncStudioDirectory(s.spoolDir)
}

func (s *StudioJobService) studioImageBytes(ctx context.Context, claim StudioJobClaim, index int, item map[string]json.RawMessage, uploader *ImageResultUploader) ([]byte, string, error) {
	if !studioImageNeedsDownload(item) {
		return uploader.fetchImageBytes(ctx, item)
	}
	file, err := os.Open(s.studioImageSpoolPath(claim, index))
	if err != nil {
		return nil, "", fmt.Errorf("read durable image original: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > StudioMaxOutputImageBytes {
		return nil, "", ErrGalleryInvalid
	}
	data, err := io.ReadAll(io.LimitReader(file, StudioMaxOutputImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 || len(data) > StudioMaxOutputImageBytes {
		return nil, "", ErrGalleryInvalid
	}
	return data, detectImageContentType(data), nil
}

func (s *StudioJobService) removeStudioResponse(path string) {
	s.removeStudioSpool(path)
	for index := 0; index < 10; index++ {
		s.removeStudioSpool(path + ".image-" + strconv.Itoa(index))
	}
}
