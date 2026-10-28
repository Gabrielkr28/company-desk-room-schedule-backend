package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/floorplan"
	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/storage"
)

type fakeStore struct {
	model *floorplan.Model
	saved floorplan.Pose
}

func (f *fakeStore) List() []floorplan.Summary {
	return []floorplan.Summary{{ID: f.model.ID, Name: f.model.Name, Seats: f.model.Stats.Seats}}
}

func (f *fakeStore) Get(id string) (*floorplan.Model, error) {
	if id != f.model.ID {
		return nil, fmt.Errorf("andar %q: %w", id, storage.ErrNotFound)
	}
	return f.model, nil
}

func (f *fakeStore) UpdatePhotoPose(_, photoID string, pose floorplan.Pose) (*floorplan.Photo, error) {
	if err := pose.Validate(); err != nil {
		return nil, err
	}
	f.saved = pose
	return &floorplan.Photo{ID: photoID, Pose: pose}, nil
}

func (f *fakeStore) PhotoFile(_, photoID string) (string, error) {
	return "", fmt.Errorf("foto %q: %w", photoID, storage.ErrNotFound)
}

func newTestServer() (*fakeStore, http.Handler) {
	store := &fakeStore{model: &floorplan.Model{ID: "andar-1", Name: "Andar", Version: "abc", Stats: floorplan.Stats{Seats: 8}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return store, New(store, logger, []string{"http://localhost:5190"}).Handler()
}

func do(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGetFloorWithETag(t *testing.T) {
	_, h := newTestServer()
	rec := do(h, http.MethodGet, "/api/floors/andar-1", "", nil)
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"abc"` {
		t.Fatalf("status=%d etag=%q", rec.Code, rec.Header().Get("ETag"))
	}
	var m floorplan.Model
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil || m.ID != "andar-1" {
		t.Fatalf("corpo inesperado: %v %+v", err, m)
	}
	if rec := do(h, http.MethodGet, "/api/floors/andar-1", "", map[string]string{"If-None-Match": `"abc"`}); rec.Code != http.StatusNotModified {
		t.Errorf("esperava 304, veio %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/floors/outro", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("esperava 404, veio %d", rec.Code)
	}
}

func TestListFloors(t *testing.T) {
	_, h := newTestServer()
	rec := do(h, http.MethodGet, "/api/floors", "", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"seats":8`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestUpdatePhotoPose(t *testing.T) {
	store, h := newTestServer()
	body := `{"position":[1,1.6,2],"yaw":10,"pitch":-30,"roll":0,"vfov":100}`
	rec := do(h, http.MethodPatch, "/api/floors/andar-1/photos/f1", body, nil)
	if rec.Code != http.StatusOK || store.saved.Yaw != 10 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	if rec := do(h, http.MethodPatch, "/api/floors/andar-1/photos/f1", `{"yaw":"x"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("JSON inválido deveria dar 400, veio %d", rec.Code)
	}
	if rec := do(h, http.MethodPatch, "/api/floors/andar-1/photos/f1", `{"position":[1,1.6,2],"vfov":1}`, nil); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("pose inválida deveria dar 422, veio %d", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	_, h := newTestServer()
	rec := do(h, http.MethodOptions, "/api/floors/andar-1/photos/f1", "", map[string]string{"Origin": "http://localhost:5190"})
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5190" {
		t.Fatalf("preflight: status=%d headers=%v", rec.Code, rec.Header())
	}
	rec = do(h, http.MethodGet, "/api/health", "", map[string]string{"Origin": "http://evil.example"})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("origem não liberada recebeu CORS")
	}
}
