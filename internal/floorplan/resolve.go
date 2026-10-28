package floorplan

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
)

const (
	defaultDoorHeight = 2.1
	chairOffset       = 0.45 // da borda da mesa até o centro da cadeira
	tolerance         = 0.05
)

// ValidationError agrega todos os problemas encontrados numa spec.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "layout inválido: " + strings.Join(e.Problems, "; ")
}

type itemDefaults struct {
	size      Vec3
	elevation float64
}

var wallItemDefaults = map[string]itemDefaults{
	"whiteboard":   {Vec3{2.4, 1.2, 0.03}, 0.9},
	"kanban":       {Vec3{2.4, 1.4, 0.03}, 0.8},
	"extinguisher": {Vec3{0.2, 0.62, 0.18}, 0.3},
	"hose-cabinet": {Vec3{0.7, 0.95, 0.18}, 0.55},
	"time-clock":   {Vec3{0.22, 0.3, 0.08}, 1.25},
	"panel":        {Vec3{1.2, 1.8, 0.03}, 0.6},
	"mural":        {Vec3{4, 2.4, 0.01}, 0.3},
	"sign":         {Vec3{0.4, 0.15, 0.04}, 2.3},
}

var floorItemDefaults = map[string]Vec3{
	"floor-vent": {0.42, 0.012, 0.42},
	"plant":      {0.5, 1.2, 0.5},
}

var columnStyles = map[string]bool{"art": true, "zone": true, "granite": true, "plain": true}

// Resolve valida a spec e calcula o modelo do andar em coordenadas do mundo.
func Resolve(spec Spec, photos []PhotoSpec) (*Model, error) {
	r := &resolver{spec: spec, ids: map[string]string{}}
	if strings.TrimSpace(spec.ID) == "" {
		r.add("andar sem id")
	}
	if spec.Width <= 0 || spec.Depth <= 0 {
		r.add("dimensões do andar devem ser positivas (width=%g, depth=%g)", spec.Width, spec.Depth)
	}
	if spec.CeilingHeight <= 0 {
		r.add("ceilingHeight deve ser positivo")
	}
	if err := r.err(); err != nil {
		return nil, err
	}

	m := &Model{
		ID:            spec.ID,
		Name:          spec.Name,
		Building:      spec.Building,
		Width:         spec.Width,
		Depth:         spec.Depth,
		CeilingHeight: spec.CeilingHeight,
		Walls:         []Wall{},
		Columns:       []Column{},
		Benches:       []Bench{},
		Seats:         []Seat{},
		Items:         []Item{},
		Photos:        []Photo{},
		Notes:         append([]string{}, spec.Notes...),
	}
	for _, ws := range spec.Walls {
		r.wall(m, ws)
	}
	for _, cs := range spec.Columns {
		r.column(m, cs)
	}
	for _, bs := range spec.Benches {
		r.bench(m, bs)
	}
	for _, is := range spec.Items {
		r.item(m, is)
	}
	r.photos(m, photos)
	if err := r.err(); err != nil {
		return nil, err
	}
	m.Warnings = overlapWarnings(m)
	m.Stats = stats(m)
	return m, nil
}

