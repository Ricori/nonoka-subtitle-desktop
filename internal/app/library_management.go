package app

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const clipMax = 200

func (s *DesktopService) SyncCloudLibrary(entries []CloudLibraryEntry) ([]LibraryEntry, error) {
	s.mu.Lock()
	previous := append([]libraryDiskEntry(nil), s.library...)
	changed := false
	cloudIDs := make(map[string]struct{}, len(entries))
	for _, cloud := range entries {
		if validLibraryID.MatchString(cloud.VideoID) {
			cloudIDs[cloud.VideoID] = struct{}{}
		}
	}
	kept := s.library[:0]
	for _, entry := range s.library {
		if _, exists := cloudIDs[entry.ID]; entry.CloudOnly && !exists {
			changed = true
			continue
		}
		kept = append(kept, entry)
	}
	s.library = kept
	for _, cloud := range entries {
		if !validLibraryID.MatchString(cloud.VideoID) {
			continue
		}
		index := -1
		for current := range s.library {
			if s.library[current].ID == cloud.VideoID {
				index = current
				break
			}
		}
		if index < 0 {
			title := strings.TrimSpace(cloud.Title)
			if title == "" {
				title = cloud.VideoID
			}
			addedAt := int64(0)
			if cloud.CreatedAt > 0 && !math.IsNaN(cloud.CreatedAt) && !math.IsInf(cloud.CreatedAt, 0) {
				addedAt = int64(cloud.CreatedAt * 1000)
			}
			s.library = append(s.library, libraryDiskEntry{LibraryEntry: LibraryEntry{
				ID: cloud.VideoID, Title: title, Fingerprint: cloud.Fingerprint, AddedAt: addedAt, CloudOnly: true,
			}})
			changed = true
			continue
		}
		entry := &s.library[index].LibraryEntry
		cloudTitle := strings.TrimSpace(cloud.Title)
		if cloudTitle != "" && ((entry.CloudOnly && entry.Title != cloudTitle) || entry.Title == "") {
			entry.Title = cloudTitle
			changed = true
		}
		if cloud.Fingerprint != "" && ((entry.CloudOnly && entry.Fingerprint != cloud.Fingerprint) || entry.Fingerprint == "") {
			entry.Fingerprint = cloud.Fingerprint
			changed = true
		}
		if cloud.CreatedAt > 0 && !math.IsNaN(cloud.CreatedAt) && !math.IsInf(cloud.CreatedAt, 0) {
			addedAt := int64(cloud.CreatedAt * 1000)
			if (entry.CloudOnly && entry.AddedAt != addedAt) || entry.AddedAt == 0 {
				entry.AddedAt = addedAt
				changed = true
			}
		}
	}
	if changed {
		if err := s.saveLibraryLocked(); err != nil {
			s.library = previous
			s.mu.Unlock()
			return nil, err
		}
	}
	s.mu.Unlock()
	if changed {
		s.emitLibrary()
	}
	return s.GetLibrary(), nil
}

func (s *DesktopService) RenameLibraryTitle(id, title string) (bool, error) {
	if !validLibraryID.MatchString(id) {
		return false, errors.New("无效的媒体 ID")
	}
	s.mu.Lock()
	for index := range s.library {
		if s.library[index].ID != id {
			continue
		}
		previous := s.library[index].Title
		s.library[index].Title = title
		if err := s.saveLibraryLocked(); err != nil {
			s.library[index].Title = previous
			s.mu.Unlock()
			return false, err
		}
		s.mu.Unlock()
		s.emitLibrary()
		return true, nil
	}
	s.mu.Unlock()
	return false, nil
}

