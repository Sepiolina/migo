# Studio preview performance

## What changed

Revisiting a workflow step can reuse its focused trace. Studio retains up to 4 MiB of successful trace responses, measured as serialized UTF-8 JSON, and evicts the least recently used response when it needs space. A cache hit skips the runner and the edit debounce. Concurrent requests for the same entry share one result. Workflow edits, workspace or document changes, binding changes, new sample fingerprints, and skip changes discard the previous cache. Failed traces remain eligible for retry.

In a source preview, **Go to row** opens a window of up to 200 rows starting at the requested physical worksheet row. **First row** returns to the beginning. XLSX inspection streams formatted rows and retains the requested window. It still scans the worksheet to count the populated rows accurately, including cells beyond a stale dimension. Worksheets with unusual layouts, including duplicate rows, use Excelize's existing repair path.

## Benchmark

Run the following from `apps/studio` with Go, Python 3, and Chromium installed. The harness generates the large workbook in a temporary workspace and prints a JSON report. Use `--dist` and `--runner` to benchmark a previous production build with its matching runner.

```sh
npm run build
node scripts/bench-preview.mjs --iterations 5 --rows 100000 --check-budgets
```

The reference machine ran Linux x86-64 with a two-CPU quota on a Xeon Platinum 8573C, an 8 GiB memory limit, Chromium 154, and Go 1.26.10. Each trial used a fresh browser context and a fresh runner process per request. Cold open starts at selecting a workflow with saved bindings. The OS file cache stays warm between trials. With five trials, the reported p95 is the slowest observed value.

The BOM source has 7 physical rows and 53 columns and a separate priority input. Its second focus is step 15. Orders has 100,000 data rows plus a header, three columns, and a second focus at step 3. Both builds request the same 200-row source windows, starting at row 5 for BOM and row 90,000 for Orders. The comparable window measurement ends at the browser bridge response. The separate interactive check covers the new row field and rendered gutter at desktop and compact widths.

## Results

Times are p50 / p95 milliseconds. Runner time covers the trace or worksheet read, including process launch. Cold open also issues a validation request. Peak RSS is the highest sampled runner process high-water mark across the five trials. A dash indicates that the runner did not start.

| Fixture and action | Before, elapsed | After, elapsed | Calls per trial, before → after | Runner time, before → after | Peak runner MiB, before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| BOM · Cold open | 962 / 1,230 | 883 / 1,125 | 2 → 2 | 83 / 128 → 87 / 122 | 20.0 → 20.2 |
| BOM · Unseen step | 638 / 826 | 552 / 693 | 1 → 1 | 66 / 213 → 63 / 67 | 19.4 → 19.6 |
| BOM · Revisit first step | 702 / 750 | 122 / 167 | 1 → 0 | 75 / 108 → — | 19.3 → — |
| BOM · Revisit step 15 | 580 / 685 | 102 / 185 | 1 → 0 | 63 / 88 → — | 19.2 → — |
| BOM · Read source window | 48 / 102 | 26 / 41 | 1 → 1 | 41 / 95 → 22 / 38 | 16.2 → 14.7 |
| Orders · Cold open | 1,649 / 2,525 | 1,650 / 2,450 | 2 → 2 | 1,227 / 2,015 → 1,219 / 1,913 | 70.2 → 67.4 |
| Orders · Unseen step | 1,675 / 2,122 | 1,794 / 2,111 | 1 → 1 | 1,322 / 1,762 → 1,424 / 1,757 | 67.5 → 70.9 |
| Orders · Revisit first step | 1,528 / 2,014 | 61 / 64 | 1 → 0 | 1,189 / 1,654 → — | 69.3 → — |
| Orders · Revisit step 3 | 1,578 / 2,076 | 69 / 79 | 1 → 0 | 1,221 / 1,718 → — | 71.3 → — |
| Orders · Read source window | 4,134 / 4,729 | 1,859 / 2,627 | 1 → 1 | 4,118 / 4,708 → 1,850 / 2,618 | 338.0 → 20.4 |

For the first Orders step revisit, Chromium reported 28 / 37 ms of layout and style recalculation before the change and 19 / 19 ms after it. The baseline spent 1,189 / 1,654 ms in the trace runner for the same action. These timings favored trace reuse over grid virtualization. After the change, file fingerprint checks took 2 / 3 ms on cold Orders opens. Neither build requested fingerprints on step revisits. Studio continues to check file contents when the window regains focus.

The first visit to a step on a large input still executes the workflow. The median unseen Orders step took 1.68 s before and 1.79 s after in this run. The distant-window optimization reduces retained worksheet data, while full-sheet scanning still contributes to elapsed time. Each desktop request starts its own runner process.

## Release budgets

Both repeat visits must make zero runner calls. The p95 limit per revisit is 250 ms for BOM and 400 ms for Orders. Reading the distant Orders window has a 2,800 ms p95 limit and a 200 MiB peak runner RSS limit on Linux. The measured build passed these checks on the fixture sizes and machine class above.
