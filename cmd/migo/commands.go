package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Sepiolina/migo/engine/wftest"
	"github.com/Sepiolina/migo/engine/workflow"
)

const usage = `migo creates, checks, runs and tests workflow files, without a server.

  migo new <name> [-template id] [-dir workflows]    create a workflow from a template (-list shows them)
  migo validate <workflow.yaml>...                   check workflow files without running them
  migo run [-in name=path]... -out <file> <workflow.yaml>
                                                     run on local files (-check keeps no output)
  migo test [-update] [path]...                      run the *.test.yaml files at the paths (default: .)
  migo describe | schema | docs [-out file]          the step catalog (JSON), a JSON Schema for editors,
                                                     the step reference (Markdown)

"migo <command> -h" lists a command's flags. "migo [flags] workflow.yaml" is the same as "migo run".
Guide: docs/CLI.md
`

// commands are the subcommands besides run (the default) and the generators.
var commands = map[string]func(ctx context.Context, args []string, stdout, stderr io.Writer) int{
	"new":      newWorkflow,
	"validate": validate,
	"test":     test,
	"help": func(_ context.Context, _ []string, stdout, _ io.Writer) int {
		fmt.Fprint(stdout, usage)
		return 0
	},
}

// parse reads flags that may come before, between and after the arguments.
// It returns the arguments and 0, or an exit code (ok false) when done.
func parse(flags *flag.FlagSet, args []string, stderr io.Writer) ([]string, int, bool) {
	flags.SetOutput(stderr)
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, 0, false
			}
			return nil, 2, false
		}
		if flags.NArg() == 0 {
			return positional, 0, true
		}
		positional, args = append(positional, flags.Arg(0)), flags.Args()[1:]
	}
}

var workflowFileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// newWorkflow writes <dir>/<name>.yaml from a starter template.
func newWorkflow(_ context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migo new", flag.ContinueOnError)
	template := flags.String("template", "blank", "the template to start from (see -list)")
	dir := flags.String("dir", "workflows", "the folder to create the workflow in")
	list := flags.Bool("list", false, "list the templates and exit")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: migo new <name> [-template id] [-dir workflows]\n       migo new -list")
		flags.PrintDefaults()
	}
	names, code, ok := parse(flags, args, stderr)
	if !ok {
		return code
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "ERR", err)
		return 2
	}
	if *list {
		for _, t := range workflow.Templates() {
			fmt.Fprintf(stdout, "%-14s %s\n", t.ID, t.Description)
		}
		return 0
	}
	if len(names) != 1 {
		return fail(fmt.Errorf("give one workflow name, e.g. migo new order-totals -template parts-summary"))
	}
	name := strings.TrimSuffix(names[0], ".yaml")
	if !workflowFileName.MatchString(name) {
		return fail(fmt.Errorf("name %q: use lowercase letters, digits and hyphens (1-40 characters); it becomes the route /api/%s", name, name))
	}
	t, found := workflow.Template(*template)
	if !found {
		var ids []string
		for _, t := range workflow.Templates() {
			ids = append(ids, t.ID)
		}
		return fail(fmt.Errorf("no template %q; choose one of: %s", *template, strings.Join(ids, ", ")))
	}
	path := filepath.Join(*dir, name+".yaml")
	if _, err := os.Stat(path); err == nil {
		return fail(fmt.Errorf("%s already exists", path))
	}
	// The workflow's display name follows the file name: order-totals -> Order totals.
	title := strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "-", " ")
	_, rest, _ := strings.Cut(t.Source, "\n")
	source := "name: " + title + "\n" + rest
	if _, err := workflow.Parse([]byte(source)); err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return fail(err)
	}
	wf, _ := workflow.Parse([]byte(source))
	run := "migo run"
	for _, in := range wf.Inputs {
		sample := t.Samples[in.Name]
		if sample == "" {
			sample = "<" + in.Label + ">"
		}
		run += fmt.Sprintf(" -in %s=%s", in.Name, quote(sample))
	}
	fmt.Fprintf(stdout, "created %s from the template %q\n\nnext:\n  migo validate %s\n  %s -out out%s %s\n", path, t.ID, path, run, wf.Ext(), path)
	fmt.Fprintf(stdout, "\nIt is served at /api/%s once it is in the service's workflows folder. Steps: docs/STEP_REFERENCE.md\n", name)
	return 0
}

func quote(s string) string {
	if strings.ContainsAny(s, " <>") {
		return `"` + s + `"`
	}
	return s
}

// validate compiles workflow files with the production parser, running nothing.
func validate(_ context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migo validate", flag.ContinueOnError)
	flags.Usage = func() { fmt.Fprintln(stderr, "usage: migo validate <workflow.yaml>...") }
	files, code, ok := parse(flags, args, stderr)
	if !ok {
		return code
	}
	if len(files) == 0 {
		flags.Usage()
		return 2
	}
	failed := 0
	for _, f := range files {
		wf, err := workflow.Load(f)
		if err != nil {
			failed++
			fmt.Fprintf(stdout, "FAIL  %s\n      %v\n", f, err)
			continue
		}
		var inputs, sheets []string
		for _, in := range wf.Inputs {
			inputs = append(inputs, in.Name)
		}
		for _, sh := range wf.Sheets {
			sheets = append(sheets, sh.Name)
		}
		fmt.Fprintf(stdout, "ok    %s  (inputs: %s; %s output: %s)\n", f, strings.Join(inputs, ", "), wf.Format, strings.Join(sheets, ", "))
	}
	if failed > 0 {
		return 1
	}
	return 0
}

// test runs workflow test files (see engine/wftest).
func test(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("migo test", flag.ContinueOnError)
	update := flags.Bool("update", false, "write the outputs as the expected files instead of failing on a difference")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: migo test [-update] [path]...   (a path is a *.test.yaml file or a folder searched for them)")
		flags.PrintDefaults()
	}
	paths, code, ok := parse(flags, args, stderr)
	if !ok {
		return code
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	files, err := wftest.Find(paths)
	if err == nil && len(files) == 0 {
		err = fmt.Errorf("no *%s files at %s", wftest.Suffix, strings.Join(paths, ", "))
	}
	if err != nil {
		fmt.Fprintln(stderr, "ERR", err)
		return 2
	}
	sort.Strings(files)
	passed, failed, updated := 0, 0, 0
	for _, path := range files {
		fmt.Fprintln(stdout, path)
		f, err := wftest.Load(path)
		if err != nil {
			failed++
			fmt.Fprintf(stdout, "  FAIL  %v\n", err)
			continue
		}
		for _, r := range f.Run(ctx, *update) {
			switch {
			case r.Err != nil:
				failed++
				fmt.Fprintf(stdout, "  FAIL  %s\n        %v\n", r.Name, r.Err)
			case r.Updated:
				updated++
				fmt.Fprintf(stdout, "  new   %s (expected file written; check it before committing)\n", r.Name)
			default:
				passed++
				fmt.Fprintf(stdout, "  ok    %s\n", r.Name)
			}
		}
	}
	fmt.Fprintf(stdout, "\n%d passed, %d failed", passed, failed)
	if updated > 0 {
		fmt.Fprintf(stdout, ", %d written", updated)
	}
	fmt.Fprintln(stdout)
	if failed > 0 {
		return 1
	}
	return 0
}