// Validate confere se a pose é plausível (não depende do andar).
func (p Pose) Validate() error {
	if problems := p.problems(); len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (p Pose) problems() []string {
	var out []string
	if p.VFov < 20 || p.VFov > 160 {
		out = append(out, "vfov deve estar entre 20 e 160 graus")
	}
	if p.Pitch < -90 || p.Pitch > 90 {
		out = append(out, "pitch deve estar entre -90 e 90 graus")
	}
	if p.Roll < -45 || p.Roll > 45 {
		out = append(out, "roll deve estar entre -45 e 45 graus")
	}
	if p.Position[1] < 0.1 || p.Position[1] > 5 {
		out = append(out, "altura da câmera deve estar entre 0,1 e 5 m")
	}
	return out
}

type resolver struct {
	spec     Spec
	ids      map[string]string // id → tipo do elemento, para achar duplicados
	problems []string
}

func (r *resolver) add(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *resolver) err() error {
	if len(r.problems) == 0 {
		return nil
	}
	return &ValidationError{Problems: r.problems}
}

// claim registra um id e reporta vazios ou repetidos. Retorna false se o elemento deve ser ignorado.
func (r *resolver) claim(id, kind string) bool {
	if strings.TrimSpace(id) == "" {
		r.add("%s sem id", kind)
		return false
	}
	if prev, ok := r.ids[id]; ok {
		r.add("id %q repetido (%s e %s)", id, prev, kind)
		return false
	}
	r.ids[id] = kind
	return true
}

func (r *resolver) inside(p Vec2, margin float64) bool {
	return p[0] >= -margin && p[0] <= r.spec.Width+margin && p[1] >= -margin && p[1] <= r.spec.Depth+margin
}

func (r *resolver) confidence(owner string, c Confidence) Confidence {
	switch c {
	case "":
		return ConfidenceHigh
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		return c
	}
	r.add("%s: confidence %q inválida (use alta, media ou baixa)", owner, c)
	return ConfidenceLow
}

func (r *resolver) wall(m *Model, ws WallSpec) {
	if !r.claim(ws.ID, "parede") {
		return
	}
	defaults := r.spec.WallDefaults
	height := firstPositive(ws.Height, defaults.Height, r.spec.CeilingHeight)
	thickness := firstPositive(ws.Thickness, defaults.Thickness, 0.2)
	dx, dz := ws.To[0]-ws.From[0], ws.To[1]-ws.From[1]
	length := math.Hypot(dx, dz)
	if length < 0.1 {
		r.add("parede %q: comprimento zero", ws.ID)
		return
	}
	if !r.inside(ws.From, tolerance) || !r.inside(ws.To, tolerance) {
		r.add("parede %q: fora dos limites do andar", ws.ID)
	}

	dir := Vec2{dx / length, dz / length}
	inward := Vec2{-dir[1], dir[0]}
	mid := Vec2{(ws.From[0] + ws.To[0]) / 2, (ws.From[1] + ws.To[1]) / 2}
	if (r.spec.Width/2-mid[0])*inward[0]+(r.spec.Depth/2-mid[1])*inward[1] < 0 {
		inward = Vec2{-inward[0], -inward[1]}
	}

	w := Wall{
		ID:         ws.ID,
		Name:       ws.Name,
		Start:      ws.From,
		End:        ws.To,
		Length:     round(length),
		Height:     height,
		Thickness:  thickness,
		Color:      firstNonEmpty(ws.Color, defaults.Color, "#8e979c"),
		Inward:     Vec2{round(inward[0]), round(inward[1])},
		Confidence: r.confidence("parede "+ws.ID, ws.Confidence),
		Openings:   []Opening{},
	}

	openings := append([]OpeningSpec(nil), ws.Openings...)
	sort.SliceStable(openings, func(i, j int) bool { return openings[i].At < openings[j].At })
	prevEnd, prevID := math.Inf(-1), ""
	for _, op := range openings {
		if !r.claim(op.ID, "vão") {
			continue
		}
		h := firstPositive(op.Height, defaultDoorHeight)
		start, end := op.At-op.Width/2, op.At+op.Width/2
		switch {
		case op.Kind == "":
			r.add("vão %q: kind obrigatório", op.ID)
		case op.Width <= 0:
			r.add("vão %q: largura deve ser positiva", op.ID)
		case start < -tolerance || end > length+tolerance:
			r.add("vão %q: fora da parede %q (0–%.2f m)", op.ID, ws.ID, length)
		case h > height:
			r.add("vão %q: mais alto que a parede %q", op.ID, ws.ID)
		case start < prevEnd-tolerance:
			r.add("vãos %q e %q se sobrepõem", prevID, op.ID)
		}
		prevEnd, prevID = end, op.ID
		w.Openings = append(w.Openings, Opening{
			ID: op.ID, Kind: op.Kind, Along: op.At, Width: op.Width, Height: h, Label: op.Label, Color: op.Color,
		})
	}
	m.Walls = append(m.Walls, w)

	faceYaw := yawFacing(inward)
	for _, is := range ws.Items {
		if !r.claim(is.ID, "item de parede") {
			continue
		}
		if is.Kind == "" {
			r.add("item %q: kind obrigatório", is.ID)
			continue
		}
		if is.At < -tolerance || is.At > length+tolerance {
			r.add("item %q: fora da parede %q (0–%.2f m)", is.ID, ws.ID, length)
			continue
		}
		def, ok := wallItemDefaults[is.Kind]
		if !ok {
			def = itemDefaults{Vec3{1, 1, 0.05}, 1}
		}
		size := Vec3{firstPositive(is.Width, def.size[0]), firstPositive(is.Height, def.size[1]), firstPositive(is.Depth, def.size[2])}
		elevation := def.elevation
		if is.Elevation != nil {
			elevation = *is.Elevation
		}
		offset := size[2]/2 + 0.01
		m.Items = append(m.Items, Item{
			ID:     is.ID,
			Kind:   is.Kind,
			WallID: ws.ID,
			Position: Vec3{
				round(ws.From[0] + dir[0]*is.At + inward[0]*offset),
				round(elevation + size[1]/2),
				round(ws.From[1] + dir[1]*is.At + inward[1]*offset),
			},
			Yaw:   faceYaw,
			Size:  size,
			Label: is.Label,
			Color: is.Color,
		})
	}
}

func (r *resolver) column(m *Model, cs ColumnSpec) {
	if !r.claim(cs.ID, "pilar") {
		return
	}
	if cs.Size[0] <= 0 || cs.Size[1] <= 0 {
		r.add("pilar %q: tamanho deve ser positivo", cs.ID)
		return
	}
	if !r.inside(cs.At, tolerance) {
		r.add("pilar %q: fora dos limites do andar", cs.ID)
	}
	style := firstNonEmpty(cs.Style, "art")
	if !columnStyles[style] {
		r.add("pilar %q: style %q inválido (art, zone, granite, plain)", cs.ID, style)
	}
	m.Columns = append(m.Columns, Column{
		ID:         cs.ID,
		Position:   cs.At,
		Size:       cs.Size,
		Height:     r.spec.CeilingHeight,
		Style:      style,
		Label:      cs.Label,
		Confidence: r.confidence("pilar "+cs.ID, cs.Confidence),
	})
}

func (r *resolver) bench(m *Model, bs BenchSpec) {
	if !r.claim(bs.ID, "bancada") {
		return
	}
	defaults := r.spec.BenchDefaults
	perSide := bs.SeatsPerSide
	if perSide == 0 {
		perSide = defaults.SeatsPerSide
	}
	if perSide == 0 {
		perSide = 4
	}
	seatW := firstPositive(bs.SeatWidth, defaults.SeatWidth, 1.5)
	sideD := firstPositive(bs.SideDepth, defaults.SideDepth, 0.8)
	switch {
	case perSide < 1 || perSide > 30:
		r.add("bancada %q: seatsPerSide deve estar entre 1 e 30", bs.ID)
		return
	case seatW < 0.6 || seatW > 3:
		r.add("bancada %q: seatWidth deve estar entre 0,6 e 3 m", bs.ID)
		return
	case sideD < 0.4 || sideD > 1.5:
		r.add("bancada %q: sideDepth deve estar entre 0,4 e 1,5 m", bs.ID)
		return
	}

	length := float64(perSide) * seatW
	theta := bs.Rotation * math.Pi / 180
	sin, cos := math.Sin(theta), math.Cos(theta)
	toWorld := func(lx, lz float64) Vec2 {
		return Vec2{bs.Center[0] + lx*cos + lz*sin, bs.Center[1] - lx*sin + lz*cos}
	}
	for _, c := range [][2]float64{{-length / 2, -sideD}, {length / 2, -sideD}, {length / 2, sideD}, {-length / 2, sideD}} {
		if !r.inside(toWorld(c[0], c[1]), tolerance) {
			r.add("bancada %q: sai dos limites do andar", bs.ID)
			break
		}
	}

	conf := r.confidence("bancada "+bs.ID, bs.Confidence)
	b := Bench{
		ID:           bs.ID,
		Row:          bs.Row,
		Bay:          bs.Bay,
		Center:       bs.Center,
		Rotation:     bs.Rotation,
		Length:       length,
		SideDepth:    sideD,
		SeatWidth:    seatW,
		SeatsPerSide: perSide,
		Sides:        []string{},
		Confidence:   conf,
	}
	omit := strings.ToUpper(strings.TrimSpace(bs.OmitSide))
	known := map[string]bool{}
	for _, s := range []float64{-1, 1} {
		side := cardinal(Vec2{s * sin, s * cos})
		if side == omit {
			continue
		}
		b.Sides = append(b.Sides, side)
		yaw := bs.Rotation // quem senta no lado +z local olha para -z local
		if s < 0 {
			yaw += 180
		}
		for i := 1; i <= perSide; i++ {
			key := side + strconv.Itoa(i)
			known[key] = true
			ov := bs.Seats[key]
			if ov.Disabled {
				continue
			}
			id := bs.ID + "-" + key
			if !r.claim(id, "lugar") {
				continue
			}
			monitors, laptop := defaultEquipment(id)
			if ov.Monitors != nil {
				monitors = *ov.Monitors
				if monitors < 0 || monitors > 4 {
					r.add("lugar %q: monitors deve estar entre 0 e 4", id)
				}
			}
			if ov.Laptop != nil {
				laptop = *ov.Laptop
			}
			lx := -length/2 + seatW*(float64(i)-0.5)
			m.Seats = append(m.Seats, Seat{
				ID:         id,
				BenchID:    bs.ID,
				Side:       side,
				Index:      i,
				Position:   roundVec2(toWorld(lx, s*(sideD+chairOffset))),
				DeskCenter: roundVec2(toWorld(lx, s*sideD/2)),
				DeskSize:   Vec2{seatW, sideD},
				Yaw:        normalizeDeg(yaw),
				Monitors:   monitors,
				Laptop:     laptop,
				Note:       ov.Note,
				Confidence: conf,
			})
			b.SeatCount++
		}
	}
	unknown := make([]string, 0)
	for key := range bs.Seats {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	for _, key := range unknown {
		r.add("bancada %q: override para lugar inexistente %q", bs.ID, key)
	}
	m.Benches = append(m.Benches, b)
}

func (r *resolver) item(m *Model, is ItemSpec) {
	if !r.claim(is.ID, "item") {
		return
	}
	if is.Kind == "" {
		r.add("item %q: kind obrigatório", is.ID)
		return
	}
	if !r.inside(is.At, tolerance) {
		r.add("item %q: fora dos limites do andar", is.ID)
	}
	def, ok := floorItemDefaults[is.Kind]
	if !ok {
		def = Vec3{0.5, 0.5, 0.5}
	}
	size := Vec3{firstPositive(is.Width, def[0]), firstPositive(is.Height, def[1]), firstPositive(is.Depth, def[2])}
	m.Items = append(m.Items, Item{
		ID:       is.ID,
		Kind:     is.Kind,
		Position: Vec3{is.At[0], round(size[1] / 2), is.At[1]},
		Yaw:      normalizeDeg(is.Rotation),
		Size:     size,
		Label:    is.Label,
		Color:    is.Color,
	})
}

func (r *resolver) photos(m *Model, photos []PhotoSpec) {
	seen := map[string]bool{}
	for _, ps := range photos {
		if ps.ID == "" || seen[ps.ID] {
			r.add("foto com id vazio ou repetido: %q", ps.ID)
			continue
		}
		seen[ps.ID] = true
		for _, p := range ps.Pose.problems() {
			r.add("foto %q: %s", ps.ID, p)
		}
		if pos := ps.Pose.Position; !r.inside(Vec2{pos[0], pos[2]}, 1) {
			r.add("foto %q: câmera fora do andar", ps.ID)
		}
		aspect := ps.Aspect
		if aspect <= 0 {
			aspect = 9.0 / 16
		}
		m.Photos = append(m.Photos, Photo{
			ID:        ps.ID,
			Label:     ps.Label,
			Angle:     ps.Angle,
			Direction: ps.Direction,
			Aspect:    aspect,
			Note:      ps.Note,
			Pose:      ps.Pose,
		})
	}
}

type box struct {
	id                     string
	minX, minZ, maxX, maxZ float64
}

func (a box) overlaps(b box, eps float64) bool {
	return a.minX < b.maxX-eps && a.maxX > b.minX+eps && a.minZ < b.maxZ-eps && a.maxZ > b.minZ+eps
}

func overlapWarnings(m *Model) []string {
	warnings := []string{}
	benches := make([]box, len(m.Benches))
	for i, b := range m.Benches {
		theta := b.Rotation * math.Pi / 180
		hx := math.Abs(b.Length/2*math.Cos(theta)) + math.Abs(b.SideDepth*math.Sin(theta))
		hz := math.Abs(b.Length/2*math.Sin(theta)) + math.Abs(b.SideDepth*math.Cos(theta))
		benches[i] = box{b.ID, b.Center[0] - hx, b.Center[1] - hz, b.Center[0] + hx, b.Center[1] + hz}
	}
	for i := range benches {
		for j := i + 1; j < len(benches); j++ {
			if benches[i].overlaps(benches[j], 0.02) {
				warnings = append(warnings, fmt.Sprintf("bancadas %s e %s se sobrepõem", benches[i].id, benches[j].id))
			}
		}
	}
	for _, c := range m.Columns {
		cb := box{c.ID, c.Position[0] - c.Size[0]/2, c.Position[1] - c.Size[1]/2, c.Position[0] + c.Size[0]/2, c.Position[1] + c.Size[1]/2}
		for _, bb := range benches {
			if cb.overlaps(bb, 0.02) {
				warnings = append(warnings, fmt.Sprintf("pilar %s invade a bancada %s", c.ID, bb.id))
			}
		}
	}
	return warnings
}

func stats(m *Model) Stats {
	s := Stats{
		Benches:      len(m.Benches),
		Seats:        len(m.Seats),
		Columns:      len(m.Columns),
		ByConfidence: map[Confidence]int{ConfidenceHigh: 0, ConfidenceMedium: 0, ConfidenceLow: 0},
	}
	for _, seat := range m.Seats {
		s.ByConfidence[seat.Confidence]++
	}
	return s
}

// defaultEquipment varia monitores/notebook de forma determinística pelo id do lugar.
func defaultEquipment(id string) (monitors int, laptop bool) {
	h := fnv.New32a()
	h.Write([]byte(id))
	v := h.Sum32()
	monitors = 1
	if v%100 < 65 {
		monitors = 2
	}
	return monitors, (v/100)%100 < 40
}

// cardinal devolve N, S, E ou W para uma direção no plano (z positivo = sul).
func cardinal(d Vec2) string {
	if math.Abs(d[1]) >= math.Abs(d[0]) {
		if d[1] > 0 {
			return "S"
		}
		return "N"
	}
	if d[0] > 0 {
		return "E"
	}
	return "W"
}

// yawFacing devolve o yaw (graus) de um objeto cuja frente aponta para d.
func yawFacing(d Vec2) float64 {
	return normalizeDeg(round(math.Atan2(-d[0], -d[1]) * 180 / math.Pi))
}

// normalizeDeg leva um ângulo para o intervalo (-180, 180].
func normalizeDeg(a float64) float64 {
	a = math.Mod(a, 360)
	if a > 180 {
		a -= 360
	} else if a <= -180 {
		a += 360
	}
	return a
}

func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func roundVec2(v Vec2) Vec2 {
	return Vec2{round(v[0]), round(v[1])}
}

func firstPositive(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
