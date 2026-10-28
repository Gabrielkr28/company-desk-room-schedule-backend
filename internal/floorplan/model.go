package floorplan

// Model é o andar resolvido: tudo em coordenadas do mundo, pronto para renderizar.
type Model struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Building      string   `json:"building"`
	Version       string   `json:"version"`
	Width         float64  `json:"width"`
	Depth         float64  `json:"depth"`
	CeilingHeight float64  `json:"ceilingHeight"`
	Walls         []Wall   `json:"walls"`
	Columns       []Column `json:"columns"`
	Benches       []Bench  `json:"benches"`
	Seats         []Seat   `json:"seats"`
	Items         []Item   `json:"items"`
	Photos        []Photo  `json:"photos"`
	Stats         Stats    `json:"stats"`
	Notes         []string `json:"notes"`
	Warnings      []string `json:"warnings"`
	ReloadError   string   `json:"reloadError,omitempty"`
}

type Wall struct {
	ID         string     `json:"id"`
	Name       string     `json:"name,omitempty"`
	Start      Vec2       `json:"start"`
	End        Vec2       `json:"end"`
	Length     float64    `json:"length"`
	Height     float64    `json:"height"`
	Thickness  float64    `json:"thickness"`
	Color      string     `json:"color"`
	Inward     Vec2       `json:"inward"` // normal unitária apontando para dentro do andar
	Confidence Confidence `json:"confidence"`
	Openings   []Opening  `json:"openings"`
}

type Opening struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"`
	Along  float64 `json:"along"` // distância do início da parede até o centro do vão
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Label  string  `json:"label,omitempty"`
	Color  string  `json:"color,omitempty"`
}

type Column struct {
	ID         string     `json:"id"`
	Position   Vec2       `json:"position"`
	Size       Vec2       `json:"size"`
	Height     float64    `json:"height"`
	Style      string     `json:"style"`
	Label      string     `json:"label,omitempty"`
	Confidence Confidence `json:"confidence"`
}

type Bench struct {
	ID           string     `json:"id"`
	Row          string     `json:"row,omitempty"`
	Bay          int        `json:"bay,omitempty"`
	Center       Vec2       `json:"center"`
	Rotation     float64    `json:"rotation"`
	Length       float64    `json:"length"`
	SideDepth    float64    `json:"sideDepth"`
	SeatWidth    float64    `json:"seatWidth"`
	SeatsPerSide int        `json:"seatsPerSide"`
	Sides        []string   `json:"sides"`
	SeatCount    int        `json:"seatCount"`
	Confidence   Confidence `json:"confidence"`
}

// Seat é um lugar de trabalho. ID estável no formato <bancada>-<lado><n> (ex.: I6-S1).
type Seat struct {
	ID         string     `json:"id"`
	BenchID    string     `json:"benchId"`
	Side       string     `json:"side"`
	Index      int        `json:"index"`
	Position   Vec2       `json:"position"`   // onde fica a cadeira
	DeskCenter Vec2       `json:"deskCenter"` // centro do tampo deste lugar
	DeskSize   Vec2       `json:"deskSize"`   // [largura ao longo da bancada, profundidade]
	Yaw        float64    `json:"yaw"`        // para onde a pessoa olha (0 = norte)
	Monitors   int        `json:"monitors"`
	Laptop     bool       `json:"laptop"`
	Note       string     `json:"note,omitempty"`
	Confidence Confidence `json:"confidence"`
}

// Item é qualquer objeto decorativo/funcional; os de parede trazem WallID.
type Item struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	WallID   string  `json:"wallId,omitempty"`
	Position Vec3    `json:"position"` // centro do objeto
	Yaw      float64 `json:"yaw"`      // para onde a frente do objeto aponta
	Size     Vec3    `json:"size"`     // largura, altura, profundidade
	Label    string  `json:"label,omitempty"`
	Color    string  `json:"color,omitempty"`
}

type Photo struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Angle     int     `json:"angle"`
	Direction string  `json:"direction"`
	Aspect    float64 `json:"aspect"`
	Note      string  `json:"note,omitempty"`
	Pose      Pose    `json:"pose"`
	URL       string  `json:"url"`
	Available bool    `json:"available"`
}

type Stats struct {
	Benches      int                `json:"benches"`
	Seats        int                `json:"seats"`
	Columns      int                `json:"columns"`
	ByConfidence map[Confidence]int `json:"byConfidence"`
}

// Summary é a versão curta usada na listagem de andares.
type Summary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Building string `json:"building"`
	Seats    int    `json:"seats"`
	Error    string `json:"error,omitempty"`
}
