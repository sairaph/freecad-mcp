"""list_subelements filters: validated, applied before the rows are built, totals kept."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class V:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = x, y, z

    def __sub__(self, o):
        return V(self.x - o.x, self.y - o.y, self.z - o.z)

    @property
    def Length(self):
        return (self.x**2 + self.y**2 + self.z**2) ** 0.5

    def normalize(self):
        n = self.Length
        self.x, self.y, self.z = self.x / n, self.y / n, self.z / n
        return self


class Part:
    class Line: ...
    class LineSegment: ...
    class Circle: ...
    class Plane: ...
    class Cylinder: ...


class Edge:
    def __init__(self, curve, a, b, zmax=5.0, radius=None):
        self.Curve = curve()
        if radius:
            self.Curve.Radius, self.Curve.Center = radius, V()
        self.a, self.b, self.zmax = V(*a), V(*b), zmax
        self.FirstParameter, self.LastParameter = 0, 1
        self.Length = (self.b - self.a).Length if curve is not Part.Circle else 6.28 * (radius or 1)
        self.ZMax = zmax

    def valueAt(self, t):
        return self.a if t == 0 else self.b


class Face:
    def __init__(self, surface, zmax=5.0):
        self.Surface, self.ZMax = surface(), zmax
        self.Area, self.CenterOfMass = 10.0, V()
        self.ParameterRange = (0, 1, 0, 1)

    def normalAt(self, u, v):
        return V(0, 0, 1)


@pytest.fixture
def sub(monkeypatch):
    monkeypatch.syspath_prepend(str(ADDON))
    for name, attrs in {
        "rpc_server.gui_task": {"run_on_gui": lambda task, *a, **k: task()},
        "rpc_server.lookup": {"require_document": None, "require_object": None},
        "rpc_server.serialize": {"finite_or_none": lambda v: v, "tight_bound_box": lambda part: types.SimpleNamespace(ZMax=part.ZMax, ZMin=0.0)},
    }.items():
        module = types.ModuleType(name)
        module.__dict__.update(attrs)
        monkeypatch.setitem(sys.modules, name, module)
    spec = importlib.util.spec_from_file_location("_subelements_filters_test", ADDON / "rpc_server" / "subelements.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_valid_filters_are_kept_and_none_values_dropped(sub) -> None:
    given, error = sub.check_filters("edges", {"curve": "line", "along": "z", "min_length": 2, "smooth": False, "on_bottom": None})
    assert error is None and given == {"curve": "line", "along": "z", "min_length": 2, "smooth": False}
    assert sub.check_filters("faces", None) == ({}, None)
    assert sub.check_filters("all", {"surface": "plane", "on_bottom": True})[1] is None


@pytest.mark.parametrize(
    "kind, filters, message",
    [
        ("faces", {"curve": "line"}, "curve filters edges"),
        ("faces", {"min_length": 1}, "min_length filters edges"),
        ("edges", {"surface": "plane"}, "surface filters faces"),
        ("edges", {"curve": "arc"}, "curve must be one of"),
        ("faces", {"surface": "blob"}, "surface must be one of"),
        ("edges", {"along": "w"}, "along must be x, y or z"),
        ("edges", {"on_bottom": "yes"}, "on_bottom must be true or false"),
        ("edges", {"smooth": 1.5}, "smooth must be true or false"),
        ("edges", {"min_length": -1}, "min_length must be a number"),
        ("edges", {"min_length": True}, "min_length must be a number"),
        ("edges", {"colour": "red"}, "unknown filter 'colour'"),
        ("edges", "all", "filters must be an object"),
    ],
)
def test_bad_filters_are_refused_with_a_short_message(sub, kind, filters, message) -> None:
    given, error = sub.check_filters(kind, filters)
    assert given == {} and message in error["error"]


def lines():
    return [
        Edge(Part.Line, (0, 0, 0), (0, 0, 10), zmax=10),    # along z, long
        Edge(Part.Line, (0, 0, 0), (0, 0, 0.5), zmax=0.5),  # along z, short
        Edge(Part.Line, (0, 0, 0), (7, 0, 0), zmax=0.0),    # along x, on the bottom
        Edge(Part.Circle, (0, 0, 0), (0, 0, 0), zmax=5, radius=2),
    ]


def test_edges_are_filtered_cheapest_first_by_length_curve_axis_and_bottom(sub) -> None:
    edges = lines()
    match = lambda f: [i for i, e in enumerate(edges) if sub._edge_matches(e, f, Part, 0.0)]
    assert match({"along": "z"}) == [0, 1]
    # An arc whose end points line up with an axis is not along it.
    arc = Edge(Part.Circle, (0, 0, 0), (0, 4, 0), radius=2)
    assert sub._along(arc) == "y"
    assert not sub._edge_matches(arc, {"along": "y"}, Part, 0.0)
    assert sub._edge_matches(Edge(Part.Line, (0, 0, 0), (0, 4, 0)), {"along": "y"}, Part, 0.0)
    assert match({"along": "z", "min_length": 1}) == [0]
    assert match({"curve": "circle"}) == [3]
    assert match({"curve": "line", "on_bottom": True}) == [2]
    assert match({"on_bottom": False}) == [0, 1, 3]
    assert match({}) == [0, 1, 2, 3]


def test_faces_are_filtered_by_surface_and_bottom(sub) -> None:
    faces = [Face(Part.Plane, zmax=0.0), Face(Part.Cylinder, zmax=5), Face(Part.Plane, zmax=5)]
    match = lambda f: [i for i, face in enumerate(faces) if sub._face_matches(face, f, Part, 0.0)]
    assert match({"surface": "plane"}) == [0, 2]
    assert match({"surface": "cylinder"}) == [1]
    assert match({"on_bottom": True}) == [0]
    assert match({"surface": "plane", "on_bottom": False}) == [2]


def test_the_join_is_only_looked_at_for_edges_that_passed_the_other_filters(sub, monkeypatch) -> None:
    edges = lines()
    shape = types.SimpleNamespace(Faces=[], Edges=edges)
    obj = types.SimpleNamespace()
    sub.require_document = lambda name: (types.SimpleNamespace(Name="D"), None)
    sub.require_object = lambda doc, name: (obj, None)
    tess = types.ModuleType("rpc_server.tessellation")
    tess.shape_of = lambda o: shape
    monkeypatch.setitem(sys.modules, "rpc_server.tessellation", tess)
    monkeypatch.setitem(sys.modules, "Part", Part)
    joined = []
    monkeypatch.setattr(sub, "_edge_faces", lambda faces: {})
    monkeypatch.setattr(sub, "_join_kind", lambda faces, edge, held: joined.append(edge) or ("smooth" if edge is edges[0] else None))
    result = sub._list_subelements_gui("D", "O", "edges", {"along": "z", "smooth": False})
    assert [r["name"] for r in result["edges"]] == ["Edge2"] and result["edge_count"] == 4
    assert joined == [edges[0], edges[1]]  # the circle and the x line never reached the join
    assert result["filters"] == {"along": "z", "smooth": False}
    unfiltered = sub._list_subelements_gui("D", "O", "edges", {})
    assert len(unfiltered["edges"]) == 4 and "filters" not in unfiltered
