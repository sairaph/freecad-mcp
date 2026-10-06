"""Regression test: one invalid shape must not break the whole document serialization.

See https://github.com/neka-nat/freecad-mcp/issues/109 (reported upstream, before the fork)
"""

import importlib
import sys
import types
from pathlib import Path

# --- Stub FreeCAD so serialize.py can be imported without a FreeCAD install ---
freecad_stub = types.ModuleType("FreeCAD")


class _StubVector:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = x, y, z


class _StubRotation:
    Axis = _StubVector(0, 0, 1)
    Angle = 0.0


class _StubPlacement:
    Base = _StubVector()
    Rotation = _StubRotation()


class _BrokenShape:
    """Shape whose compute failed: every attribute read raises RuntimeError."""

    def __getattr__(self, name):
        raise RuntimeError("shape is invalid")


class _StubBoundBox:
    XMin, YMin, ZMin = 0.0, 0.0, 0.0
    XMax, YMax, ZMax = 1.0, 2.0, 3.0


class _GoodShape:
    Volume = 42.0
    Area = 10.0
    Vertexes = [object() for _ in range(8)]
    Edges = [object() for _ in range(6)]
    Faces = [object() for _ in range(4)]
    Solids = [object()]
    BoundBox = _StubBoundBox()
    ShapeType = "Solid"
    CenterOfMass = _StubVector(0.5, 1.0, 1.5)


freecad_stub.Vector = _StubVector
freecad_stub.Rotation = _StubRotation
freecad_stub.Placement = _StubPlacement


class _StubDocument:
    Name = "Doc"
    Label = "Doc"
    FileName = ""
    Objects = []


freecad_stub.Document = _StubDocument
sys.modules["FreeCAD"] = freecad_stub

serialize_path = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"
sys.path.insert(0, str(serialize_path))
serialize = importlib.import_module("rpc_server.serialize")


def test_good_shape_serializes():
    result = serialize.serialize_shape(_GoodShape())
    assert result["Volume"] == 42.0
    assert result["VertexCount"] == 8
    assert result["SolidCount"] == 1


class _SweptShape(_GoodShape):
    """A swept thread: BoundBox is the loose box, optimalBoundingBox the exact one."""

    BoundBox = types.SimpleNamespace(XMin=-26.0, YMin=-26.0, ZMin=-0.8, XMax=26.0, YMax=26.0, ZMax=12.8)
    calls: list = []

    def optimalBoundingBox(self, use_triangulation, use_shape_tolerance):
        self.calls.append((use_triangulation, use_shape_tolerance))
        return types.SimpleNamespace(
            XMin=-20.0, YMin=-20.0, ZMin=-0.8, XMax=20.0, YMax=20.0, ZMax=12.8, isValid=lambda: True
        )


def test_the_bound_box_is_the_tight_one_without_triangulation():
    result = serialize.serialize_shape(_SweptShape())
    assert result["BoundBox"] == [-20.0, -20.0, -0.8, 20.0, 20.0, 12.8]
    assert _SweptShape.calls == [(False, False)]


def test_a_shape_the_tight_box_cannot_handle_keeps_its_plain_box():
    # _GoodShape has no optimalBoundingBox, as for a shape OpenCascade cannot measure.
    assert serialize.serialize_shape(_GoodShape())["BoundBox"] == [0.0, 0.0, 0.0, 1.0, 2.0, 3.0]


class _CountedSolid:
    """A solid whose Volume is an integral: the calls are counted."""

    def __init__(self, volume, x, faces=8):
        self._volume, self.CenterOfMass, self.Faces = volume, _StubVector(x, 0.0, 0.0), [object()] * faces
        self.volume_calls = 0

    @property
    def Volume(self):
        self.volume_calls += 1
        return self._volume


class _Compound:
    ShapeType = "Compound"
    Area = 10.0
    Vertexes, Edges = [object()] * 4, [object()] * 6
    BoundBox = _StubBoundBox()

    def __init__(self, solids, free_faces=0, shared=0):
        self.Solids = solids
        self.Faces = [object()] * (sum(len(s.Faces) for s in solids) + free_faces - shared)
        self.compound_volume_calls = 0

    @property
    def Volume(self):
        self.compound_volume_calls += 1
        return sum(s._volume for s in self.Solids)


