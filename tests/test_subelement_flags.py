"""list_subelements marks faces and edges on the bottom and edges with no length."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class Box:
    def __init__(self, zmax: float):
        self.ZMax = zmax
        self.isValid = lambda: True


class Part:
    """An edge or face with the tight box ``serialize.tight_bound_box`` reads."""

    def __init__(self, zmax: float, length: float = 5.0):
        self.Length = length
        self.Area = 4.0
        self.Surface = None
        self.Curve = None
        self.CenterOfMass = types.SimpleNamespace(x=0, y=0, z=0)
        self.FirstParameter = self.LastParameter = 0.0
        self._box = Box(zmax)

    def optimalBoundingBox(self, _triangulation, _tolerance):
        return self._box

    def valueAt(self, _parameter):
        return types.SimpleNamespace(x=0.0, y=0.0, z=0.0)


@pytest.fixture
def subelements(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.syspath_prepend(str(ADDON))
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintError=lambda _m: None)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    for name in ("rpc_server.gui_task", "rpc_server.lookup"):
        module = types.ModuleType(name)
        module.run_on_gui = module.require_document = module.require_object = lambda *a, **k: None
        monkeypatch.setitem(sys.modules, name, module)
    spec = importlib.util.spec_from_file_location("_subelements_test", ADDON / "rpc_server" / "subelements.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


PART = types.SimpleNamespace(Line=type("Line", (), {}), LineSegment=type("LineSegment", (), {}), Circle=type("Circle", (), {}),
                             Plane=type("Plane", (), {}))


def test_an_edge_or_face_at_the_lowest_z_is_on_the_bottom(subelements) -> None:
    assert subelements._edge_row("Edge1", Part(0.00005), PART, 0.0)["on_bottom"] is True
    assert subelements._face_row("Face1", Part(0.0), PART, 0.0)["on_bottom"] is True
    assert "on_bottom" not in subelements._edge_row("Edge2", Part(10.0), PART, 0.0)
    assert "on_bottom" not in subelements._face_row("Face2", Part(0.01), PART, 0.0)
    assert "on_bottom" not in subelements._edge_row("Edge3", Part(0.0), PART, None)


def test_an_edge_with_no_length_is_degenerate_and_never_on_the_bottom(subelements) -> None:
    row = subelements._edge_row("Edge4", Part(0.0, length=0.0), PART, 0.0)
    assert row["degenerate"] is True and "on_bottom" not in row
    assert "degenerate" not in subelements._edge_row("Edge5", Part(0.0, length=0.5), PART, 0.0)
