// Package httpapi expõe o domínio do andar via HTTP/JSON.
package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/floorplan"
	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/storage"
)

// Store é o que a API precisa da camada de persistência.
type Store interface {
	List() []floorplan.Summary
	Get(id string) (*floorplan.Model, error)
	UpdatePhotoPose(floorID, photoID string, pose floorplan.Pose) (*floorplan.Photo, error)
	PhotoFile(floorID, photoID string) (string, error)
}

type Server struct {
	store   Store
	logger  *slog.Logger
	origins map[string]bool
	mux     *http.ServeMux
}

// New monta as rotas. allowedOrigins controla o CORS (o frontend em dev usa o proxy do Vite).
func New(store Store, logger *slog.Logger, allowedOrigins []string) *Server {
	s := &Server{store: store, logger: logger, origins: map[string]bool{}, mux: http.NewServeMux()}
	for _, o := range allowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			s.origins[o] = true
		}
	}
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/floors", s.listFloors)
	s.mux.HandleFunc("GET /api/floors/{id}", s.getFloor)
	s.mux.HandleFunc("PATCH /api/floors/{id}/photos/{photoId}", s.updatePhotoPose)
	s.mux.HandleFunc("GET /api/floors/{id}/photos/{photoId}/image", s.photoImage)
	return s
}

func (s *Server) Handler() http.Handler {
	return s.recoverPanics(s.logRequests(s.cors(s.mux)))
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().Format(time.RFC3339)})
}

func (s *Server) listFloors(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

// getFloor responde com ETag = versão do layout, permitindo polling barato (304).
func (s *Server) getFloor(w http.ResponseWriter, r *http.Request) {
	model, err := s.store.Get(r.PathValue("id"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	etag := `"` + model.Version + `"`
	if model.ReloadError != "" {
		etag = `"` + model.Version + `-err"`
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *Server) updatePhotoPose(w http.ResponseWriter, r *http.Request) {
	var pose floorplan.Pose
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pose); err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	photo, err := s.store.UpdatePhotoPose(r.PathValue("id"), r.PathValue("photoId"), pose)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, photo)
}

func (s *Server) photoImage(w http.ResponseWriter, r *http.Request) {
	path, err := s.store.PhotoFile(r.PathValue("id"), r.PathValue("photoId"))
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeFile(w, r, path)
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	var verr *floorplan.ValidationError
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.As(err, &verr):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "problems": verr.Problems})
	default:
		s.logger.Error("erro interno", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.origins[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", "GET, PATCH, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Content-Type, If-None-Match")
			h.Set("Access-Control-Expose-Headers", "ETag")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelInfo
		if rec.status == http.StatusNotModified {
			level = slog.LevelDebug // polling do frontend
		}
		s.logger.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path, "status", rec.status, "dur", time.Since(start).Round(time.Microsecond))
	})
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.logger.Error("panic", "value", v, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
