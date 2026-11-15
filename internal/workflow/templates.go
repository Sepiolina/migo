package workflow

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

// Starter workflows, one YAML file each in templates/. `migo new` writes a
// workflow from one, and Mai Studio offers them on its start page (through
// the catalog, like the steps). To add a template: add templates/<id>.yaml
// and an entry below, then run `make describe`. Every template must parse
// and run on its sample files (TestTemplatesRun).

//go:embed templates/*.yaml
var templateFS embed.FS

// TemplateInfo describes one starter workflow.
type TemplateInfo struct {
	ID          string `json:"id"`          // the file name in templates/, without .yaml
	Title       string `json:"title"`       // "Summarize by group"
	Description string `json:"description"` // one line: what the workflow does
	// Inputs is the file kind of each input, in order ("xlsx" or "csv"), and
	// Output the output format; both are read from the workflow itself.
	Inputs []string `json:"inputs"`
	Output string   `json:"output"`
	// Samples names a sample file for each input, relative to the repository
	// root, so the template runs as soon as it is created.
	Samples map[string]string `json:"samples"`
	Source  string            `json:"source"` // the workflow YAML
}

var templates = []TemplateInfo{
	{ID: "clean-sheet", Title: "Clean a sheet",
		Description: "Trim cells, drop blank and duplicate rows, and write a tidy copy.",
		Samples:     map[string]string{"sheet": "examples/parts.xlsx"}},
	{ID: "parts-summary", Title: "Summarize by group",
		Description: "Keep rows with a positive quantity, then total the quantity for each part.",
		Samples:     map[string]string{"parts": "examples/parts.xlsx"}},
	{ID: "lookup-enrich", Title: "Look up and enrich",
		Description: "Add each customer's region from a CSV list, then total sales per region.",
		Samples:     map[string]string{"orders": "examples/orders.xlsx", "regions": "examples/regions.csv"}},
	{ID: "orders-totals", Title: "Customer order totals",
		Description: "The tutorial: filter quantities, calculate line totals and total each customer.",
		Samples:     map[string]string{"orders": "examples/orders.xlsx"}},
	{ID: "excel-to-csv", Title: "Excel to CSV",
		Description: "Convert a sheet to a UTF-8 CSV file, for imports into other systems.",
		Samples:     map[string]string{"sheet": "examples/parts.xlsx"}},
	{ID: "blank", Title: "Blank workflow",
		Description: "One source, one output. Add steps from the table or the command menu.",
		Samples:     map[string]string{}},
}

// Templates returns the starter workflows in display order.
func Templates() []TemplateInfo {
	out := make([]TemplateInfo, len(templates))
	for i, t := range templates {
		data, err := templateFS.ReadFile(path.Join("templates", t.ID+".yaml"))
		if err != nil {
			panic(fmt.Sprintf("workflow: template %q: %v", t.ID, err))
		}
		wf, err := Parse(data)
		if err != nil {
			panic(fmt.Sprintf("workflow: template %q: %v", t.ID, err))
		}
		t.Source, t.Output, t.Inputs = string(data), wf.Format, []string{}
		for _, in := range wf.Inputs {
			kind := "xlsx"
			if strings.EqualFold(path.Ext(t.Samples[in.Name]), ".csv") {
				kind = "csv"
			}
			t.Inputs = append(t.Inputs, kind)
		}
		if t.Samples == nil {
			t.Samples = map[string]string{}
		}
		out[i] = t
	}
	return out
}

// Template returns the starter workflow with this id.
func Template(id string) (TemplateInfo, bool) {
	for _, t := range Templates() {
		if t.ID == id {
			return t, true
		}
	}
	return TemplateInfo{}, false
}
