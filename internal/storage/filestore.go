// Package storage guarda os andares em arquivos JSON (data/floors/<id>/).
// Na Fase 6 esta implementação dá lugar a um repositório Postgres com a mesma interface.
package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Gabrielkr28/company-desk-room-schedule-backend/internal/floorplan"
)

const (
	layoutFile = "layout.json"
	photosFile = "photos.json"
	photosDir  = "photos"
)

// ErrNotFound indica andar ou foto inexistente.
var ErrNotFound = errors.New("não encontrado")

// FileStore lê os andares do disco e recarrega quando os arquivos mudam (hot reload).
type FileStore struct {
	root   string
	logger *slog.Logger

	mu     sync.Mutex
	floors map[string]*floorEntry
}

type floorEntry struct {
	dir       string
	layoutMod time.Time
	photosMod time.Time
	photos    []floorplan.PhotoSpec
	model     *floorplan.Model
	loadErr   error // erro da última leitura; o último modelo válido continua sendo servido
}

// NewFileStore abre <dataDir>/floors e carrega todos os andares.
func NewFileStore(dataDir string, logger *slog.Logger) (*FileStore, error) {
	root := filepath.Join(dataDir, "floors")
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("diretório de andares não encontrado: %s", root)
	}
	s := &FileStore{root: root, logger: logger, floors: map[string]*floorEntry{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.scanLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// List devolve os andares disponíveis, ordenados por id.
func (s *FileStore) List() []floorplan.Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.scanLocked(); err != nil {
		s.logger.Error("falha ao listar andares", "err", err)
	}
	out := make([]floorplan.Summary, 0, len(s.floors))
	for id, fe := range s.floors {
		sum := floorplan.Summary{ID: id}
		if fe.model != nil {
			sum.Name, sum.Building, sum.Seats = fe.model.Name, fe.model.Building, fe.model.Stats.Seats
		}
		if fe.loadErr != nil {
			sum.Error = fe.loadErr.Error()
		}
		out = append(out, sum)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get devolve o modelo resolvido. Se o JSON atual for inválido mas já houve
// uma versão válida, devolve a válida com ReloadError preenchido.
func (s *FileStore) Get(id string) (*floorplan.Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fe, err := s.entryLocked(id)
	if err != nil {
		return nil, err
	}
	if fe.model == nil {
		return nil, fe.loadErr
	}
	m := *fe.model
	if fe.loadErr != nil {
		m.ReloadError = fe.loadErr.Error()
	}
	return &m, nil
}

// UpdatePhotoPose grava a pose calibrada de uma foto em photos.json.
func (s *FileStore) UpdatePhotoPose(floorID, photoID string, pose floorplan.Pose) (*floorplan.Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fe, err := s.entryLocked(floorID)
	if err != nil {
		return nil, err
	}
	if fe.model == nil {
		return nil, fe.loadErr
	}
	if err := pose.Validate(); err != nil {
		return nil, err
	}
	photos := append([]floorplan.PhotoSpec(nil), fe.photos...)
	idx := -1
	for i := range photos {
		if photos[i].ID == photoID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("foto %q: %w", photoID, ErrNotFound)
	}
	photos[idx].Pose = pose

	spec, err := readSpec(filepath.Join(fe.dir, layoutFile))
	if err != nil {
		return nil, err
	}
	if _, err := floorplan.Resolve(spec, photos); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(floorplan.PhotoSet{Photos: photos}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(fe.dir, photosFile), append(data, '\n')); err != nil {
		return nil, err
	}
	fe.photosMod = time.Time{} // força a releitura mesmo que o relógio do FS não tenha avançado
	s.reloadLocked(floorID, fe)
	if fe.model == nil {
		return nil, fe.loadErr
	}
	for i := range fe.model.Photos {
		if fe.model.Photos[i].ID == photoID {
			p := fe.model.Photos[i]
			return &p, nil
		}
	}
	return nil, fmt.Errorf("foto %q: %w", photoID, ErrNotFound)
}

// PhotoFile devolve o caminho da imagem de uma foto.
func (s *FileStore) PhotoFile(floorID, photoID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fe, err := s.entryLocked(floorID)
	if err != nil {
		return "", err
	}
	for _, p := range fe.photos {
		if p.ID != photoID {
			continue
		}
		if p.File == "" || filepath.Base(p.File) != p.File {
			return "", fmt.Errorf("foto %q: nome de arquivo inválido", photoID)
		}
		path := filepath.Join(fe.dir, photosDir, p.File)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("arquivo da foto %q: %w", photoID, ErrNotFound)
		}
		return path, nil
	}
	return "", fmt.Errorf("foto %q: %w", photoID, ErrNotFound)
}

// entryLocked garante que o andar está carregado e atualizado.
func (s *FileStore) entryLocked(id string) (*floorEntry, error) {
	if filepath.Base(id) != id || id == "." || id == ".." {
		return nil, fmt.Errorf("andar %q: %w", id, ErrNotFound)
	}
	dir := filepath.Join(s.root, id)
	if _, err := os.Stat(filepath.Join(dir, layoutFile)); err != nil {
		delete(s.floors, id)
		return nil, fmt.Errorf("andar %q: %w", id, ErrNotFound)
	}
	fe := s.floors[id]
	if fe == nil {
		fe = &floorEntry{dir: dir}
		s.floors[id] = fe
	}
	s.reloadLocked(id, fe)
	return fe, nil
}

func (s *FileStore) scanLocked() error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := s.entryLocked(e.Name()); err == nil {
			seen[e.Name()] = true
		}
	}
	for id := range s.floors {
		if !seen[id] {
			delete(s.floors, id)
		}
	}
	return nil
}

