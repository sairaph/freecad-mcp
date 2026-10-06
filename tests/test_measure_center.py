"""measure's point "center": centres and axes of circles, spheres and cylinders, and the distances between them."""

import importlib.util
import math
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class V:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = float(x), float(y), float(z)

    def add(self, o): return V(self.x + o.x, self.y + o.y, self.z + o.z)
    def sub(self, o): return V(self.x - o.x, self.y - o.y, self.z - o.z)
    def multiply(self, k): return V(self.x * k, self.y * k, self.z * k)
    def dot(self, o): return self.x * o.x + self.y * o.y + self.z * o.z
    def cross(self, o): return V(self.y * o.z - self.z * o.y, self.z * o.x - self.x * o.z, self.x * o.y - self.y * o.x)

    @property
    def Length(self): return math.sqrt(self.dot(self))

    def normalize(self): return self.multiply(1 / self.Length)
    def distanceToPoint(self, o): return self.sub(o).Length


class Part:
    class Circle:
        def __init__(self, c, r): self.Center, self.Radius = c, r

    class Sphere:
        def __init__(self, c, r): self.Center, self.Radius = c, r

    class Cylinder:
        def __init__(self, c, axis, r): self.Center, self.Axis, self.Radius = c, axis, r

    class Plane:
        pass


class Elem:
    def __init__(self, shape_type, geometry):
        self.ShapeType = shape_type
        self.Curve = self.Surface = geometry


@pytest.fixture
def measure(monkeypatch):
    monkeypatch.syspath_prepend(str(ADDON))
    for name, attrs in {
        "rpc_server.gui_task": {"run_on_gui": lambda *a, **k: None},
        "rpc_server.lookup": {"require_document": None, "require_object": None},
        "rpc_server.serialize": {"finite_or_none": lambda v: v},
    }.items():
        module = types.ModuleType(name)
        module.__dict__.update(attrs)
        monkeypatch.setitem(sys.modules, name, module)
    spec = importlib.util.spec_from_file_location("_measure_center_test", ADDON / "rpc_server" / "measure.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def hole(x, y, z=5.0, r=1.6):
    return Elem("Edge", Part.Circle(V(x, y, z), r))


def bore(x, y, r=1.6):
    return Elem("Face", Part.Cylinder(V(x, y, 7), V(0, 0, -1), r))


def test_what_has_a_centre(measure) -> None:
    kind, centre, direction, what = measure._center_of(hole(1, 2, 3), Part)
    assert (kind, centre.x, centre.y, centre.z, what) == ("point", 1, 2, 3, "circle of radius 1.6")
    kind, centre, direction, what = measure._center_of(Elem("Face", Part.Sphere(V(4, 5, 6), 3)), Part)
    assert (kind, centre.z, what) == ("point", 6, "sphere of radius 3")
    kind, centre, direction, what = measure._center_of(bore(10, 20), Part)
    assert kind == "axis" and direction.z == pytest.approx(-1) and what == "cylinder of radius 1.6"
    assert measure._center_of(Elem("Face", Part.Plane()), Part) is None
    assert measure._center_of(Elem("Vertex", None), Part) is None


def test_two_hole_edges_are_31_apart_from_centre_to_centre(measure) -> None:
    a, b = measure._center_of(hole(110, 70), Part), measure._center_of(hole(141, 70), Part)
    value, points = measure._center_distance(a, b)
    assert value == pytest.approx(31.0) and points == [[110, 70, 5], [141, 70, 5]]


def test_two_parallel_axes_are_the_centre_distance(measure) -> None:
    a, b = measure._center_of(bore(110, 70), Part), measure._center_of(bore(141, 70), Part)
    assert measure._center_distance(a, b)[0] == pytest.approx(31.0)
    # An axis pointing the other way is the same line.
    flipped = ("axis", V(141, 70, 7), V(0, 0, 1), "x")
    assert measure._center_distance(a, flipped)[0] == pytest.approx(31.0)


def test_a_point_to_an_axis_is_the_perpendicular_distance_either_way_round(measure) -> None:
    point = measure._center_of(hole(110, 70, 12), Part)
    axis = measure._center_of(bore(141, 70), Part)
    value, points = measure._center_distance(point, axis)
    assert value == pytest.approx(31.0) and points[0] == [110, 70, 12] and points[1] == pytest.approx([141, 70, 12])
    value, points = measure._center_distance(axis, point)
    assert value == pytest.approx(31.0) and points[1] == [110, 70, 12]


def test_skew_axes_give_the_distance_between_the_lines(measure) -> None:
    a = ("axis", V(10, 10, 0), V(0, 0, 1), "x")
    b = ("axis", V(0, 0, 5), V(0, 1, 0), "y")
    value, points = measure._center_distance(a, b)
    assert value == pytest.approx(10.0)
    assert points[0] == pytest.approx([10, 10, 5]) and points[1] == pytest.approx([0, 10, 5])


def test_measure_validates_the_point_argument(measure) -> None:
    refs = [{"object": "A", "sub": "Edge1", "point": "center"}, {"object": "A", "sub": "Edge2", "point": "center"}]
    assert "is for distance, not radius" in measure.measure("D", "radius", refs[:1])["error"]
    bad = measure.measure("D", "distance", [{**refs[0], "point": "middle"}, refs[1]])
    assert 'point must be "center"' in bad["error"]


def test_a_ref_with_no_centre_or_an_axis_against_a_plain_shape_is_refused(measure) -> None:
    doc = types.SimpleNamespace(Name="D")
    refs = [{"object": "A", "sub": "Face1", "point": "center"}, {"object": "A", "sub": "Face2", "point": "center"}]
    plane = Elem("Face", Part.Plane())
    reply = measure._measure_between_centres(doc, [plane, bore(1, 1)], refs, {}, Part)
    assert "A.Face1 has no centre" in reply["error"] and "list_subelements" in reply["hint"]
    refs = [{"object": "A", "sub": "Face1", "point": "center"}, {"object": "A"}]
    reply = measure._measure_between_centres(doc, [bore(1, 1), plane], refs, {}, Part)
    assert "cylinder's axis" in reply["error"]
    result = {}
    refs = [{"object": "A", "sub": "Edge1", "point": "center"}, {"object": "A", "sub": "Edge2", "point": "center"}]
    assert measure._measure_between_centres(doc, [hole(0, 0), hole(3, 4)], refs, result, Part) is None
    assert result["value"] == pytest.approx(5.0) and result["points_used"] == [
        "centre of A.Edge1 (circle of radius 1.6)", "centre of A.Edge2 (circle of radius 1.6)"]
