# Conformance suite

Each `.test.yaml` file names a workflow, and each case gives the inputs and
either the exact output `migo` writes or the error it stops with. Any
program that uses the engine, or claims to run workflows the way `migo`
does, must pass every case unchanged.

```sh
go run ./cmd/migo test conformance
```

Outputs are compared by sheet names and every cell value, or by text for
CSV. `bom.test.yaml` also checks the BOM workflow against
`testdata/golden_random7_nosap.xlsx`, a reference from the earlier BOM
implementation.

The suite covers:

- every step type;
- the JavaScript rules for numbers and text;
- `.xlsx`, `.xls` and `.csv` inputs;
- `.xlsx` and `.csv` outputs;
- mocked `sql` and `rest` steps;
- errors;
- the Studio templates.

When an engine change alters an output on purpose, update the expected
files and review the difference before you commit:

```sh
go run ./cmd/migo test -update conformance
```

`make engine-kit` puts this folder in the engine kit zip.
