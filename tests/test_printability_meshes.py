"""check_printability places meshes by their global bounding box and checks their overlap by box.

The real printability, tessellation, plate and lookup code runs; only what FreeCAD
provides (documents, shapes, meshes) is replaced by small objects that behave as
FreeCAD 1.1.3 does: a mesh's BoundBox includes its own Placement (checked live), a
copy with a Placement set is moved, a solid's intersection is its shared volume.
"""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class Box:
    def __init__(self, x0, y0, z0, x1, y1, z1):
        self.XMin, self.YMin, self.ZMin, self.XMax, self.YMax, self.ZMax = x0, y0, z0, x1, y1, z1

    def isValid(self) -> bool:
        return True


class Placement:
    def __init__(self, dx: float = 0.0):
        self.dx = dx

    def isIdentity(self) -> bool:
        return self.dx == 0


class Mesh:
    """A mesh whose points are local; its Placement moves them along x."""

    CountFacets = 12
    copies = 0

    def __init__(self, local, dx: float = 0.0):
        self.local, self.dx = local, dx

    @property
    def BoundBox(self) -> Box:
        x0, y0, z0, x1, y1, z1 = self.local
        return Box(x0 + self.dx, y0, z0, x1 + self.dx, y1, z1)

    def copy(self) -> "Mesh":
        Mesh.copies += 1
        return Mesh(self.local, self.dx)

    @property
    def Placement(self) -> Placement:
        return Placement(self.dx)

    @Placement.setter
    def Placement(self, value: Placement) -> None:
        self.dx = value.dx


class MeshObject:
    """A Mesh::Feature: its own Placement is part of its Mesh, a container adds its own."""

    def __init__(self, name: str, local, own_dx: float = 0.0, container: "Container | None" = None):
        self.Name, self.Label = name, name
        self.Mesh = Mesh(local, own_dx)
        self.own_dx = own_dx
        self.container = container
        if container is not None:
            container.Group.append(self)

    def isDerivedFrom(self, type_id: str) -> bool:
        return type_id == "Mesh::Feature"

    def getParentGeoFeatureGroup(self):
        return self.container

    def getGlobalPlacement(self) -> Placement:
        return Placement(self.own_dx + (self.container.dx if self.container else 0.0))


class Container:
    """An App::Part: no shape of its own here, a global placement and members."""

    def __init__(self, name: str, dx: float):
        self.Name, self.Label, self.dx = name, name, dx
        self.Group: list = []

    def isDerivedFrom(self, _type_id: str) -> bool:
        return False

    def getParentGeoFeatureGroup(self):
        return None

    def getGlobalPlacement(self) -> Placement:
        return Placement(self.dx)


class Shape:
    def __init__(self, box, volume_with=None):
        self.box = box
        self.Solids = [object()]
        self.volume_with = volume_with or {}

    def isNull(self) -> bool:
        return False

    def optimalBoundingBox(self, *_args) -> Box:
        return Box(*self.box)

    @property
    def BoundBox(self) -> Box:
        return Box(*self.box)

    def copy(self) -> "Shape":
        return self

    def common(self, other: "Shape"):
        return types.SimpleNamespace(Volume=self.volume_with.get(id(other), 0.0))


class Solid:
    def __init__(self, name: str, box, volume_with=None):
        self.Name, self.Label = name, name
        self.Shape = Shape(box, volume_with)
        self.Group: list = []

    def isDerivedFrom(self, _type_id: str) -> bool:
        return False

    def getParentGeoFeatureGroup(self):
        return None


class Empty:
    Name = Label = "Empty"
    Group: list = []

    def isDerivedFrom(self, _type_id: str) -> bool:
        return False

    def getParentGeoFeatureGroup(self):
        return None


class Doc:
    Name = "Doc"

    def __init__(self, *objects):
        self.Objects = list(objects)

    def getObject(self, name: str):
        return next((o for o in self.Objects if o.Name == name), None)


