#!/usr/bin/env python3
"""Create the synthetic tutorial workbook using Python's standard library only.

From the repository root: python3 scripts/generate-orders-fixture.py
The fixed ZIP metadata makes repeated generation byte-for-byte reproducible.
"""

import argparse
from pathlib import Path
from xml.sax.saxutils import escape
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


ROWS = (
    ("Customer", "Quantity", "Unit price"),
    ("Acme", 2, 10),
    ("Acme", 3, 10),
    ("Beta", 1, 5),
    ("Acme", 0, 999),
    ("Beta", -2, 100),
)


def generate(destination):
    rows = []
    for index, values in enumerate(ROWS, 1):
        cells = []
        for column, value in zip("ABC", values):
            address = f"{column}{index}"
            if isinstance(value, str):
                cells.append(f'<c r="{address}" t="inlineStr"><is><t>{escape(value)}</t></is></c>')
            else:
                cells.append(f'<c r="{address}"><v>{value}</v></c>')
        rows.append(f'<row r="{index}">{"".join(cells)}</row>')
    files = {
        "[Content_Types].xml": '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>',
        "_rels/.rels": '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>',
        "xl/workbook.xml": '<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Orders" sheetId="1" r:id="rId1"/></sheets></workbook>',
        "xl/_rels/workbook.xml.rels": '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>',
        "xl/worksheets/sheet1.xml": '<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><dimension ref="A1:C6"/><sheetData>' + "".join(rows) + '</sheetData></worksheet>',
    }
    with ZipFile(destination, "w") as archive:
        for name, value in files.items():
            entry = ZipInfo(name, date_time=(2020, 1, 1, 0, 0, 0))
            entry.compress_type = ZIP_DEFLATED
            entry.external_attr = 0o644 << 16
            archive.writestr(entry, '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>' + value)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", nargs="?", type=Path,
                        default=Path(__file__).resolve().parents[1] / "examples/orders.xlsx")
    generate(parser.parse_args().destination)
