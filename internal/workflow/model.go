// Package workflow executes declarative, values-based Excel recipes.
// The existing domain-specific BOM pipeline remains available independently.
package workflow

// Config is a resolved recipe. Load resolves imports and presets before Run.
type Config struct {
	Workflow string            `yaml:"workflow"`
	Input    map[string]string `yaml:"input"`
	Process  []Process         `yaml:"process"`
	Output   []Output          `yaml:"output"`
	BaseDir  string            `yaml:"-"`
}

type Process struct {
	From      string `yaml:"from"`
	As        string `yaml:"as"`
	HeaderRow int    `yaml:"header_row,omitempty"`
	Do        []Step `yaml:"do"`
}

type Output struct {
	File     string            `yaml:"file"`
	Template string            `yaml:"template,omitempty"`
	Sheets   map[string]string `yaml:"sheets"`
}

// Step has exactly one operation. Enabled defaults to true.
type Step struct {
	Enabled     *bool          `yaml:"enabled,omitempty"`
	Map         *MapSpec       `yaml:"map,omitempty"`
	Filter      *FilterSpec    `yaml:"filter,omitempty"`
	ForwardFill *ColumnsSpec   `yaml:"forward_fill,omitempty"`
	Group       *GroupSpec     `yaml:"group,omitempty"`
	Aggregate   *AggregateSpec `yaml:"aggregate,omitempty"`
	Sort        *SortSpec      `yaml:"sort,omitempty"`
	Deduplicate *ColumnsSpec   `yaml:"deduplicate,omitempty"`
	Calculate   *CalculateSpec `yaml:"calculate,omitempty"`
	Transform   *TransformSpec `yaml:"transform,omitempty"`
	Join        *JoinSpec      `yaml:"join,omitempty"`
	Merge       *MergeSpec     `yaml:"merge,omitempty"`
	Compare     *CompareSpec   `yaml:"compare,omitempty"`
	Style       *StyleSpec     `yaml:"style,omitempty"`
}

type ColumnsSpec struct {
	Columns []string `yaml:"columns"`
}
type Mapping struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
	Move bool   `yaml:"move,omitempty"`
}
type MapSpec struct {
	Columns []Mapping `yaml:"columns"`
	Keep    bool      `yaml:"keep,omitempty"`
}
type FilterSpec struct {
	All            []FilterSpec `yaml:"all,omitempty"`
	Any            []FilterSpec `yaml:"any,omitempty"`
	Column         string       `yaml:"column,omitempty"`
	Equal          *string      `yaml:"equal,omitempty"`
	NotEqual       *string      `yaml:"not_equal,omitempty"`
	GreaterThan    *float64     `yaml:"greater_than,omitempty"`
	GreaterOrEqual *float64     `yaml:"greater_or_equal,omitempty"`
	LessThan       *float64     `yaml:"less_than,omitempty"`
	LessOrEqual    *float64     `yaml:"less_or_equal,omitempty"`
	Contains       *string      `yaml:"contains,omitempty"`
	In             []string     `yaml:"in,omitempty"`
	Blank          *bool        `yaml:"blank,omitempty"`
}
type GroupSpec struct {
	By        []string          `yaml:"by"`
	Aggregate map[string]string `yaml:"aggregate,omitempty"`
}
type AggregateSpec struct {
	Columns map[string]string `yaml:"columns"`
}
type SortSpec struct {
	By      []string `yaml:"by"`
	Order   string   `yaml:"order,omitempty"`
	Numeric bool     `yaml:"numeric,omitempty"`
}
type CalculateSpec struct {
	Column    string   `yaml:"column"`
	Operation string   `yaml:"operation"`
	Columns   []string `yaml:"columns"`
	Value     *float64 `yaml:"value,omitempty"`
}
type TransformSpec struct {
	Columns   []string `yaml:"columns"`
	Operation string   `yaml:"operation"`
	Old       string   `yaml:"old,omitempty"`
	New       string   `yaml:"new,omitempty"`
}
type JoinSpec struct {
	With      string   `yaml:"with"`
	On        []string `yaml:"on"`
	RightOn   []string `yaml:"right_on,omitempty"`
	How       string   `yaml:"how,omitempty"`
	Normalize bool     `yaml:"normalize,omitempty"`
	Suffix    string   `yaml:"suffix,omitempty"`
}
type MergeSpec struct {
	With []string `yaml:"with"`
}
type CompareSpec struct {
	With         string   `yaml:"with"`
	By           []string `yaml:"by"`
	Columns      []string `yaml:"columns,omitempty"`
	Normalize    bool     `yaml:"normalize,omitempty"`
	StatusColumn string   `yaml:"status_column,omitempty"`
}
type HeaderStyle struct {
	Bold       bool   `yaml:"bold,omitempty"`
	Background string `yaml:"background,omitempty"`
	Color      string `yaml:"color,omitempty"`
}
type StyleSpec struct {
	Header        *HeaderStyle      `yaml:"header,omitempty"`
	AutoWidth     bool              `yaml:"auto_width,omitempty"`
	FreezeHeader  bool              `yaml:"freeze_header,omitempty"`
	NumberFormats map[string]string `yaml:"number_formats,omitempty"`
}

// Table contains unique headers and rectangular string rows. Input formulas
// become cached values; output templates retain their untouched formulas.
type Table struct {
	Columns []string
	Rows    [][]string
	Style   *StyleSpec
}
