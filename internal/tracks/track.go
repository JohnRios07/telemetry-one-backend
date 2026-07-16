package tracks

type Point struct {
	Index             int     `json:"index"`
	X                 float64 `json:"x"`
	Y                 float64 `json:"y"`
	Z                 float64 `json:"z"`
	AccumulatedMeters float64 `json:"accumulatedMeters"`
	HeadingRadians    float64 `json:"headingRadians"`
	Curvature         float64 `json:"curvature"`
}

type Track struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	LayoutName   string      `json:"layoutName"`
	Country      string      `json:"country"`
	LengthMeters float64     `json:"lengthMeters"`
	CenterLine   []Point     `json:"centerLine"`
	Sectors      []Sector    `json:"sectors"`
	Corners      []Corner    `json:"corners"`
	Fingerprint  Fingerprint `json:"fingerprint"`
}

type Sector struct {
	Number      int      `json:"number"`
	Name        string   `json:"name"`
	StartMeters float64  `json:"startMeters"`
	EndMeters   float64  `json:"endMeters"`
	Sources     []Source `json:"sources,omitempty"`
}

type CornerDefinitionMode string

const (
	CornerDefinitionCatalogManual      CornerDefinitionMode = "catalog_manual"
	CornerDefinitionCatalogEnumerated  CornerDefinitionMode = "catalog_enumerated"
	CornerDefinitionAutoDetectedFuture CornerDefinitionMode = "auto_detected_future"
)

type Corner struct {
	ID             string               `json:"id"`
	Number         int                  `json:"number"`
	Name           string               `json:"name"`
	DefinitionMode CornerDefinitionMode `json:"definitionMode"`
	StartMeters    float64              `json:"startMeters"`
	ApexMeters     float64              `json:"apexMeters"`
	EndMeters      float64              `json:"endMeters"`
	Direction      string               `json:"direction"`
	RadiusMeters   float64              `json:"radiusMeters"`
	Severity       string               `json:"severity"`
	Confidence     float64              `json:"confidence"`
	Sources        []Source             `json:"sources,omitempty"`
}

type Fingerprint struct {
	NormalizedLength float64   `json:"normalizedLength"`
	SampleCount      int       `json:"sampleCount"`
	Signature        []float64 `json:"signature"`
	ElevationProfile []float64 `json:"elevationProfile"`
}
