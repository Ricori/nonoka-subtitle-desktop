package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type AppPaths struct {
	Root        string
	ConfigFile  string
	LibraryFile string
	CacheDir    string
	ThumbDir    string
	FFmpegDir   string
	TempDir     string
	WebviewDir  string
}

func defaultAppPaths() (AppPaths, error) {
	if root := os.Getenv("NONOKA_DATA_DIR"); root != "" {
		return appPathsAt(root), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return AppPaths{}, err
	}
	return appPathsAt(filepath.Join(base, "Nonoka")), nil
}

func appPathsAt(root string) AppPaths {
	return AppPaths{
		Root:        root,
		ConfigFile:  filepath.Join(root, "config.json"),
		LibraryFile: filepath.Join(root, "library.json"),
		CacheDir:    filepath.Join(root, "videos"),
		ThumbDir:    filepath.Join(root, "thumbs"),
		FFmpegDir:   filepath.Join(root, "ffmpeg"),
		TempDir:     filepath.Join(root, "temp"),
		WebviewDir:  filepath.Join(root, "webview"),
	}
}

func (p AppPaths) ensure() error {
	for _, dir := range []string{p.Root, p.CacheDir, p.ThumbDir, p.FFmpegDir, p.TempDir, p.WebviewDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
