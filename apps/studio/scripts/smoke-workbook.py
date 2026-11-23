"""Compare the Go runner's workbook pages with the saved BOM's OOXML.

Run from any directory with Python 3 and Go, or pass --runner for an existing binary.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import posixpath
import re
import shutil
import subprocess
import tempfile
from xml.etree import ElementTree
from zipfile import ZipFile

REPO = Path(__file__).resolve().parents[3]
NS = {"m": "http://schemas.openxmlformats.org/spreadsheetml/2006/main"}
REL_NS = "{http://schemas.openxmlformats.org/officeDocument/2006/relationships}"
BORDER_NAMES = {1: "thin", 2: "medium", 5: "thick"}


def require_equal(actual, expected, label):
    if actual != expected:
        raise AssertionError(f"{label}: expected {expected!r}, got {actual!r}")


def request(runner, workspace, command, **params):
    response = json.loads(subprocess.check_output(
        [runner],
        input=json.dumps({
            "version": 1, "id": command, "command": command,
            "params": {"workspace": str(workspace), **params},
        }) + "\n",
        text=True,
        timeout=60,
    ))
    if not response["ok"]:
        raise RuntimeError(response["error"])
    return response["result"]


def spreadsheet_color(element):
    if element is None:
        return None
    rgb = element.get("rgb")
    return f"#{rgb[-6:]}" if rgb else None


def read_style(styles, style_id):
    xf = styles.find("m:cellXfs", NS)[style_id]
    font = styles.find("m:fonts", NS)[int(xf.get("fontId"))]
    fill = styles.find("m:fills", NS)[int(xf.get("fillId"))].find("m:patternFill", NS)
    border = styles.find("m:borders", NS)[int(xf.get("borderId"))]
    alignment = xf.find("m:alignment", NS)
    return {
        "bold": font.find("m:b", NS) is not None,
        "fontFamily": font.find("m:name", NS).get("val"),
        "fontSize": float(font.find("m:sz", NS).get("val")),
        "fontColor": spreadsheet_color(font.find("m:color", NS)),
        "fillColor": spreadsheet_color(fill.find("m:fgColor", NS)),
        "horizontal": alignment.get("horizontal"),
        "vertical": alignment.get("vertical"),
        "borders": {
            side: (edge.get("style"), spreadsheet_color(edge.find("m:color", NS)))
            for side in ("left", "right", "top", "bottom")
            if (edge := border.find(f"m:{side}", NS)).get("style")
        },
    }


def column_number(letters):
    number = 0
    for letter in letters:
        number = number * 26 + ord(letter) - ord("A") + 1
    return number


def read_reference(path):
    with ZipFile(path) as book:
        def xml(part):
            return ElementTree.fromstring(book.read(part))

        styles = xml("xl/styles.xml")
        strings = [
            "".join(text.text or "" for text in item.findall(".//m:t", NS))
            for item in xml("xl/sharedStrings.xml")
        ]
        targets = {
            rel.get("Id"): posixpath.normpath(posixpath.join("xl", rel.get("Target"))).lstrip("/")
            for rel in xml("xl/_rels/workbook.xml.rels")
        }
        sheets = {}
        for sheet in xml("xl/workbook.xml").findall("m:sheets/m:sheet", NS):
            document = xml(targets[sheet.get(f"{REL_NS}id")])
            sheet_format = document.find("m:sheetFormatPr", NS)
            default_height = float(sheet_format.get("defaultRowHeight", "15")) if sheet_format is not None else 15
            heights = {
                int(row.get("r")): float(row.get("ht", default_height))
                for row in document.findall("m:sheetData/m:row", NS)
            }
            widths = {}
            for col in document.findall("m:cols/m:col", NS):
                for index in range(int(col.get("min")), int(col.get("max")) + 1):
                    widths[index] = float(col.get("width"))

            cells = {}
            for cell in document.findall("m:sheetData/m:row/m:c", NS):
                address = cell.get("r")
                column, row = re.fullmatch(r"([A-Z]+)([0-9]+)", address).groups()
                value = cell.findtext("m:v", "", NS)
                if cell.get("t") == "s":
                    value = strings[int(value)]
                style = read_style(styles, int(cell.get("s", "0")))
                cells[int(row), column_number(column)] = (address, value, style)
            sheets[sheet.get("name")] = widths, cells, heights, default_height
        return sheets


def compare_page(runner, workspace, path, sheets, name, offset, limit, column_offset, column_limit):
    page = request(runner, workspace, "inspectWorkbookPage", path=path, sheet=name,
                   offset=offset, limit=limit, columnOffset=column_offset, columnLimit=column_limit)
    widths, cells, heights, default_height = sheets[name]
    row_count = max(row for row, _ in cells)
    column_count = max(widths)
    label = f"{name} page ({offset}, {column_offset})"
    require_equal(page["sheets"], list(sheets), f"{label} sheet list")
    require_equal(page["sheet"], name, f"{label} active sheet")
    require_equal(page["totalRows"], row_count, f"{label} total rows")
    require_equal(page["totalColumns"], column_count, f"{label} total columns")
    require_equal(page["offset"], offset, f"{label} row offset")
    require_equal(page["columnOffset"], column_offset, f"{label} column offset")
    row_range = range(offset + 1, min(row_count, offset + limit) + 1)
    column_range = range(column_offset + 1, min(column_count, column_offset + column_limit) + 1)
    require_equal(page["widths"], [widths[col] for col in column_range], f"{label} widths")
    require_equal(page.get("rowHeights", [15] * len(row_range)),
                  [heights.get(row, default_height) for row in row_range], f"{label} heights")
    require_equal(len(page["rows"]), len(row_range), f"{label} row count")
    require_equal(len(page["styleIds"]), len(row_range), f"{label} style row count")
    for i, row in enumerate(row_range):
        require_equal(len(page["rows"][i]), len(column_range), f"{label} row width")
        require_equal(len(page["styleIds"][i]), len(column_range), f"{label} style row width")
        for j, column in enumerate(column_range):
            address, value, style = cells[row, column]
            location = f"{name}!{address}"
            require_equal(page["rows"][i][j], value, location)
            style_id = page["styleIds"][i][j]
            actual = page["styles"].get(str(style_id), {})
            for field, expected in style.items():
                value = actual.get(field)
                if field == "borders":
                    value = {side: (BORDER_NAMES[edge["style"]], edge.get("color"))
                             for side, edge in (value or {}).items()}
                elif field == "bold":
                    value = bool(value)
                elif field == "fontColor" and expected is None:
                    # Desktop workbook readers map theme/default text to black.
                    if value in ("#000000", None):
                        value = None
                require_equal(value, expected, f"{location} {field}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--runner", type=Path, help="path to a prebuilt mai-runner binary")
    parser.add_argument("--save", type=Path, help="copy the verified XLSX to this path")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="mai-workbook-") as directory:
        work = Path(directory)
        runner = args.runner.resolve() if args.runner else work / "mai-runner"
        if not args.runner:
            if os.name == "nt":
                runner = runner.with_suffix(".exe")
            subprocess.run([os.environ.get("MAI_GO", "go"), "build", "-mod=readonly",
                            "-o", str(runner), "./cmd/mai-runner"], cwd=REPO, check=True)

        for input_file in ("bom_basic.xlsx", "prio_basic.xlsx"):
            shutil.copyfile(REPO / "testdata" / input_file, work / input_file)
        result = request(runner, work, "run", yaml=(REPO / "workflows/bom.yaml").read_text(encoding="utf-8"),
                         inputs={"bom": "bom_basic.xlsx", "priority": "prio_basic.xlsx"})
        artifact = work / result["path"]
        require_equal(hashlib.sha256(artifact.read_bytes()).hexdigest(), result["sha256"], "saved file SHA-256")
        sheets = read_reference(artifact)
        require_equal(list(sheets), ["BOM", "debug"], "saved sheets")
        bom = sheets["BOM"][1]
        for coordinates, value, fill in [
            ((6, 10), "YAGEO CORPORATION", "#FF0000"),
            ((6, 11), "RC0402FR-0710KL_REV_B2", "#FF0000"),
            ((3, 12), "", "#000000"),
            ((5, 15), "MAIN", "#0070C0"),
        ]:
            address, text, style = bom[coordinates]
            require_equal((text, style["fillColor"]), (value, fill), f"saved BOM!{address}")

        for sheet in sheets:
            compare_page(runner, work, result["path"], sheets, sheet, 0, 7, 0, 27)
        compare_page(runner, work, result["path"], sheets, "BOM", 2, 4, 9, 6)
        compare_page(runner, work, result["path"], sheets, "debug", 1, 3, 19, 8)
        if args.save:
            shutil.copyfile(artifact, args.save)
        print(f"Workbook smoke passed for BOM and debug ({result['sha256']}).")


if __name__ == "__main__":
    main()
