package workflow

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Service is how the HTTP service exposes a workflow. Every field is
// optional; the zero value keeps the original behaviour:
//
//	service:
//	  route: bom-v2            # URL segment (default: the file name)
//	  description: Annotates a customer BOM with Forth P/N and MAIN/SUB.
//	  owner_field: bam         # form field with the requester's id (default bam)
//	  owner_required: false    # default true
//	  max_upload_mb: 40        # per-file upload limit (default: the service's)
type Service struct {
	Route         string
	Description   string
	OwnerField    string
	OwnerRequired bool
	MaxUploadMB   int
}

// Routes are lowercase letters, digits and '-', like workflow file names.
var routeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)

// ValidRoute reports whether name can be a workflow URL segment.
func ValidRoute(name string) bool { return routeName.MatchString(name) }

func parseService(n *yaml.Node) (Service, error) {
	svc := Service{OwnerField: "bam", OwnerRequired: true}
	if n == nil || n.Kind == 0 {
		return svc, nil
	}
	var spec struct {
		Route         string `yaml:"route"`
		Description   string `yaml:"description"`
		OwnerField    string `yaml:"owner_field"`
		OwnerRequired *bool  `yaml:"owner_required"`
		MaxUploadMB   int    `yaml:"max_upload_mb"`
	}
	if err := strictDecode(n, &spec); err != nil {
		return svc, fmt.Errorf("service: %w", err)
	}
	if spec.Route != "" && !ValidRoute(spec.Route) {
		return svc, fmt.Errorf("service.route %q: use lowercase letters, digits and hyphens (1-40 characters)", spec.Route)
	}
	if spec.MaxUploadMB < 0 || spec.MaxUploadMB > 1024 {
		return svc, fmt.Errorf("service.max_upload_mb must be between 1 and 1024")
	}
	if f := strings.TrimSpace(spec.OwnerField); f != "" {
		if f == "files" {
			return svc, fmt.Errorf("service.owner_field must not be the upload field \"files\"")
		}
		svc.OwnerField = f
	}
	svc.Route = spec.Route
	svc.Description = strings.TrimSpace(spec.Description)
	svc.MaxUploadMB = spec.MaxUploadMB
	if spec.OwnerRequired != nil {
		svc.OwnerRequired = *spec.OwnerRequired
	}
	return svc, nil
}

// ---- step registry --------------------------------------------------------------

// TableFunc transforms the current table. It receives a copy, so it may
// change and return it.
type TableFunc func(t *Table) (*Table, error)

// RegisterTableStep adds a generic table step from outside this package,
// e.g. in an init function of a package linked into the service:
//
//	func init() {
//		workflow.RegisterTableStep("upper", func(arg *yaml.Node) (workflow.TableFunc, error) {
//			...
//		})
//	}
//
// compile runs when a workflow file loads, so argument errors stop startup
// rather than failing a request. Registering a name twice panics.
func RegisterTableStep(name string, compile func(arg *yaml.Node) (TableFunc, error)) {
	if _, dup := ops[name]; dup || name == "if" || !inputName.MatchString(name) {
		panic(fmt.Sprintf("workflow: step %q is already registered or invalid", name))
	}
	ops[name] = tableStep(func(arg *yaml.Node, _ *scope) (tableFn, error) {
		fn, err := compile(arg)
		if err != nil {
			return nil, err
		}
		return func(t *Table, _ *State) (*Table, error) { return fn(t) }, nil
	})
}

// StepNames lists every step a workflow may use, sorted (plus "if").
func StepNames() []string {
	names := []string{"if"}
	for name := range ops {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
