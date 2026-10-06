"""Part::Fillet and Part::Chamfer edges from obj_properties: names, entries, defaults and checks."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


@pytest.fixture
def mapper(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.syspath_prepend(str(ADDON))
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintError=lambda _message: None)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    spec = importlib.util.spec_from_file_location("_property_mapper_test", ADDON / "rpc_server" / "property_mapper.py")
    module = importlib.util.module_from_spec(spec)
    monkeypatch.setitem(sys.modules, spec.name, module)  # the dataclass in it looks its module up
    spec.loader.exec_module(module)
    return module


class Fillet:
    """A Part::Fillet over a base with twelve edges; Edges is what FreeCAD stores."""

    def __init__(self, type_id: str = "Part::Fillet", edges: int = 12):
        self.TypeId = type_id
        self.Base = types.SimpleNamespace(Name="Box", Shape=types.SimpleNamespace(Edges=[object()] * edges))
        self.Document = types.SimpleNamespace(Name="Doc")
        self.Edges: list = []
        self.PropertiesList = ["Base", "Edges"]

    def getTypeIdOfProperty(self, name: str) -> str:
        return "Part::PropertyFilletEdges" if name == "Edges" else "App::PropertyLink"


def test_edge_names_take_the_radius_given_beside_them(mapper) -> None:
    assert mapper.fillet_edge_entries(Fillet(), ["Edge1", "Edge12"], 2) == [(1, 2.0, 2.0), (12, 2.0, 2.0)]


def test_entries_can_differ_and_a_chamfer_takes_size_and_a_second_size(mapper) -> None:
    entries = [{"edge": "Edge3", "radius": 1.5}, {"edge": "Edge4", "size": 2, "size2": 3}, [5, 1, 1]]
    assert mapper.fillet_edge_entries(Fillet("Part::Chamfer"), entries, None) == [(3, 1.5, 1.5), (4, 2.0, 3.0), (5, 1.0, 1.0)]


@pytest.mark.parametrize(
    "value, default, message",
    [
        (["Edge13"], 1, "Edge 'Edge13' does not exist on 'Box', which has Edge1 to Edge12. Call list_subelements"),
        (["Face1"], 1, "'Face1' is not an edge name"),
        (["Edge1"], None, "Give the Radius for Edge1"),
        (["Edge1"], "=Params.r", "cannot be an expression"),
        (["Edge1"], 0, "must be a number above 0"),
        ([], 1, "Edges must be a list"),
        ("Edge1", 1, "Edges must be a list"),
    ],
)
def test_a_bad_edge_list_says_what_is_wrong(mapper, value, default, message: str) -> None:
    with pytest.raises(ValueError, match=message.replace("(", r"\(").replace(")", r"\)")):
        mapper.fillet_edge_entries(Fillet(), value, default)


def test_a_new_size_alone_resizes_the_listed_edges_and_edges_are_set_after_base(mapper) -> None:
    obj = Fillet()
    mapper.set_object_property(None, obj, {"Edges": ["Edge1", "Edge2"], "Radius": 2})
    assert obj.Edges == [(1, 2.0, 2.0), (2, 2.0, 2.0)]
    mapper.set_object_property(None, obj, {"Radius": 0.5})
    assert obj.Edges == [(1, 0.5, 0.5), (2, 0.5, 0.5)]


def test_a_wrong_edge_fails_the_call_without_touching_the_edges(mapper) -> None:
    obj = Fillet()
    obj.Edges = [(1, 1.0, 1.0)]
    with pytest.raises(ValueError, match="Edge 'Edge40' does not exist"):
        mapper.set_object_property(None, obj, {"Edges": ["Edge40"], "Radius": 1})
    assert obj.Edges == [(1, 1.0, 1.0)]


def test_the_edges_now_are_listed_with_their_sizes(mapper) -> None:
    obj = Fillet()
    obj.Edges = [(1, 4.0, 4.0), (3, 2.0, 4.0), (5, 0.25, 0.25)]
    assert mapper.fillet_edges_text(obj, ["Radius"]) == ["Edge1 r4", "Edge3 r2 to r4", "Edge5 r0.25"]
    chamfer = Fillet("Part::Chamfer")
    chamfer.Edges = [(2, 1.5, 1.5)]
    assert mapper.fillet_edges_text(chamfer, ["Edges", "Size"]) == ["Edge2 s1.5"]


def test_nothing_is_listed_when_the_call_did_not_touch_the_edges(mapper) -> None:
    obj = Fillet()
    obj.Edges = [(1, 4.0, 4.0)]
    assert mapper.fillet_edges_text(obj, ["Label"]) == []
    assert mapper.fillet_edges_text(types.SimpleNamespace(TypeId="Part::Box", getTypeIdOfProperty=lambda _n: ""), ["Radius"]) == []
