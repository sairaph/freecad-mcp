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


class Normal:
    def __init__(self, x: float, y: float, z: float):
        self.v = (x, y, z)

    def getAngle(self, other: "Normal") -> float:
        import math

        dot = sum(a * b for a, b in zip(self.v, other.v))
        return math.acos(max(-1.0, min(1.0, dot)))


class Edge:
    FirstParameter, LastParameter = 0.0, 1.0

    def __init__(self, key: int, seam_of=None):
        self.key, self.seam_of = key, seam_of

    def hashCode(self) -> int:
        return self.key

    def isSame(self, other: "Edge") -> bool:
        return self.key == other.key

    def isSeam(self, face: "Face") -> bool:
        return self.seam_of is face

    def valueAt(self, _parameter: float):
        return types.SimpleNamespace(x=0.0, y=0.0, z=0.0)


class Face:
    def __init__(self, edges: list, normal: Normal):
        self.Edges, self._normal = edges, normal
        self.Surface = types.SimpleNamespace(parameter=lambda _point: (0.0, 0.0))

    def normalAt(self, _u: float, _v: float) -> Normal:
        return self._normal


def test_edges_where_two_faces_meet_tangentially_are_smooth_and_creases_are_not(subelements) -> None:
    crease, smooth, almost = Edge(1), Edge(2), Edge(3)
    side = Normal(1, 0, 0)
    faces = [
        Face([crease, smooth, almost], side),
        Face([crease], Normal(0, 1, 0)),
        Face([smooth], Normal(1, 0, 0)),
        Face([almost], Normal(0.99999, 0.0044721, 0)),  # about 4.5e-3 rad from the first
    ]
    held = subelements._edge_faces(faces)
    assert subelements._join_kind(faces, crease, held) is None
    assert subelements._join_kind(faces, smooth, held) == "smooth"
    assert subelements._join_kind(faces, almost, held) is None


def test_two_faces_folded_back_on_each_other_do_not_join_smoothly(subelements) -> None:
    fold = Edge(1)
    faces = [Face([fold], Normal(1, 0, 0)), Face([fold], Normal(-1, 0, 0))]
    assert subelements._join_kind(faces, fold, subelements._edge_faces(faces)) is None


def test_a_seam_is_where_one_face_meets_itself_and_a_free_boundary_is_neither(subelements) -> None:
    holder = Face([], Normal(1, 0, 0))
    seam, boundary = Edge(1), Edge(2)
    holder.Edges = [seam, boundary]
    seam.seam_of = holder
    faces = [holder]
    held = subelements._edge_faces(faces)
    assert subelements._join_kind(faces, seam, held) == "seam"
    assert subelements._join_kind(faces, boundary, held) is None
    assert subelements._join_kind(faces, Edge(99), held) is None


def test_the_faces_are_read_once_and_the_row_marks_smooth_and_seam(subelements) -> None:
    class Counting:
        reads = 0

    class Shape:
        @property
        def Faces(self):
            Counting.reads += 1
            return [Face([Edge(1)], Normal(1, 0, 0)), Face([Edge(1)], Normal(1, 0, 0))]

    shape = Shape()
    faces = shape.Faces
    subelements._edge_faces(faces)
    assert Counting.reads == 1
    assert subelements._edge_row("Edge1", Part(1.0), PART, None, "smooth")["smooth"] is True
    seam = subelements._edge_row("Edge2", Part(1.0), PART, None, "seam")
    assert seam["smooth"] is True and seam["seam"] is True
    assert "smooth" not in subelements._edge_row("Edge3", Part(1.0), PART, None)
    # A degenerate edge stays degenerate only.
    degenerate = subelements._edge_row("Edge4", Part(1.0, length=0.0), PART, None, "smooth")
    assert degenerate["degenerate"] is True and "smooth" not in degenerate