// reloadLocked relê os arquivos do andar se a data de modificação mudou.
func (s *FileStore) reloadLocked(id string, fe *floorEntry) {
	layoutPath, photosPath := filepath.Join(fe.dir, layoutFile), filepath.Join(fe.dir, photosFile)
	lm, pm := modTime(layoutPath), modTime(photosPath)
	if fe.model != nil && fe.layoutMod.Equal(lm) && fe.photosMod.Equal(pm) {
		return
	}
	if fe.model == nil && fe.loadErr != nil && fe.layoutMod.Equal(lm) && fe.photosMod.Equal(pm) {
		return
	}
	fe.layoutMod, fe.photosMod = lm, pm

	model, photos, err := load(id, layoutPath, photosPath, filepath.Join(fe.dir, photosDir))
	if err != nil {
		fe.loadErr = err
		s.logger.Warn("layout com erro; servindo a última versão válida", "floor", id, "err", err)
		return
	}
	fe.model, fe.photos, fe.loadErr = model, photos, nil
	s.logger.Info("andar carregado", "floor", id, "seats", model.Stats.Seats, "warnings", len(model.Warnings), "version", model.Version)
}

func load(id, layoutPath, photosPath, imagesDir string) (*floorplan.Model, []floorplan.PhotoSpec, error) {
	layoutRaw, err := os.ReadFile(layoutPath)
	if err != nil {
		return nil, nil, err
	}
	spec, err := decodeSpec(layoutRaw)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", layoutFile, err)
	}
	if spec.ID != id {
		return nil, nil, fmt.Errorf("%s: id %q diferente da pasta %q", layoutFile, spec.ID, id)
	}

	var photoSet floorplan.PhotoSet
	photosRaw, err := os.ReadFile(photosPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// andar sem fotos de referência
	case err != nil:
		return nil, nil, err
	default:
		if err := decodeStrict(photosRaw, &photoSet); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", photosFile, err)
		}
	}

	model, err := floorplan.Resolve(spec, photoSet.Photos)
	if err != nil {
		return nil, nil, err
	}
	for i := range model.Photos {
		p := &model.Photos[i]
		p.URL = fmt.Sprintf("/api/floors/%s/photos/%s/image", id, p.ID)
		_, statErr := os.Stat(filepath.Join(imagesDir, photoSet.Photos[i].File))
		p.Available = statErr == nil && photoSet.Photos[i].File != ""
	}
	h := fnv.New64a()
	h.Write(layoutRaw)
	h.Write(photosRaw)
	model.Version = fmt.Sprintf("%016x", h.Sum64())
	return model, photoSet.Photos, nil
}

func readSpec(path string) (floorplan.Spec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return floorplan.Spec{}, err
	}
	return decodeSpec(raw)
}

func decodeSpec(raw []byte) (floorplan.Spec, error) {
	var spec floorplan.Spec
	err := decodeStrict(raw, &spec)
	return spec, err
}

// decodeStrict recusa campos desconhecidos: um erro de digitação no JSON vira erro visível.
func decodeStrict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