@pytest.fixture
def printability(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.syspath_prepend(str(ADDON))
    monkeypatch.setitem(sys.modules, "FreeCAD", sys.modules.get("FreeCAD") or types.ModuleType("FreeCAD"))
    part = types.ModuleType("Part")

    def get_shape(obj):
        shape = getattr(obj, "Shape", None)
        if shape is None:
            raise RuntimeError("no shape")
        return shape

    part.getShape = get_shape
    monkeypatch.setitem(sys.modules, "Part", part)
    gui_task = types.ModuleType("rpc_server.gui_task")
    gui_task.resolve_timeout = lambda value, default: float(default)
    gui_task.run_on_gui = lambda task, *_args, **_kwargs: task()
    monkeypatch.setitem(sys.modules, "rpc_server.gui_task", gui_task)
    spec = importlib.util.spec_from_file_location("_printability_test", ADDON / "rpc_server" / "printability.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def check(printability, monkeypatch: pytest.MonkeyPatch, doc: Doc, names=None, plate=(100.0, 100.0)):
    from rpc_server import lookup

    monkeypatch.setattr(lookup.FreeCAD, "listDocuments", lambda: {"Doc": doc}, raising=False)
    return printability._check_printability_gui("Doc", names, list(plate), [0.0, 0.0], 60.0)


def by_name(reply: dict) -> dict:
    return {o["name"]: o for o in reply["objects"]}


def test_a_mesh_fits_by_its_global_bounds_with_its_placement_counted_once(printability, monkeypatch) -> None:
    # Local points 0..20 in x, placed 30 along x: the mesh reports 30..50 already.
    mesh = MeshObject("Ball", (0, 0, 0, 20, 10, 5), own_dx=30)
    reply = check(printability, monkeypatch, Doc(mesh), ["Ball"])
    part = by_name(reply)["Ball"]
    assert part["size"] == [20.0, 10.0, 5.0]
    assert part["margin_mm"]["x_low"] == 30.0 and part["margin_mm"]["x_high"] == 50.0
    assert part["inside"] and part["issues"] == []
    assert part["overlap_checked_by"] == "bounding box"
    assert reply["printable"] is True


def test_a_mesh_past_the_plate_is_reported_like_a_solid(printability, monkeypatch) -> None:
    mesh = MeshObject("Ball", (0, 0, 0, 20, 10, 5), own_dx=90)
    reply = check(printability, monkeypatch, Doc(mesh), ["Ball"])
    assert by_name(reply)["Ball"]["issues"] == ["extends past x = 100 (the plate edge) by 10 mm"]
    assert reply["printable"] is False


def test_a_mesh_in_a_moved_container_counts_the_container_once(printability, monkeypatch) -> None:
    part = Container("Part", dx=50)
    mesh = MeshObject("Ball", (0, 0, 0, 20, 10, 5), own_dx=10, container=part)
    reply = check(printability, monkeypatch, Doc(part, mesh), ["Part"])
    # Own 10 and the container's 50, each once.
    assert by_name(reply)["Part"]["bound_box"][0] == 60.0
    assert by_name(reply)["Part"]["bound_box"][3] == 80.0


def test_two_solids_use_the_exact_intersection_a_mesh_uses_the_box(printability, monkeypatch) -> None:
    # A nut in a cavity: the boxes overlap, the solids share no volume.
    nut = Solid("Nut", (10, 10, 0, 20, 20, 5))
    housing = Solid("Housing", (0, 0, 0, 40, 40, 20), volume_with={id(nut.Shape): 0.0})
    reply = check(printability, monkeypatch, Doc(housing, nut), ["Housing", "Nut"])
    assert reply["overlaps"] == [] and reply["printable"] is True

    ball = MeshObject("Ball", (10, 10, 0, 20, 20, 5))
    reply = check(printability, monkeypatch, Doc(housing, ball), ["Housing", "Ball"])
    assert reply["overlaps"] == [{"a": "Housing", "b": "Ball", "volume_mm3": 500.0, "by": "bounding box"}]
    assert reply["printable"] is False
    assert "bounding boxes share 500.00 mm^3" in by_name(reply)["Ball"]["issues"][0]
    assert "bounding box" in by_name(reply)["Housing"]["issues"][0]


def test_meshes_whose_boxes_only_touch_do_not_overlap(printability, monkeypatch) -> None:
    left = MeshObject("Left", (0, 0, 0, 20, 20, 5))
    right = MeshObject("Right", (20, 0, 0, 40, 20, 5))
    reply = check(printability, monkeypatch, Doc(left, right), ["Left", "Right"])
    assert reply["overlaps"] == [] and reply["printable"] is True


def test_without_names_the_visible_meshes_and_solids_are_checked_and_empty_objects_are_not(
    printability, monkeypatch
) -> None:
    doc = Doc(MeshObject("Ball", (0, 0, 0, 10, 10, 10)), Solid("Block", (50, 0, 0, 60, 10, 10)), Empty())
    reply = check(printability, monkeypatch, doc)
    assert sorted(by_name(reply)) == ["Ball", "Block"]


def test_choosing_the_default_parts_copies_no_mesh_and_measuring_copies_each_once(printability, monkeypatch) -> None:
    part = Container("Part", dx=50)
    MeshObject("Ball", (0, 0, 0, 20, 10, 5), container=part)
    Mesh.copies = 0
    reply = check(printability, monkeypatch, Doc(part, *part.Group))
    assert sorted(by_name(reply)) == ["Part"]
    assert Mesh.copies == 1


def test_an_empty_mesh_is_not_a_default_part(printability, monkeypatch) -> None:
    empty = MeshObject("Empty", (0, 0, 0, 1, 1, 1))
    empty.Mesh.CountFacets = 0
    assert check(printability, monkeypatch, Doc(empty))["objects"] == []


def test_an_object_with_neither_a_solid_nor_a_mesh_says_so(printability, monkeypatch) -> None:
    reply = check(printability, monkeypatch, Doc(Empty()), ["Empty"])
    assert by_name(reply)["Empty"]["issues"] == ["has no solid or mesh shape to place on the plate"]
    assert reply["printable"] is False


def test_a_solid_above_the_plate_floats_as_a_warning_and_the_layout_stays_printable(printability, monkeypatch) -> None:
    up = Solid("Up", (0, 0, 5, 10, 10, 8))
    down = Solid("Down", (20, 0, 0, 30, 10, 3))
    reply = check(printability, monkeypatch, Doc(up, down), ["Up", "Down"])
    rows = by_name(reply)
    assert rows["Up"]["issues"] == [] and rows["Up"]["warnings"] == ["floats 5 mm above the plate: it needs slicer supports, or move it down so its lowest point is at z 0"]
    assert rows["Up"]["bound_box"][2] == 5.0 and rows["Up"]["inside"]
    assert rows["Down"]["warnings"] == []
    assert rows["Down"]["issues"] == [] and rows["Down"]["bound_box"][2] == 0.0
    assert reply["printable"] is True


def test_a_part_inside_a_moved_container_floats_by_its_global_height(printability, monkeypatch) -> None:
    # The container is the part: its own shape is global already, as Part.getShape gives it.
    holder = Solid("PartA", (20, 20, 5, 60, 50, 8))
    reply = check(printability, monkeypatch, Doc(holder), ["PartA"])
    assert by_name(reply)["PartA"]["warnings"][0].startswith("floats 5 mm above the plate")
