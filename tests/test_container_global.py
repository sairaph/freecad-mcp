"""A shape in a moved container is reported where the container puts it (get_object's Shape and
the compact list's Origin members).

The real tessellation and serialize code runs; FreeCAD's shapes and placements are small
fakes: a placement is a shift along x, y and z, a shape is a box.
"""

import importlib
import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"

if str(ADDON) not in sys.path:
    sys.path.insert(0, str(ADDON))
tessellation = importlib.import_module("rpc_server.tessellation")


@pytest.fixture
def serialize(monkeypatch: pytest.MonkeyPatch):
    """serialize.py loaded as a private copy against a bare FreeCAD stub, so the module other tests import is untouched."""
    monkeypatch.setitem(sys.modules, "FreeCAD", types.ModuleType("FreeCAD"))
    spec = importlib.util.spec_from_file_location("_serialize_global_test", ADDON / "rpc_server" / "serialize.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module
tessellation = importlib.import_module("rpc_server.tessellation")


class Box:
    def __init__(self, x0, y0, z0, x1, y1, z1):
        self.XMin, self.YMin, self.ZMin, self.XMax, self.YMax, self.ZMax = x0, y0, z0, x1, y1, z1
        self.XLength, self.YLength, self.ZLength = x1 - x0, y1 - y0, z1 - z0

    def isValid(self):
        return True


class Shift:
    def __init__(self, dx=0.0, dy=0.0, dz=0.0):
        self.d = (dx, dy, dz)

    def isIdentity(self):
        return self.d == (0.0, 0.0, 0.0)

    def toMatrix(self):
        return self.d


class Solid:
    ShapeType = "Solid"
    Volume = 120.0
    Area = 100.0
    Vertexes = Edges = Faces = ()
    Solids = (object(),)

    def __init__(self, box):
        self.box = box

    def isNull(self):
        return False

    def copy(self):
        return Solid(self.box)

    def transformShape(self, d):
        b = self.box
        self.box = (b[0] + d[0], b[1] + d[1], b[2] + d[2], b[3] + d[0], b[4] + d[1], b[5] + d[2])

    def optimalBoundingBox(self, *_args):
        return Box(*self.box)

    @property
    def BoundBox(self):
        return Box(*self.box)

    @property
    def CenterOfMass(self):
        b = self.box
        return types.SimpleNamespace(x=(b[0] + b[3]) / 2, y=(b[1] + b[4]) / 2, z=(b[2] + b[5]) / 2)


class Group:
    def __init__(self, name, shift=None, parent=None):
        self.Name = name
        self.Placement = shift or Shift()
        self.parent = parent

    def getParentGeoFeatureGroup(self):
        return self.parent

    def getGlobalPlacement(self):
        up = self.parent.getGlobalPlacement().d if self.parent else (0.0, 0.0, 0.0)
        return Shift(*(a + b for a, b in zip(up, self.Placement.d)))


class Feature:
    def __init__(self, name, box, parent=None):
        self.Name = name
        self.Shape = Solid(box)
        self.parent = parent

    def getParentGeoFeatureGroup(self):
        return self.parent


def test_an_object_in_a_moved_part_is_global_and_says_which_container_moves_it(serialize) -> None:
    part = Group("PartA", Shift(20, 20, 5))
    leaf = Feature("Leaf", (0, 0, 0, 40, 30, 3), part)
    shape = serialize._object_shape(leaf)
    assert shape["BoundBox"] == [20.0, 20.0, 5.0, 60.0, 50.0, 8.0]
    assert shape["CenterOfMass"] == {"x": 40.0, "y": 35.0, "z": 6.5}
    assert shape["LocalBoundBox"] == [0.0, 0.0, 0.0, 40.0, 30.0, 3.0]
    assert "inside PartA, which moves it" in shape["BoundBoxNote"]
    # The document's own shape is not moved.
    assert leaf.Shape.box == (0, 0, 0, 40, 30, 3)


def test_the_container_named_is_the_one_with_a_placement(serialize) -> None:
    part = Group("PartA", Shift(0, 0, 5))
    body = Group("Body", Shift(), part)
    pad = Feature("Pad", (0, 0, 0, 10, 10, 4), body)
    assert tessellation.moving_container(pad) == "PartA"
    assert serialize._object_shape(pad)["BoundBox"] == [0.0, 0.0, 5.0, 10.0, 10.0, 9.0]


def test_an_unmoved_container_and_no_container_leave_the_shape_alone(serialize) -> None:
    still = Feature("InStill", (1, 2, 3, 4, 5, 6), Group("PartB"))
    alone = Feature("Alone", (1, 2, 3, 4, 5, 6))
    for obj in (still, alone):
        shape = serialize._object_shape(obj)
        assert shape["BoundBox"] == [1.0, 2.0, 3.0, 4.0, 5.0, 6.0]
        assert "LocalBoundBox" not in shape and "BoundBoxNote" not in shape
        assert tessellation.global_shape(obj) is obj.Shape


def test_an_object_without_a_shape_or_a_container_api_still_reads() -> None:
    assert tessellation.global_shape(types.SimpleNamespace()) is None
    odd = types.SimpleNamespace(Shape=Solid((0, 0, 0, 1, 1, 1)))
    assert tessellation.global_shape(odd) is odd.Shape
    assert tessellation.moving_container(odd) is None


def test_the_origin_members_of_every_origin_are_named(serialize) -> None:
    axis, plane, point = (types.SimpleNamespace(Name=n) for n in ("X_Axis", "XY_Plane", "Origin001"))
    origin = types.SimpleNamespace(Name="Origin", TypeId="App::Origin", OriginFeatures=[axis, plane, point])
    broken = types.SimpleNamespace(Name="Origin002", TypeId="App::Origin")
    box = types.SimpleNamespace(Name="Box", TypeId="Part::Box")
    doc = types.SimpleNamespace(Objects=[origin, box, broken, axis, plane, point])
    assert serialize._origin_member_names(doc) == {"X_Axis", "XY_Plane", "Origin001"}
