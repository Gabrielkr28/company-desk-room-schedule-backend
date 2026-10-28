package floorplan

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func baseSpec() Spec {
	return Spec{
		ID: "teste", Name: "Teste", Width: 20, Depth: 10, CeilingHeight: 3,
		WallDefaults:  WallDefaults{Height: 3, Thickness: 0.25, Color: "#888888"},
		BenchDefaults: BenchDefaults{SeatsPerSide: 4, SeatWidth: 1.5, SideDepth: 0.8},
	}
}

func seatByID(t *testing.T, m *Model, id string) Seat {
	t.Helper()
	for _, s := range m.Seats {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("lugar %s não encontrado", id)
	return Seat{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestBenchSeatsAreNamedAndPlaced(t *testing.T) {
	spec := baseSpec()
	spec.Benches = []BenchSpec{{ID: "I6", Center: Vec2{10, 5}}}

	m, err := Resolve(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Seats) != 8 {
		t.Fatalf("esperava 8 lugares, veio %d", len(m.Seats))
	}
	// S1: ponta oeste do lado sul, olhando para o norte.
	s1 := seatByID(t, m, "I6-S1")
	if !near(s1.Position[0], 7.75) || !near(s1.Position[1], 5+0.8+chairOffset) {
		t.Errorf("posição de I6-S1 = %v", s1.Position)
	}
	if !near(s1.DeskCenter[1], 5.4) || s1.Yaw != 0 {
		t.Errorf("mesa/yaw de I6-S1 = %v / %v", s1.DeskCenter, s1.Yaw)
	}
	// N4: ponta leste do lado norte, olhando para o sul.
	n4 := seatByID(t, m, "I6-N4")
	if !near(n4.Position[0], 12.25) || !near(n4.Position[1], 5-0.8-chairOffset) || n4.Yaw != 180 {
		t.Errorf("I6-N4 = %+v", n4)
	}
	if got := m.Benches[0].Sides; strings.Join(got, ",") != "N,S" {
		t.Errorf("lados = %v", got)
	}
}

func TestRotatedBenchUsesEastWestSides(t *testing.T) {
	spec := baseSpec()
	spec.Benches = []BenchSpec{{ID: "R1", Center: Vec2{10, 5}, Rotation: 90, SeatsPerSide: 2}}

	m, err := Resolve(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	e1 := seatByID(t, m, "R1-E1")
	if e1.Yaw != 90 { // lado leste olha para oeste
		t.Errorf("yaw de R1-E1 = %v", e1.Yaw)
	}
	if !near(e1.Position[0], 10+0.8+chairOffset) || !near(e1.Position[1], 5.75) { // índice 1 fica na ponta sul (x local → norte)
		t.Errorf("posição de R1-E1 = %v", e1.Position)
	}
	seatByID(t, m, "R1-W2")
}

func TestSeatOverridesAndOmittedSide(t *testing.T) {
	spec := baseSpec()
	two, yes := 2, true
	spec.Benches = []BenchSpec{{
		ID: "A1", Center: Vec2{10, 5}, OmitSide: "N",
		Seats: map[string]SeatOverride{"S1": {Monitors: &two, Laptop: &yes, Note: "minha"}, "S2": {Disabled: true}},
	}}
	m, err := Resolve(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Seats) != 3 {
		t.Fatalf("esperava 3 lugares (lado N omitido, S2 desativado), veio %d", len(m.Seats))
	}
	s1 := seatByID(t, m, "A1-S1")
	if s1.Monitors != 2 || !s1.Laptop || s1.Note != "minha" {
		t.Errorf("override não aplicado: %+v", s1)
	}
}

func TestValidationCollectsProblems(t *testing.T) {
	spec := baseSpec()
	spec.Walls = []WallSpec{{
		ID: "w", From: Vec2{0, 0}, To: Vec2{20, 0},
		Openings: []OpeningSpec{
			{ID: "d1", Kind: "door", At: 5, Width: 1},
			{ID: "d2", Kind: "door", At: 5.5, Width: 1},  // sobrepõe d1
			{ID: "d3", Kind: "door", At: 19.9, Width: 1}, // sai da parede
		},
	}}
	spec.Benches = []BenchSpec{
		{ID: "B1", Center: Vec2{1, 5}}, // sai do andar
		{ID: "B2", Center: Vec2{10, 5}, Seats: map[string]SeatOverride{"S9": {}}},
		{ID: "w", Center: Vec2{10, 8}}, // id repetido
	}

	_, err := Resolve(spec, nil)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("esperava ValidationError, veio %v", err)
	}
	joined := strings.Join(verr.Problems, "\n")
	for _, want := range []string{"se sobrepõem", `"d3": fora da parede`, `"B1": sai dos limites`, `inexistente "S9"`, `id "w" repetido`} {
		if !strings.Contains(joined, want) {
			t.Errorf("faltou problema %q em:\n%s", want, joined)
		}
	}
}

func TestWallItemsFaceTheInterior(t *testing.T) {
	spec := baseSpec()
	elev := 1.0
	spec.Walls = []WallSpec{
		{ID: "norte", From: Vec2{0, 0}, To: Vec2{20, 0}, Items: []WallItemSpec{{ID: "q", Kind: "whiteboard", At: 4, Elevation: &elev, Height: 1}}},
		{ID: "leste", From: Vec2{20, 0}, To: Vec2{20, 10}},
	}
	m, err := Resolve(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Walls[0].Inward != (Vec2{0, 1}) || m.Walls[1].Inward != (Vec2{-1, 0}) {
		t.Errorf("normais = %v / %v", m.Walls[0].Inward, m.Walls[1].Inward)
	}
	item := m.Items[0]
	if item.Yaw != 180 || !near(item.Position[1], 1.5) || item.Position[2] <= 0 {
		t.Errorf("quadro = %+v", item)
	}
}

func TestOverlapWarnings(t *testing.T) {
	spec := baseSpec()
	spec.Benches = []BenchSpec{{ID: "A", Center: Vec2{6, 5}}, {ID: "B", Center: Vec2{9, 5}}}
	spec.Columns = []ColumnSpec{{ID: "P", At: Vec2{15, 5}, Size: Vec2{0.8, 0.8}}}
	m, err := Resolve(spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Warnings) != 1 || !strings.Contains(m.Warnings[0], "A e B") {
		t.Errorf("avisos = %v", m.Warnings)
	}
}

func TestPoseValidation(t *testing.T) {
	ok := Pose{Position: Vec3{1, 1.6, 1}, Yaw: 10, Pitch: -30, VFov: 100}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := Pose{Position: Vec3{1, 9, 1}, Pitch: -120, VFov: 5}
	var verr *ValidationError
	if err := bad.Validate(); !errors.As(err, &verr) || len(verr.Problems) != 3 {
		t.Fatalf("esperava 3 problemas, veio %v", err)
	}
}
