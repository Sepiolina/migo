package bom

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	"miservice/internal/config"
	"miservice/internal/logx"
)

// Recipe declares the ordered BOM stages and the files used by Run. Build
// leaves file selection to Pipeline.Process, as required by uploaded jobs.
type Recipe struct {
	Workflow string            `yaml:"workflow"`
	Engine   string            `yaml:"engine"`
	Input    map[string]string `yaml:"input"`
	Process  []RecipeStep      `yaml:"process"`
	Output   RecipeOutput      `yaml:"output"`
	BaseDir  string            `yaml:"-"`
}

type RecipeStep struct {
	Stage   string `yaml:"stage"`
	Enabled *bool  `yaml:"enabled,omitempty"`
}

type RecipeOutput struct {
	File string `yaml:"file"`
}

//go:embed default.yaml
var defaultRecipeYAML []byte

// DefaultRecipe returns an independent copy of the embedded legacy BOM recipe.
func DefaultRecipe() (*Recipe, error) { return ParseRecipe(defaultRecipeYAML, ".") }

// LoadRecipe resolves file paths relative to the recipe's containing directory.
func LoadRecipe(path string) (*Recipe, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRecipe(data, filepath.Dir(path))
}

// ParseRecipe accepts one strict YAML mapping. Aliases and merge keys are
// deliberately unsupported so every declared stage is explicit and reviewable.
func ParseRecipe(data []byte, baseDir string) (*Recipe, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("BOM recipe: %w", err)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("BOM recipe: %w", err)
		}
		return nil, fmt.Errorf("BOM recipe: multiple YAML documents are not supported")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("BOM recipe must be a mapping")
	}
	if err := checkRecipeNode(doc.Content[0]); err != nil {
		return nil, err
	}
	decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var recipe Recipe
	if err := decoder.Decode(&recipe); err != nil {
		return nil, fmt.Errorf("BOM recipe: %w", err)
	}
	var err error
	recipe.BaseDir, err = filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := recipe.Validate(); err != nil {
		return nil, err
	}
	return &recipe, nil
}

func checkRecipeNode(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("BOM recipe line %d: YAML aliases are not supported", node.Line)
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return fmt.Errorf("BOM recipe line %d: mapping keys must be strings; merge keys are not supported", key.Line)
			}
			if seen[key.Value] {
				return fmt.Errorf("BOM recipe line %d: duplicate key %q", key.Line, key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := checkRecipeNode(child); err != nil {
			return err
		}
	}
	return nil
}

func (s RecipeStep) active() bool { return s.Enabled == nil || *s.Enabled }

// RecipeStageRegistry is the extension point for YAML-selectable BOM stages.
// Register additional primitives here without changing DefaultStages, which
// remains the original Go pipeline and the equivalence reference. A primitive
// runs only when a recipe selects it; declare its prerequisites in Validate.
func RecipeStageRegistry(deps Deps) map[string]Stage {
	registry := make(map[string]Stage)
	for _, stage := range DefaultStages(deps) {
		registry[stage.Name] = stage
	}
	return registry
}

// Validate checks executable dependencies, including optional enrichment: if
// material-master or sap-enrich is enabled it must precede cft-002-priority.
// Disabled steps are excluded from execution but still checked for typos and
// duplicates. Read and write are mandatory; other stages may be omitted when
// none of the enabled stages depend on them.
func (r *Recipe) Validate() error {
	if r == nil {
		return fmt.Errorf("BOM recipe is nil")
	}
	if strings.TrimSpace(r.Workflow) == "" {
		return fmt.Errorf("BOM recipe: workflow must not be blank")
	}
	if r.Engine != "bom" {
		return fmt.Errorf("BOM recipe: engine must be bom")
	}
	if len(r.Input) != 2 || strings.TrimSpace(r.Input["bom"]) == "" || strings.TrimSpace(r.Input["priority"]) == "" {
		return fmt.Errorf("BOM recipe: input requires exactly nonblank bom and priority paths")
	}
	if strings.TrimSpace(r.Output.File) == "" || !strings.EqualFold(filepath.Ext(r.Output.File), ".xlsx") {
		return fmt.Errorf("BOM recipe: output.file must be an .xlsx path")
	}
	known := map[string]bool{}
	for _, stage := range RecipeStageRegistry(Deps{}) {
		known[stage.Name] = true
	}
	seen, active := map[string]bool{}, map[string]int{}
	for i, step := range r.Process {
		if !known[step.Stage] {
			return fmt.Errorf("BOM recipe process %d: unknown stage %q", i+1, step.Stage)
		}
		if seen[step.Stage] {
			return fmt.Errorf("BOM recipe process %d: duplicate stage %q", i+1, step.Stage)
		}
		seen[step.Stage] = true
		if step.active() {
			active[step.Stage] = i
		}
	}
	if len(r.Process) == 0 || r.Process[0].Stage != "read-inputs" || !r.Process[0].active() {
		return fmt.Errorf("BOM recipe: enabled read-inputs must be the first process step")
	}
	if r.Process[len(r.Process)-1].Stage != "write-workbook" || !r.Process[len(r.Process)-1].active() {
		return fmt.Errorf("BOM recipe: enabled write-workbook must be the last process step")
	}
	dependencies := map[string][]string{
		"pcb-scope":             {"group-rows"},
		"pcb-reformat":          {"pcb-scope"},
		"cft-001-vendor-policy": {"pcb-reformat"},
		"material-master":       {"cft-001-vendor-policy"},
		"sap-enrich":            {"material-master"},
		"cft-002-priority":      {"group-rows", "pcb-scope", "cft-001-vendor-policy"},
	}
	for _, name := range []string{"material-master", "sap-enrich"} {
		if _, enabled := active[name]; enabled {
			dependencies["cft-002-priority"] = append(dependencies["cft-002-priority"], name)
		}
	}
	for i, step := range r.Process {
		if !step.active() {
			continue
		}
		for _, dep := range dependencies[step.Stage] {
			position, exists := active[dep]
			if !exists || position >= i {
				return fmt.Errorf("BOM recipe process %d (%s): requires enabled %s before it", i+1, step.Stage, dep)
			}
		}
	}
	return nil
}