func (s *DesktopService) RenameLibraryID(oldID, newID string) (bool, error) {
	if !validLibraryID.MatchString(oldID) || !validLibraryID.MatchString(newID) {
		return false, errors.New("无效的媒体 ID")
	}
	if oldID == newID {
		_, exists := s.findEntry(oldID)
		return exists, nil
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.Lock()
	index := -1
	for current := range s.library {
		if s.library[current].ID == newID {
			s.mu.Unlock()
			return false, errors.New("目标媒体 ID 已存在")
		}
		if s.library[current].ID == oldID {
			index = current
		}
	}
	if index < 0 {
		s.mu.Unlock()
		return false, nil
	}

	pairs := [][2]string{
		{filepath.Join(s.config.CacheDir, oldID+".mp4"), filepath.Join(s.config.CacheDir, newID+".mp4")},
		{filepath.Join(s.paths.ThumbDir, oldID+".jpg"), filepath.Join(s.paths.ThumbDir, newID+".jpg")},
	}
	moved := make([][2]string, 0, len(pairs))
	for _, pair := range pairs {
		changed, err := renameIfPresent(pair[0], pair[1])
		if err != nil {
			rollbackRenames(moved)
			s.mu.Unlock()
			return false, err
		}
		if changed {
			moved = append(moved, pair)
		}
	}
	s.library[index].ID = newID
	if err := s.saveLibraryLocked(); err != nil {
		s.library[index].ID = oldID
		rollbackRenames(moved)
		s.mu.Unlock()
		return false, err
	}
	s.mu.Unlock()
	s.emitLibrary()
	return true, nil
}

func (s *DesktopService) GetClips(id string) []Clip {
	if !validLibraryID.MatchString(id) {
		return []Clip{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.library {
		if entry.ID == id {
			return append([]Clip(nil), entry.Clips...)
		}
	}
	return []Clip{}
}

func (s *DesktopService) SetClips(id string, clips []Clip) (bool, error) {
	if !validLibraryID.MatchString(id) {
		return false, errors.New("无效的媒体 ID")
	}
	normalized := normalizeClips(clips)
	s.mu.Lock()
	for index := range s.library {
		if s.library[index].ID != id {
			continue
		}
		previous := s.library[index].Clips
		s.library[index].Clips = normalized
		if err := s.saveLibraryLocked(); err != nil {
			s.library[index].Clips = previous
			s.mu.Unlock()
			return false, err
		}
		s.mu.Unlock()
		s.emitLibrary()
		return true, nil
	}
	if len(normalized) == 0 {
		s.mu.Unlock()
		return true, nil
	}
	s.library = append([]libraryDiskEntry{{LibraryEntry: LibraryEntry{ID: id, Clips: normalized}}}, s.library...)
	if err := s.saveLibraryLocked(); err != nil {
		s.library = s.library[1:]
		s.mu.Unlock()
		return false, err
	}
	s.mu.Unlock()
	s.emitLibrary()
	return true, nil
}

func (s *DesktopService) RemoveLibraryData(id string, options LibraryRemoveOptions) error {
	return s.removeLibraryData(id, options)
}

func (s *DesktopService) removeLibraryData(id string, options LibraryRemoveOptions) error {
	if !validLibraryID.MatchString(id) {
		return errors.New("无效的媒体 ID")
	}
	if options.Cache {
		s.cacheMu.Lock()
		err := os.Remove(s.cachePath(id))
		s.cacheMu.Unlock()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if options.Thumb {
		if err := os.Remove(s.thumbPath(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !options.Entry {
		return nil
	}
	s.mu.Lock()
	previous := s.library
	next := make([]libraryDiskEntry, 0, len(previous))
	for _, entry := range previous {
		if entry.ID != id {
			next = append(next, entry)
		}
	}
	s.library = next
	if err := s.saveLibraryLocked(); err != nil {
		s.library = previous
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	s.emitLibrary()
	return nil
}

func (s *DesktopService) PathExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (s *DesktopService) RevealInFolder(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absolute); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", "/select,", absolute).Start()
	case "darwin":
		return exec.Command("open", "-R", absolute).Start()
	default:
		return fmt.Errorf("不支持的平台：%s", runtime.GOOS)
	}
}

func (s *DesktopService) PickCacheDirectory() (string, error) {
	s.mu.RLock()
	app, home, current := s.app, s.home, s.config.CacheDir
	s.mu.RUnlock()
	if app == nil || home == nil {
		return "", errors.New("应用尚未初始化")
	}
	return app.Dialog.OpenFile().
		SetTitle("选择视频缓存目录").
		SetDirectory(current).
		AttachToWindow(home).
		CanChooseDirectories(true).
		CanChooseFiles(false).
		CanCreateDirectories(true).
		PromptForSingleSelection()
}

func (s *DesktopService) MigrateCacheDirectory(newDir string) (CacheMigrationResult, error) {
	if strings.TrimSpace(newDir) == "" {
		return CacheMigrationResult{}, errors.New("缓存目录不能为空")
	}
	destination, err := filepath.Abs(newDir)
	if err != nil {
		return CacheMigrationResult{}, err
	}
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.mu.RLock()
	source := s.config.CacheDir
	s.mu.RUnlock()
	source, err = filepath.Abs(source)
	if err != nil {
		return CacheMigrationResult{}, err
	}
	result := CacheMigrationResult{OK: true, Dir: destination}
	if samePath(source, destination) {
		return result, nil
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return CacheMigrationResult{}, err
	}
	entries, err := os.ReadDir(source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return CacheMigrationResult{}, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".mp4") {
			continue
		}
		from := filepath.Join(source, entry.Name())
		to := filepath.Join(destination, entry.Name())
		if err := moveFile(from, to); err != nil {
			result.Kept++
			continue
		}
		result.Moved++
	}
	s.mu.Lock()
	next := s.config
	next.CacheDir = destination
	if err := writeJSONAtomic(s.paths.ConfigFile, persistedConfig(next)); err != nil {
		s.mu.Unlock()
		return result, err
	}
	s.config = next
	s.mu.Unlock()
	return result, nil
}

func normalizeClips(clips []Clip) []Clip {
	if len(clips) > clipMax {
		clips = clips[:clipMax]
	}
	result := make([]Clip, 0, len(clips))
	for _, clip := range clips {
		if math.IsNaN(clip.T0) || math.IsNaN(clip.T1) || math.IsInf(clip.T0, 0) || math.IsInf(clip.T1, 0) || clip.T1 <= clip.T0 {
			continue
		}
		clip.ID = truncateRunes(clip.ID, 32)
		clip.Name = truncateRunes(clip.Name, 80)
		clip.T0 = math.Max(0, clip.T0)
		if clip.CreatedAt == 0 {
			clip.CreatedAt = time.Now().UnixMilli()
		}
		result = append(result, clip)
	}
	return result
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func renameIfPresent(from, to string) (bool, error) {
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if _, err := os.Stat(to); err == nil {
		return false, errors.New("目标文件已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	return true, os.Rename(from, to)
}

func rollbackRenames(pairs [][2]string) {
	for index := len(pairs) - 1; index >= 0; index-- {
		_ = os.Rename(pairs[index][1], pairs[index][0])
	}
}

func moveFile(from, to string) error {
	if _, err := os.Stat(to); err == nil {
		return errors.New("目标文件已存在")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	input, err := os.Open(from)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary := to + ".migrating"
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := os.Rename(temporary, to); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Remove(from); err != nil {
		_ = os.Remove(to)
		return err
	}
	return nil
}

func samePath(left, right string) bool {
	left, right = filepath.Clean(left), filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
