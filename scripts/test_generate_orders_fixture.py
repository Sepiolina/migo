import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location(
    "orders_fixture", Path(__file__).with_name("generate-orders-fixture.py")
)
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


class OrdersFixtureTests(unittest.TestCase):
    def test_bundled_fixture_matches_reproducible_generator(self):
        with tempfile.TemporaryDirectory() as directory:
            first = Path(directory) / "first.xlsx"
            second = Path(directory) / "second.xlsx"
            fixture.generate(first)
            fixture.generate(second)
            self.assertEqual(first.read_bytes(), second.read_bytes())
            bundled = Path(__file__).resolve().parents[1] / "examples/orders.xlsx"
            self.assertEqual(first.read_bytes(), bundled.read_bytes())


if __name__ == "__main__":
    unittest.main()
