"""A signed quantity written to a sheet cell stays a quantity, not text."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"
sys.path.insert(0, str(ADDON))


@pytest.fixture
def spreadsheet(monkeypatch):
    # gui_task, lookup, serialize and transactions need FreeCAD itself; the cell helper uses none of them.
    for name, attrs in {
        "rpc_server.gui_task": {"run_on_gui": None},
        "rpc_server.lookup": {"require_document": None},
        "rpc_server.serialize": {"serialize_value": None},
        "rpc_server.transactions": {"active_document": None, "transaction": None},
    }.items():
        module = types.ModuleType(name)
        for attr, value in attrs.items():
            setattr(module, attr, value)
        monkeypatch.setitem(sys.modules, name, module)
    spec = importlib.util.spec_from_file_location("_spreadsheet_under_test", ADDON / "rpc_server" / "spreadsheet.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def store(spreadsheet, sheet, address, content):
    """What update_spreadsheet_cells does for one cell."""
    converted = {address: content} if spreadsheet._set_cell_content(sheet, address, content) else {}
    spreadsheet._settle_signed_cells(sheet, converted)


class Sheet:
    """FreeCAD's cell parser as measured: a leading sign makes a number with a
    unit text, an expression with an unknown unit is stored as text again, and
    a cell has no value before the sheet is recomputed."""

    def __init__(self, units=("mm", "deg")):
        self.cells: dict[str, str] = {}
        self.units = units
        self.computed = False

    def set(self, address: str, content: str) -> None:
        if content.startswith("'"):
            self.cells[address] = content
        elif content.startswith("="):
            unit = content.split()[-1].lstrip("=0123456789.+-")
            self.cells[address] = content if unit in self.units else "'" + content
        elif content[:1] in "+-" and not content[1:].replace(".", "").isdigit():
            self.cells[address] = "'" + content
        else:
            self.cells[address] = content

    def recompute(self) -> None:
        self.computed = True

    def getContents(self, address: str) -> str:
        return self.cells.get(address, "")

    def get(self, address: str) -> str:
        if not self.computed:
            raise ValueError("Invalid cell address or property: " + address)
        return self.cells[address]


@pytest.mark.parametrize("content", ["-20 deg", "-20 mm", "- 5 mm", "+5 mm", "-1.5e2 mm", "-.5 mm"])
def test_a_signed_quantity_is_stored_as_an_expression(spreadsheet, content: str) -> None:
    sheet = Sheet()
    store(spreadsheet, sheet, "A1", content)
    assert sheet.cells["A1"] == "=" + content


@pytest.mark.parametrize("content", ["-20", "20 deg", "text", "=-20 mm"])
def test_content_FreeCAD_does_not_store_as_text_is_left_as_given(spreadsheet, content: str) -> None:
    sheet = Sheet()
    store(spreadsheet, sheet, "A1", content)
    assert sheet.cells["A1"] == content


def test_a_signed_number_with_an_unknown_unit_stays_the_text_FreeCAD_made(spreadsheet) -> None:
    sheet = Sheet()
    store(spreadsheet, sheet, "A1", "-20 foo")
    assert sheet.cells["A1"] == "'-20 foo"


def test_explicit_text_is_never_turned_into_an_expression(spreadsheet) -> None:
    sheet = Sheet()
    store(spreadsheet, sheet, "A1", "'-20 mm")
    assert sheet.cells["A1"] == "'-20 mm"


def test_an_expression_that_parses_but_cannot_be_evaluated_stays_the_text_as_entered(spreadsheet) -> None:
    class Failing(Sheet):
        def get(self, address: str) -> str:
            return "ERR: cannot evaluate" if self.cells[address].startswith("=") else super().get(address)

    sheet = Failing()
    store(spreadsheet, sheet, "A1", "-20 mm")
    assert sheet.cells["A1"] == "'-20 mm"


def test_a_batch_of_signed_cells_recomputes_the_sheet_once(spreadsheet) -> None:
    class Counting(Sheet):
        recomputes = 0

        def recompute(self) -> None:
            Counting.recomputes += 1
            super().recompute()

    sheet = Counting()
    converted = {}
    for address, content in {"A1": "-20 deg", "A2": "-20 mm", "A3": "-20 foo", "A4": "text"}.items():
        if spreadsheet._set_cell_content(sheet, address, content):
            converted[address] = content
    spreadsheet._settle_signed_cells(sheet, converted)
    assert Counting.recomputes == 1
    assert [sheet.cells[a] for a in ("A1", "A2", "A3", "A4")] == ["=-20 deg", "=-20 mm", "'-20 foo", "text"]
    spreadsheet._settle_signed_cells(sheet, {})
    assert Counting.recomputes == 1


def test_a_failure_while_storing_the_expression_restores_the_entered_content(spreadsheet) -> None:
    class Refusing(Sheet):
        def set(self, address: str, content: str) -> None:
            if content.startswith("="):
                raise RuntimeError("refused")
            super().set(address, content)

    sheet = Refusing()
    assert spreadsheet._set_cell_content(sheet, "A1", "-20 mm") is False
    assert sheet.cells["A1"] == "'-20 mm"


def test_a_sheet_that_cannot_recompute_gives_every_converted_cell_back_as_entered(spreadsheet) -> None:
    class Broken(Sheet):
        def recompute(self) -> None:
            raise RuntimeError("no")

    sheet = Broken()
    converted = {}
    for address in ("A1", "A2"):
        converted[address] = "-20 mm"
        spreadsheet._set_cell_content(sheet, address, "-20 mm")
    spreadsheet._settle_signed_cells(sheet, converted)
    assert [sheet.cells[a] for a in ("A1", "A2")] == ["'-20 mm", "'-20 mm"]
