package storage

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/floorplan"
)

const testLayout = `{
  "id": "andar-x", "name": "X", "building": "B", "width": 20, "depth": 10, "ceilingHeight": 3,
  "wallDefaults": {"height": 3, "thickness": 0.25, "color": "#888888"},
  "benchDefaults": {"seatsPerSide": 2, "seatWidth": 1.5, "sideDepth": 0.8},
  "walls": [], "columns": [], "items": [],
  "benches": [{"id": "A1", "center": [10, 5]}]
}`

const testPhotos = `{"photos": [{"id": "f1", "file": "f1.jpg", "label": "F1", "angle": 90, "direction": "leste", "aspect": 0.5625,
  "pose": {"position": [10, 1.6, 6], "yaw": -90, "pitch": -30, "roll": 0, "vfov": 100}}]}`

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestStore(t *testing.T) (*FileStore, string) {
	t.Helper()
	data := t.TempDir()
	dir := filepath.Join(data, "floors", "andar-x")
	must(t, os.MkdirAll(filepath.Join(dir, "photos"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "layout.json"), []byte(testLayout), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "photos.json"), []byte(testPhotos), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "photos", "f1.jpg"), []byte("jpg"), 0o644))
	s, err := NewFileStore(data, quietLogger())
	must(t, err)
	return s, dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// touch grava o arquivo com mtime no futuro para o hot reload perceber a mudança.
func touch(t *testing.T, path, content string, step int) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(content), 0o644))
	future := time.Now().Add(time.Duration(step) * time.Second)
	must(t, os.Chtimes(path, future, future))
}

func TestGetResolvesFloorAndPhotos(t *testing.T) {
	s, _ := newTestStore(t)
	m, err := s.Get("andar-x")
	must(t, err)
	if m.Stats.Seats != 4 || len(m.Photos) != 1 {
		t.Fatalf("modelo inesperado: %+v", m.Stats)
	}
	if p := m.Photos[0]; !p.Available || p.URL != "/api/floors/andar-x/photos/f1/image" {
		t.Errorf("foto = %+v", p)
	}
	if m.Version == "" {
		t.Error("versão vazia")
	}
	if _, err := s.Get("nao-existe"); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
	if _, err := s.Get(".."); !errors.Is(err, ErrNotFound) {
		t.Errorf("path traversal deveria dar ErrNotFound, veio %v", err)
	}
}

func TestHotReloadKeepsLastValidModel(t *testing.T) {
	s, dir := newTestStore(t)
	first, err := s.Get("andar-x")
	must(t, err)

	layout := filepath.Join(dir, "layout.json")
	touch(t, layout, strings.Replace(testLayout, `"seatsPerSide": 2`, `"seatsPerSide": 3`, 1), 2)
	m, err := s.Get("andar-x")
	must(t, err)
	if m.Stats.Seats != 6 || m.Version == first.Version {
		t.Fatalf("hot reload não aplicou: seats=%d", m.Stats.Seats)
	}

	touch(t, layout, `{"id": "andar-x", "campoErrado": 1}`, 4)
	m, err = s.Get("andar-x")
	must(t, err)
	if m.Stats.Seats != 6 || !strings.Contains(m.ReloadError, "campoErrado") {
		t.Fatalf("deveria manter o último válido e reportar o erro: seats=%d err=%q", m.Stats.Seats, m.ReloadError)
	}
}

func TestUpdatePhotoPosePersists(t *testing.T) {
	s, dir := newTestStore(t)
	pose := floorplan.Pose{Position: floorplan.Vec3{9, 1.2, 6.5}, Yaw: -88, Pitch: -35, VFov: 104}
	p, err := s.UpdatePhotoPose("andar-x", "f1", pose)
	must(t, err)
	if p.Pose != pose {
		t.Fatalf("pose devolvida = %+v", p.Pose)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "photos.json"))
	must(t, err)
	if !strings.Contains(string(raw), `"yaw": -88`) {
		t.Errorf("photos.json não foi atualizado:\n%s", raw)
	}

	bad := pose
	bad.VFov = 1
	var verr *floorplan.ValidationError
	if _, err := s.UpdatePhotoPose("andar-x", "f1", bad); !errors.As(err, &verr) {
		t.Errorf("esperava ValidationError, veio %v", err)
	}
	outside := pose
	outside.Position = floorplan.Vec3{80, 1.5, 80}
	if _, err := s.UpdatePhotoPose("andar-x", "f1", outside); !errors.As(err, &verr) {
		t.Errorf("câmera fora do andar deveria falhar, veio %v", err)
	}
	if _, err := s.UpdatePhotoPose("andar-x", "nada", pose); !errors.Is(err, ErrNotFound) {
		t.Errorf("esperava ErrNotFound, veio %v", err)
	}
}

func TestPhotoFile(t *testing.T) {
	s, dir := newTestStore(t)
	path, err := s.PhotoFile("andar-x", "f1")
	must(t, err)
	if path != filepath.Join(dir, "photos", "f1.jpg") {
		t.Errorf("caminho = %s", path)
	}
}

// O layout real do repositório precisa sempre resolver sem erros.
func TestSeedFloorIsValid(t *testing.T) {
	s, err := NewFileStore(filepath.Join("..", "..", "data"), quietLogger())
	must(t, err)
	m, err := s.Get("andar-1")
	must(t, err)
	if m.ReloadError != "" {
		t.Fatal(m.ReloadError)
	}
	if len(m.Warnings) > 0 {
		t.Errorf("avisos no layout: %v", m.Warnings)
	}
	mine := false
	for _, seat := range m.Seats {
		if seat.ID == "I6-S1" {
			mine = seat.Note != ""
		}
	}
	if !mine {
		t.Error("lugar I6-S1 (mesa das fotos) ausente ou sem nota")
	}
	if len(m.Photos) != 4 {
		t.Errorf("esperava 4 fotos, veio %d", len(m.Photos))
	}
}
