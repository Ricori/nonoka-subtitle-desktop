package app

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type cacheCandidate struct {
	path       string
	size       int64
	lastAccess int64
}

func (s *DesktopService) SetOpenInEditor(id string) bool {
	if id != "" && !validLibraryID.MatchString(id) {
		return false
	}
	s.mu.Lock()
	s.openInEditor = id
	s.mu.Unlock()
	return true
}

func (s *DesktopService) sweepPartFiles() int64 {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.RLock()
	dir := s.config.CacheDir
	s.mu.RUnlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var removed int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".part") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr == nil && os.Remove(path) == nil {
			removed += info.Size()
		}
	}
	return removed
}

func (s *DesktopService) convergeCacheLocked() error {
	s.mu.RLock()
	dir := s.config.CacheDir
	limitBytes := int64(s.config.CacheLimitGB * 1024 * 1024 * 1024)
	openID := s.openInEditor
	access := make(map[string]int64, len(s.library))
	for _, entry := range s.library {
		access[entry.ID] = entry.LastAccess
	}
	s.mu.RUnlock()
	if limitBytes <= 0 {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var total int64
	candidates := make([]cacheCandidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".mp4") && !strings.HasSuffix(lower, ".part") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		total += info.Size()
		if !strings.HasSuffix(lower, ".mp4") {
			continue
		}
		id := strings.TrimSuffix(name, filepath.Ext(name))
		if id == openID {
			continue
		}
		lastAccess, known := access[id]
		if !known {
			lastAccess = info.ModTime().UnixMilli()
		}
		candidates = append(candidates, cacheCandidate{
			path: filepath.Join(dir, name), size: info.Size(), lastAccess: lastAccess,
		})
	}
	if total <= limitBytes {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].lastAccess < candidates[j].lastAccess
	})
	for _, candidate := range candidates {
		if total <= limitBytes {
			break
		}
		if err := os.Remove(candidate.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
		total -= candidate.size
	}
	return nil
}