// Build compiles the recipe's enabled stages into a pipeline, using the same
// domain functions and injected dependencies as the original Go pipeline.
func (r *Recipe) Build(cfg *config.Config, log logx.Logger, deps Deps) (*Pipeline, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	registry := RecipeStageRegistry(deps)
	pipeline := &Pipeline{Cfg: cfg, Log: log}
	for i, step := range r.Process {
		if !step.active() {
			continue
		}
		stage, index := registry[step.Stage], i+1
		pipeline.Stages = append(pipeline.Stages, Stage{Name: stage.Name, Run: func(ctx context.Context, job *Job) error {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("BOM recipe process %d (%s): %w", index, stage.Name, err)
			}
			if err := stage.Run(ctx, job); err != nil {
				return fmt.Errorf("BOM recipe process %d (%s): %w", index, stage.Name, err)
			}
			return nil
		}})
	}
	return pipeline, nil
}

// Run uses the recipe's configured paths, protects both inputs against output
// overwrites (including symlinks and hard links), and never creates directories.
func (r *Recipe) Run(ctx context.Context, cfg *config.Config, log logx.Logger, deps Deps) error {
	return r.run(ctx, cfg, log, deps, true)
}

// Check executes and renders the recipe without publishing the workbook.
func (r *Recipe) Check(ctx context.Context, cfg *config.Config, log logx.Logger, deps Deps) error {
	return r.run(ctx, cfg, log, deps, false)
}

func (r *Recipe) run(ctx context.Context, cfg *config.Config, log logx.Logger, deps Deps, publish bool) error {
	pipeline, err := r.Build(cfg, log, deps)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resolve := func(path string) string {
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		return filepath.Join(r.BaseDir, path)
	}
	bomPath, priorityPath, outPath := resolve(r.Input["bom"]), resolve(r.Input["priority"]), resolve(r.Output.File)
	outputInfo, outputErr := os.Stat(outPath)
	if outputErr != nil && !os.IsNotExist(outputErr) {
		return fmt.Errorf("BOM recipe output: %w", outputErr)
	}
	for _, path := range []string{bomPath, priorityPath} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("BOM recipe input %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("BOM recipe input %q must be a regular file", path)
		}
		if outputInfo != nil && os.SameFile(info, outputInfo) {
			return fmt.Errorf("BOM recipe: output.file would overwrite input %q", path)
		}
	}
	parent, err := os.Stat(filepath.Dir(outPath))
	if err != nil {
		return fmt.Errorf("BOM recipe output directory: %w", err)
	}
	if !parent.IsDir() {
		return fmt.Errorf("BOM recipe output directory is not a directory")
	}
	temp, err := os.CreateTemp(filepath.Dir(outPath), ".bom-recipe-*.xlsx")
	if err != nil {
		return fmt.Errorf("BOM recipe output: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Close(); err != nil {
		return err
	}
	pipeline.Log = func(line string) {
		if line != "Successfully processed and generated: "+tempPath {
			log.Print(line)
		}
	}
	if err := pipeline.Process(ctx, bomPath, priorityPath, tempPath); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !publish {
		return nil
	}
	if err := os.Rename(tempPath, outPath); err != nil {
		return fmt.Errorf("BOM recipe publish output: %w", err)
	}
	log.Printf("Successfully processed and generated: %s", outPath)
	return nil
}
