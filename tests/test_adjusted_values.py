"""A number FreeCAD stored differently (clamped or replaced) is reported with what it stored."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class Quantity:
    def __init__(self, value, unit):
        self.Value, self.Unit = value, types.SimpleNamespace(Type=unit)


@pytest.fixture
def adjusted_values(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.syspath_prepend(str(ADDON))
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintError=lambda _message: None)
    freecad.Vector = type("Vector", (), {})
    freecad.Units = types.SimpleNamespace(Quantity=Quantity)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    spec = importlib.util.spec_from_file_location("_property_mapper_adjusted_test", ADDON / "rpc_server" / "property_mapper.py")
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)
    spec.loader.exec_module(module)
    return module.adjusted_values


def circle(**values) -> types.SimpleNamespace:
    return types.SimpleNamespace(PropertiesList=list(values), **values)


def test_a_clamped_angle_is_reported(adjusted_values) -> None:
    obj = circle(Angle1=Quantity(0, "Angle"), Radius=Quantity(5, "Length"))
    assert adjusted_values(obj, {"Angle1": -90, "Radius": 5}) == [{"name": "Angle1", "given": -90, "stored": 0.0}]


def test_a_value_stored_as_given_is_not_reported(adjusted_values) -> None:
    obj = circle(Radius=Quantity(5.0000000001, "Length"))
    assert adjusted_values(obj, {"Radius": 5}) == []


def test_expressions_strings_bools_and_other_units_are_not_compared(adjusted_values) -> None:
    obj = circle(Radius=Quantity(1, "Length"), Force=Quantity(0.1, "Force"), Flag=Quantity(1, "Length"))
    assert adjusted_values(obj, {"Radius": "=Params.r", "Force": 100, "Flag": True, "Missing": 3}) == []
