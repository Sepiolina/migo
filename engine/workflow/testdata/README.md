`parts.xls` is a synthetic BIFF8 workbook generated with Python xlwt 1.3.0.
It contains one sheet, `Parts`, with rows `ID, Quantity`, `001, 2`, and
`002, 3`. IDs are text cells; quantities are numeric cells. It contains no
user data, formulas, macros, or external links.

Reproduce with xlwt installed:

```python
import xlwt
book = xlwt.Workbook()
sheet = book.add_sheet("Parts")
for r, row in enumerate([["ID", "Quantity"], ["001", 2], ["002", 3]]):
    for c, value in enumerate(row):
        sheet.write(r, c, value)
book.save("parts.xls")
```
