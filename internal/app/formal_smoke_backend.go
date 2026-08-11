package app

import (
	"encoding/json"
	"net"
	"net/http"
	"time"
)

type formalSmokeBackend struct {
	URL      string
	listener net.Listener
	server   *http.Server
}

func newFormalSmokeBackend(id string) (*formalSmokeBackend, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	backend := &formalSmokeBackend{URL: "http://" + listener.Addr().String(), listener: listener}
	backend.server = &http.Server{Handler: formalSmokeHandler(id), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = backend.server.Serve(listener) }()
	return backend, nil
}

func (s *formalSmokeBackend) Close() error {
	return s.server.Close()
}

func formalSmokeHandler(id string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Access-Control-Allow-Origin", "*")
		response.Header().Set("Access-Control-Allow-Headers", "*")
		response.Header().Set("Access-Control-Allow-Methods", "GET, PUT, OPTIONS")
		if request.Method == http.MethodOptions {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		var body any
		switch request.URL.Path {
		case "/edit/" + id:
			body = map[string]any{
				"rev": 1, "title": "Wails Formal Smoke", "fp": "smoke", "tracks": []any{}, "ass_template": "", "is_admin": false,
				"subtitles": []any{map[string]any{"t0": 0, "t1": 2, "ja": "テスト", "zh": "测试"}},
				"track_meta": map[string]any{
					"name": "默认轨", "ja": map[string]any{"hidden": false, "style": "JP"}, "zh": map[string]any{"hidden": false, "style": "CN"},
				},
			}
		case "/edit/" + id + "/peaks":
			body = map[string]any{"per_sec": 20, "duration": 639.548005, "peaks": []any{}}
		case "/edit/state":
			body = map[string]any{"videos": []any{map[string]any{
				"video_id": id, "status": "done", "count": 1, "has_r2": false, "media": "video",
			}}}
		default:
			response.WriteHeader(http.StatusNotFound)
			body = map[string]any{"detail": "not found"}
		}
		_ = json.NewEncoder(response).Encode(body)
	})
}
