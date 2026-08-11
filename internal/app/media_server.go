package app

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type loopbackMediaServer struct {
	listener net.Listener
	server   *http.Server
	path     string
	baseURL  string
	mu       sync.RWMutex
	media    map[uint64]string
	order    []uint64
	source   func() string
}

func newLoopbackMediaServer(source func() string) (*loopbackMediaServer, error) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	mediaPath := "/media/" + token
	media := &loopbackMediaServer{
		listener: listener,
		path:     mediaPath,
		baseURL:  "http://" + listener.Addr().String() + mediaPath,
		media:    make(map[uint64]string),
		source:   source,
	}
	media.server = &http.Server{
		Handler:           media.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
	}
	go func() {
		if err := media.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("media server: %v", err)
		}
	}()
	return media, nil
}

func (s *loopbackMediaServer) URL(sequence uint64) string {
	return s.baseURL + "?v=" + strconv.FormatUint(sequence, 10)
}

func (s *loopbackMediaServer) RegisterURL(sequence uint64, path string) string {
	s.mu.Lock()
	if _, exists := s.media[sequence]; !exists {
		s.order = append(s.order, sequence)
	}
	s.media[sequence] = path
	if len(s.order) > 128 {
		delete(s.media, s.order[0])
		s.order = s.order[1:]
	}
	s.mu.Unlock()
	return s.URL(sequence)
}

func (s *loopbackMediaServer) Close() error {
	return s.server.Close()
}

func (s *loopbackMediaServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != s.path {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := s.mediaPath(r)
		if path == "" {
			http.NotFound(w, r)
			return
		}
		file, err := os.Open(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || !stat.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}

		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
		if contentType == "" {
			contentType = "video/mp4"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, stat.Name(), stat.ModTime(), file)
	})
}

func (s *loopbackMediaServer) mediaPath(request *http.Request) string {
	sequence, _ := strconv.ParseUint(request.URL.Query().Get("v"), 10, 64)
	s.mu.RLock()
	path := s.media[sequence]
	s.mu.RUnlock()
	if path == "" && s.source != nil {
		path = s.source()
	}
	return path
}