def test_a_compound_of_solids_integrates_each_solid_once_for_volume_and_centre():
    solids = [_CountedSolid(2.0, 0.0), _CountedSolid(6.0, 4.0)]
    compound = _Compound(solids)
    result = serialize.serialize_shape(compound)
    assert result["Volume"] == 8.0 and result["CenterOfMass"]["x"] == 3.0
    assert [s.volume_calls for s in solids] == [1, 1] and compound.compound_volume_calls == 0


def test_a_compound_with_free_faces_or_shared_faces_keeps_the_shapes_own_volume():
    for extra in ({"free_faces": 2}, {"shared": 2}):
        compound = _Compound([_CountedSolid(2.0, 0.0), _CountedSolid(6.0, 4.0)], **extra)
        assert serialize.serialize_shape(compound)["Volume"] == 8.0
        assert compound.compound_volume_calls == 1


def test_broken_shape_returns_error_dict_instead_of_raising():
    result = serialize.serialize_shape(_BrokenShape())
    assert "error" in result
    assert "invalid shape" in result["error"]


def test_none_shape_returns_none():
    assert serialize.serialize_shape(None) is None


def test_serialize_object_survives_broken_shape():
    obj = types.SimpleNamespace(
        Name="Pad",
        Label="Pad",
        TypeId="PartDesign::Pad",
        PropertiesList=[],
        Placement=_StubPlacement(),
        Shape=_BrokenShape(),
    )
    result = serialize.serialize_object(obj)
    assert result["Name"] == "Pad"
    assert "error" in result["Shape"]


def test_out_and_in_lists_name_each_object_once_in_order():
    twice = types.SimpleNamespace(Name="Outer")
    obj = types.SimpleNamespace(OutList=[twice, types.SimpleNamespace(Name="Tool"), twice], InList=[])
    assert serialize._names(obj, "OutList") == ["Outer", "Tool"]
    assert serialize._names(obj, "InList") == []


def test_the_solid_count_is_blank_without_a_shape_and_zero_for_an_empty_or_unreadable_one():
    assert serialize.solid_count_of(types.SimpleNamespace()) is None
    assert serialize.solid_count_of(types.SimpleNamespace(Shape=types.SimpleNamespace(Solids=[]))) == 0
    assert serialize.solid_count_of(types.SimpleNamespace(Shape=types.SimpleNamespace(Solids=[1, 2]))) == 2
    assert serialize.solid_count_of(types.SimpleNamespace(Shape=types.SimpleNamespace())) == 0


def test_the_tools_own_bookkeeping_properties_are_not_serialized():
    class _Obj:
        Name = "Mesh"
        Label = "Mesh"
        TypeId = "Fem::FemMeshShapeBaseObjectPython"
        State = ["Up-to-date"]
        PropertiesList = ["Label", "McpMeshedShape"]
        Label = "Mesh"
        McpMeshedShape = '{"face_list": [' + "[1]," * 5000 + "[1]]}"
        Placement = _StubPlacement()
        Shape = _GoodShape()

        def isValid(self):
            return True

        def getStatusString(self):
            return "Valid"

    result = serialize.serialize_object(_Obj())
    assert "McpMeshedShape" not in result["Properties"] and "Label" in result["Properties"]
    assert "McpMeshedShape" not in str(result)
    # The name the FEM code stores its record under is the one left out.
    assert "McpMeshedShape" in serialize.INTERNAL_PROPERTIES


def test_the_integrals_can_be_left_out_and_the_cheap_fields_stay():
    result = serialize.serialize_shape(_GoodShape(), integrals=False)
    assert "Volume" not in result and "Area" not in result and "CenterOfMass" not in result
    assert result["FaceCount"] == 4 and result["SolidCount"] == 1
    assert result["BoundBox"] == [0.0, 0.0, 0.0, 1.0, 2.0, 3.0]
    full = serialize.serialize_shape(_GoodShape())
    assert full["Volume"] == 42.0 and full["Area"] == 10.0 and full["CenterOfMass"] == {"x": 0.5, "y": 1.0, "z": 1.5}
