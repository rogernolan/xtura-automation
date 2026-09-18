package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"empirebus-tests/service/tracksite"
)

// renderTrack returns a cached map PNG for name@size, rendering from raw
// (when non-nil) or by reading the track when absent.
func (s *Server) renderTrack(name string, size int, raw []byte) ([]byte, error) {
	if data := s.maps.Get(name, size); data != nil {
		return data, nil
	}
	if raw == nil {
		var err error
		raw, err = s.app.TrackRead(name)
		if err != nil {
			return nil, err
		}
	}
	rendered, err := tracksite.RenderTrack(raw, size)
	if err != nil {
		return nil, err
	}
	s.maps.Put(name, size, rendered)
	return rendered, nil
}

func (s *Server) handleTrackMap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/maps/")
	if !strings.HasSuffix(name, ".png") {
		writeError(w, http.StatusBadRequest, errors.New("invalid track name"))
		return
	}
	name = strings.TrimSuffix(name, ".png")
	size := 1000
	if strings.HasSuffix(name, "@2000") {
		size = 2000
		name = strings.TrimSuffix(name, "@2000")
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, errors.New("invalid track name"))
		return
	}
	data, err := s.renderTrack(name, size, nil)
	if err != nil {
		writeTrackError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(data)
}