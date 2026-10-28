// Package floorplan contém o domínio do andar: a spec editável à mão
// (layout.json / photos.json) e o modelo resolvido que o frontend renderiza.
//
// Coordenadas em metros, origem no canto noroeste do andar:
// x cresce para leste, z cresce para sul e y é a altura.
package floorplan

// Vec2 é um ponto no plano do piso: [x, z].
type Vec2 [2]float64

// Vec3 é um ponto no espaço: [x, y, z].
type Vec3 [3]float64

// Confidence indica o quanto um elemento foi confirmado pelas fotos.
type Confidence string

const (
	ConfidenceHigh   Confidence = "alta"  // visto de perto nas fotos
	ConfidenceMedium Confidence = "media" // visto ao fundo
	ConfidenceLow    Confidence = "baixa" // inferido pela grade
)

// Spec é o formato de autoria do andar (data/floors/<id>/layout.json).
type Spec struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Building      string        `json:"building"`
	Width         float64       `json:"width"`
	Depth         float64       `json:"depth"`
	CeilingHeight float64       `json:"ceilingHeight"`
	WallDefaults  WallDefaults  `json:"wallDefaults"`
	BenchDefaults BenchDefaults `json:"benchDefaults"`
	Notes         []string      `json:"notes,omitempty"`
	Walls         []WallSpec    `json:"walls"`
	Columns       []ColumnSpec  `json:"columns"`
	Benches       []BenchSpec   `json:"benches"`
	Items         []ItemSpec    `json:"items"`
}

type WallDefaults struct {
	Height    float64 `json:"height"`
	Thickness float64 `json:"thickness"`
	Color     string  `json:"color"`
}

// WallSpec descreve uma parede pela face interna (from → to). A espessura
// cresce para o lado de fora do andar.
type WallSpec struct {
	ID         string         `json:"id"`
	Name       string         `json:"name,omitempty"`
	From       Vec2           `json:"from"`
	To         Vec2           `json:"to"`
	Height     float64        `json:"height,omitempty"`
	Thickness  float64        `json:"thickness,omitempty"`
	Color      string         `json:"color,omitempty"`
	Confidence Confidence     `json:"confidence,omitempty"`
	Openings   []OpeningSpec  `json:"openings,omitempty"`
	Items      []WallItemSpec `json:"items,omitempty"`
}

// OpeningSpec é um vão na parede. At é a distância de From até o centro do vão.
type OpeningSpec struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"` // door, double-door, glass-door, elevator
	At     float64 `json:"at"`
	Width  float64 `json:"width"`
	Height float64 `json:"height,omitempty"`
	Label  string  `json:"label,omitempty"`
	Color  string  `json:"color,omitempty"`
}

// WallItemSpec é algo preso na face interna da parede (quadro, extintor...).
type WallItemSpec struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	At        float64  `json:"at"`
	Elevation *float64 `json:"elevation,omitempty"` // altura da base; ausente = padrão do tipo
	Width     float64  `json:"width,omitempty"`
	Height    float64  `json:"height,omitempty"`
	Depth     float64  `json:"depth,omitempty"`
	Label     string   `json:"label,omitempty"`
	Color     string   `json:"color,omitempty"`
}

type ColumnSpec struct {
	ID         string     `json:"id"`
	At         Vec2       `json:"at"`
	Size       Vec2       `json:"size"`
	Style      string     `json:"style,omitempty"` // art, zone, granite, plain
	Label      string     `json:"label,omitempty"`
	Confidence Confidence `json:"confidence,omitempty"`
}

type BenchDefaults struct {
	SeatsPerSide int     `json:"seatsPerSide"`
	SeatWidth    float64 `json:"seatWidth"`
	SideDepth    float64 `json:"sideDepth"`
}

// BenchSpec é uma bancada com divisória central e lugares dos dois lados.
// Rotation em graus, anti-horário visto de cima; 0 = comprimento ao longo de x.
type BenchSpec struct {
	ID           string                  `json:"id"`
	Row          string                  `json:"row,omitempty"`
	Bay          int                     `json:"bay,omitempty"`
	Center       Vec2                    `json:"center"`
	Rotation     float64                 `json:"rotation,omitempty"`
	SeatsPerSide int                     `json:"seatsPerSide,omitempty"`
	SeatWidth    float64                 `json:"seatWidth,omitempty"`
	SideDepth    float64                 `json:"sideDepth,omitempty"`
	OmitSide     string                  `json:"omitSide,omitempty"` // N, S, E ou W: lado sem lugares
	Confidence   Confidence              `json:"confidence,omitempty"`
	Seats        map[string]SeatOverride `json:"seats,omitempty"` // chave = lado + índice, ex.: "S1"
}

type SeatOverride struct {
	Monitors *int   `json:"monitors,omitempty"`
	Laptop   *bool  `json:"laptop,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
	Note     string `json:"note,omitempty"`
}

// ItemSpec é um objeto solto no piso (ralo de ar, planta...).
type ItemSpec struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	At       Vec2    `json:"at"`
	Rotation float64 `json:"rotation,omitempty"`
	Width    float64 `json:"width,omitempty"`
	Height   float64 `json:"height,omitempty"`
	Depth    float64 `json:"depth,omitempty"`
	Label    string  `json:"label,omitempty"`
	Color    string  `json:"color,omitempty"`
}

// PhotoSet é o conteúdo de data/floors/<id>/photos.json.
type PhotoSet struct {
	Photos []PhotoSpec `json:"photos"`
}

type PhotoSpec struct {
	ID        string  `json:"id"`
	File      string  `json:"file"`
	Label     string  `json:"label"`
	Angle     int     `json:"angle"`     // ângulo informado por quem tirou a foto (90, 180, 270, 360)
	Direction string  `json:"direction"` // descrição da direção no modelo
	Aspect    float64 `json:"aspect"`    // largura / altura da imagem
	Note      string  `json:"note,omitempty"`
	Pose      Pose    `json:"pose"`
}

// Pose é a câmera que reproduz a foto dentro do modelo.
type Pose struct {
	Position Vec3    `json:"position"`
	Yaw      float64 `json:"yaw"`   // graus: 0 = norte, 90 = oeste, 180 = sul, -90 = leste
	Pitch    float64 `json:"pitch"` // graus, negativo olha para baixo
	Roll     float64 `json:"roll"`
	VFov     float64 `json:"vfov"` // campo de visão vertical em graus
}
