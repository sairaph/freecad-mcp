"""FEM meshing: the mesh follows its solid, the default order, the face check, the all-zero result, single sub-element links."""

import importlib.util
import json
import sys
import types
from pathlib import Path

import pytest

from test_object_validation import FakeDocument, FakeObject, load_object_factory

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class Box:
    def __init__(self, low, high):
        self.XMin, self.YMin, self.ZMin = low
        self.XMax, self.YMax, self.ZMax = high

    def isValid(self):
        return True


class Face:
    """A face the signature reads: its surface type, area and centre."""

    def __init__(self, kind="Plane", area=100.0, centre=(0.0, 0.0, 0.0)):
        self.Surface = type(kind, (), {})()
        self.Area = area
        self.CenterOfMass = types.SimpleNamespace(x=centre[0], y=centre[1], z=centre[2])


class Solid:
    """A shape with the tight box, volume, area, centre of mass and faces the fingerprint reads."""

    def __init__(self, size=(60.0, 30.0, 4.0), faces=6, features=((("Cylinder"), 50.0, (10.0, 15.0, 2.0)),)):
        self.size = size
        self.Faces = [Face("Plane", 100.0 + i, (float(i), 1.0, 2.0)) for i in range(faces - len(features))] + [Face(*f) for f in features]
        self.Volume = size[0] * size[1] * size[2]
        self.Area = 2 * (size[0] * size[1] + size[0] * size[2] + size[1] * size[2])
        self.com = [size[0] / 2, size[1] / 2, size[2] / 2]

    def optimalBoundingBox(self, _a, _b):
        return Box((0, 0, 0), self.size)


def shape_object(name="Beam", **kw):
    return types.SimpleNamespace(Name=name, Shape=Solid(**kw))


class FemMesh:
    def __init__(self, nodes=654, low=(0, 0, 0), high=(60.0, 30.0, 4.0), element_nodes=10):
        self.NodeCount = nodes
        self.BoundBox = Box(low, high)
        self.Volumes = [1, 2]
        self._element_nodes = element_nodes
        self.VolumeCount = 2

    def getElementNodes(self, _id):
        return list(range(self._element_nodes))


class MeshObject:
    def __init__(self, shape, **kw):
        self.Name, self.Shape = "Mesh", shape
        self.FemMesh = FemMesh(**kw)
        self.PropertiesList = ["Shape", "ElementOrder", "CharacteristicLengthMax"]
        self.ElementOrder = "2nd"
        self.Proxy = types.SimpleNamespace(Type="Fem::FemMeshGmsh")
        self.status = {}

    def addProperty(self, kind, name, group, doc):
        self.PropertiesList.append(name)
        setattr(self, name, "")

    def setPropertyStatus(self, name, status):
        self.status[name] = status

    def getGroupOfProperty(self, name):
        return "Mesh Parameters" if name in ("ElementOrder", "CharacteristicLengthMax") else "Base"


@pytest.fixture
def fem_mesh(monkeypatch):
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintError=lambda _m: None, PrintWarning=lambda _m: None)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    monkeypatch.syspath_prepend(str(ADDON))
    serialize = types.ModuleType("rpc_server.serialize")
    serialize.tight_bound_box = lambda shape: shape.optimalBoundingBox(False, False)
    serialize.center_of_mass = lambda shape: dict(zip("xyz", shape.com))
    monkeypatch.setitem(sys.modules, "rpc_server.serialize", serialize)
    spec = importlib.util.spec_from_file_location("_fem_mesh_under_test", ADDON / "rpc_server" / "fem_mesh.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_a_mesh_stores_the_fingerprint_of_its_shape_in_a_hidden_property(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)
    stored = json.loads(mesh.McpMeshedShape)
    assert {k: v for k, v in stored.items() if k not in ("face_list", "nodes")} == {
        "box": [0, 0, 0, 60.0, 30.0, 4.0], "volume": 7200.0, "area": 4320.0, "com": [30.0, 15.0, 2.0], "faces": 6}
    assert len(stored["face_list"]) == 6 and stored["face_list"][-1] == ["Cylinder", 50.0, 10.0, 15.0, 2.0]
    assert stored["nodes"] == 654
    assert mesh.status["McpMeshedShape"] == "Hidden"


def test_a_changed_shape_is_found_by_its_fingerprint_and_an_unchanged_one_is_not(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    mesh.Shape.Shape = Solid(size=(60.0, 30.0, 3.0))
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"
    # A different face count with the same box and volume is a different shape too.
    mesh.Shape.Shape = Solid(faces=7)
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"


def test_a_mesh_without_a_fingerprint_is_compared_by_its_node_box(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    mesh.Shape.Shape = Solid(size=(60.0, 30.0, 3.0))
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "bounding box"
    # A coarse mesh of a curved shape falls a little short of the exact box: still the same shape.
    close = MeshObject(shape_object(), high=(60.0, 30.0, 3.95))
    close.Shape.Shape = Solid(size=(60.0, 30.0, 4.0))
    assert fem_mesh.mesh_changed_since_meshing(close) is None
    empty = MeshObject(shape_object(), nodes=0)
    empty.Shape.Shape = Solid(size=(1.0, 1.0, 1.0))
    assert fem_mesh.mesh_changed_since_meshing(empty) is None


def test_element_order_comes_from_the_solid_elements(fem_mesh) -> None:
    assert fem_mesh.element_order(MeshObject(shape_object(), element_nodes=10)) == "2nd"
    assert fem_mesh.element_order(MeshObject(shape_object(), element_nodes=4)) == "1st"
    odd = MeshObject(shape_object(), element_nodes=4)
    odd.FemMesh.Volumes = []
    assert fem_mesh.element_order(odd) == "2nd"  # no solid elements: the property
    info = fem_mesh.mesh_info(MeshObject(shape_object(), nodes=3746))
    assert info == {"name": "Mesh", "element_order": "2nd", "node_count": 3746}


def test_only_meshing_parameters_and_links_mesh_again(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    for name in ("Shape", "CharacteristicLengthMax", "ElementOrder", "CharacteristicLengthMax.Value", "MeshRegionList"):
        assert fem_mesh.changes_meshing(mesh, [name]) is True
    assert fem_mesh.changes_meshing(mesh, ["Label", "ViewObject", "Placement"]) is False
    assert fem_mesh.is_gmsh(mesh) is True
    assert fem_mesh.is_gmsh(types.SimpleNamespace(TypeId="Part::Box")) is False


def test_create_object_meshes_a_gmsh_mesh_second_order_unless_told_otherwise() -> None:
    doc = FakeDocument(FakeObject(Name="Mesh"))
    with load_object_factory(doc) as object_factory:
        mesh = types.SimpleNamespace(Name="Mesh", PropertiesList=["ElementOrder", "Shape"], ElementOrder="1st")
        generated = []
        object_factory.generate_mesh = lambda m: generated.append(m.ElementOrder) or 100
        # _create_fem_mesh sets the order before it meshes, and keeps a requested one.
        for props, expected in (({"Shape": "Beam"}, "2nd"), ({"Shape": "Beam", "ElementOrder": "1st"}, "1st")):
            mesh.ElementOrder = "1st"
            analysis = types.SimpleNamespace(addObject=lambda obj: [obj])
            beam = types.SimpleNamespace(Name="Beam")
            doc.Analysis = types.SimpleNamespace(addObject=lambda obj: [mesh])
            doc.getObject = lambda name, beam=beam: beam
            fem = types.ModuleType("femmesh")
            sys.modules.setdefault("femmesh", fem)
            obj = object_factory.Object(name="Mesh", type="Fem::FemMeshGmsh", properties=dict(props), analysis="Analysis")
            sys.modules["ObjectsFem"].makeMeshGmsh = lambda doc_, name: mesh
            mesh.Shape = None
            object_factory._create_fem_mesh(doc, obj)
            assert generated[-1] == expected, props


def load_fem_executor(monkeypatch):
    for name in ("FreeCAD", "ObjectsFem"):
        monkeypatch.setitem(sys.modules, name, types.ModuleType(name))
    sys.modules["FreeCAD"].Console = types.SimpleNamespace(PrintError=lambda _m: None, PrintWarning=lambda _m: None)
    transactions = types.ModuleType("rpc_server.transactions")
    transactions.active_document = transactions.transaction = None
    monkeypatch.setitem(sys.modules, "rpc_server.transactions", transactions)
    monkeypatch.syspath_prepend(str(ADDON))
    for name in ("rpc_server.fem_loads", "rpc_server.fem_mesh"):
        monkeypatch.delitem(sys.modules, name, raising=False)
    serialize = types.ModuleType("rpc_server.serialize")
    serialize.tight_bound_box = lambda shape: shape.optimalBoundingBox(False, False)
    serialize.center_of_mass = lambda shape: dict(zip("xyz", shape.com))
    monkeypatch.setitem(sys.modules, "rpc_server.serialize", serialize)
    property_mapper = types.ModuleType("rpc_server.property_mapper")
    property_mapper.quantity_text = str
    monkeypatch.setitem(sys.modules, "rpc_server.property_mapper", property_mapper)
    spec = importlib.util.spec_from_file_location("_fem_executor_under_test", ADDON / "rpc_server" / "fem_executor.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ShapeWithFaces:
    def __init__(self, faces):
        self.faces = faces

    def getElement(self, name):
        if name not in self.faces:
            raise ValueError("Index out of range")
        return object()


def test_a_constraint_naming_a_face_that_is_gone_is_found_before_solving(monkeypatch) -> None:
    executor = load_fem_executor(monkeypatch)
    beam = types.SimpleNamespace(Name="Beam", Shape=ShapeWithFaces({"Face1", "Face6"}))
    fixed = types.SimpleNamespace(Name="Fixed", References=[(beam, ["Face1"])])
    load = types.SimpleNamespace(Name="Load", References=[(beam, ["Face9"])])
    force = types.SimpleNamespace(Name="Axial", References=[(beam, ["Face6"])], Direction=(beam, ["Edge5"]))
    material = types.SimpleNamespace(Name="Alu")
    analysis = types.SimpleNamespace(Group=[fixed, load, force, material])
    assert executor.missing_references(analysis) == [("Load", "Beam", "Face9"), ("Axial", "Beam", "Edge5")]
    beam.Shape = ShapeWithFaces({"Face1", "Face6", "Face9", "Edge5"})
    assert executor.missing_references(analysis) == []


def test_the_run_meshes_a_changed_solid_again_and_warns_of_a_first_order_mesh(monkeypatch) -> None:
    executor = load_fem_executor(monkeypatch)
    beam = shape_object()
    mesh = MeshObject(beam)
    mesh.FemMesh = FemMesh(nodes=3746, element_nodes=4)
    mesh.Shape.Shape = Solid(size=(60.0, 30.0, 3.0))
    calls = []

    def generate(m):
        calls.append(m.Name)
        m.FemMesh = FemMesh(nodes=3888, element_nodes=4)
        return 3888

    monkeypatch.setattr(executor, "generate_mesh", generate)
    analysis = types.SimpleNamespace(Group=[mesh])
    assert executor._remesh_changed(analysis) == [
        {"mesh": "Mesh", "shape": "Beam", "found_by": "bounding box", "nodes_before": 3746, "nodes_after": 3888}
    ]
    assert calls == ["Mesh"]
    meshes, warnings = executor._mesh_notes(analysis)
    assert meshes == [{"name": "Mesh", "element_order": "1st", "node_count": 3888}]
    assert warnings == [executor._FIRST_ORDER_WARNING]
    mesh.FemMesh = FemMesh(nodes=3888, element_nodes=10)
    assert executor._mesh_notes(analysis)[1] == []


def test_a_force_direction_takes_the_forms_references_takes_for_one_sub_element() -> None:
    beam = types.SimpleNamespace(Name="Beam")
    doc = FakeDocument(FakeObject(Name="Axial"))
    doc.getObject = lambda name: beam if name == "Beam" else None
    with load_object_factory(doc):
        property_mapper = sys.modules["rpc_server.property_mapper"]
        for form in ({"object_name": "Beam", "edge": "Edge5"}, ["Beam", "Edge5"], ["Beam", ["Edge5"]], {"object_name": "Beam", "edges": ["Edge5"]}):
            force = types.SimpleNamespace(PropertiesList=["Direction", "Force"], Direction=None,
                                          getTypeIdOfProperty=lambda name: "App::PropertyLinkSub" if name == "Direction" else "App::PropertyForce")
            property_mapper.set_object_property(doc, force, {"Direction": form})
            assert force.Direction == (beam, ["Edge5"]), form
        assert property_mapper.parse_reference_entry({"object_name": "Beam", "face": "Face2"}) == ("Beam", "Face2")
        with pytest.raises(ValueError):
            property_mapper.set_object_property(doc, force, {"Direction": ["Missing", "Edge5"]})


def test_a_feature_moved_rotated_or_mirrored_is_seen_but_a_recompute_is_not(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)

    # Nothing changed, the faces listed in another order and with float noise: still the same shape.
    same = Solid()
    same.Faces = list(reversed(same.Faces))
    for face in same.Faces:
        face.Area += 1e-11
        face.CenterOfMass.x += 1e-11
    mesh.Shape.Shape = same
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None

    # A hole moved from x 10 to x 40: box, volume, area, face count and the face types are unchanged.
    moved = Solid(features=(("Cylinder", 50.0, (40.0, 15.0, 2.0)),))
    mesh.Shape.Shape = moved
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"
    # Mirrored in y, rotated in place (areas swap with the centres): other face signatures.
    mesh.Shape.Shape = Solid(features=(("Cylinder", 50.0, (10.0, 15.0, 2.0)), ))
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    mirrored = Solid(features=(("Cylinder", 50.0, (10.0, 15.0, 2.0)),))
    mirrored.Faces[0].CenterOfMass.y = 40.0
    mesh.Shape.Shape = mirrored
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"
    rotated = Solid(features=(("Plane", 50.0, (10.0, 15.0, 2.0)),))
    mesh.Shape.Shape = rotated
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"


def test_a_fingerprint_without_the_newer_fields_is_still_compared_by_the_older_ones(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    mesh.PropertiesList.append(fem_mesh.FINGERPRINT_PROPERTY)
    mesh.McpMeshedShape = json.dumps({"box": [0, 0, 0, 60.0, 30.0, 4.0], "volume": 7200.0, "faces": 6})
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    mesh.Shape.Shape = Solid(size=(60.0, 30.0, 3.0))
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"


def test_a_run_with_loads_and_no_displacement_data_is_not_called_all_zero(monkeypatch) -> None:
    executor = load_fem_executor(monkeypatch)
    load = [{"name": "Load"}]
    assert executor._unusable_result_error(load, []) == executor._NO_DISPLACEMENT_ERROR
    assert executor._unusable_result_error(load, [0.0, 0.0]) == executor._ALL_ZERO_ERROR
    assert executor._unusable_result_error(load, [0.0, 0.2]) is None
    assert executor._unusable_result_error([], []) is None
    assert executor._unusable_result_error([], [0.0]) is None
    assert "no displacement data" in executor._NO_DISPLACEMENT_ERROR and "all zero" not in executor._NO_DISPLACEMENT_ERROR


def test_generate_mesh_leaves_the_mesh_recomputed(fem_mesh, monkeypatch) -> None:
    mesh = MeshObject(shape_object())
    recomputes = []
    mesh.Document = types.SimpleNamespace(recompute=lambda: recomputes.append(1))
    gmshtools = types.ModuleType("femmesh.gmshtools")
    gmshtools.GmshTools = lambda obj: types.SimpleNamespace(create_mesh=lambda: None)
    monkeypatch.setitem(sys.modules, "femmesh", types.ModuleType("femmesh"))
    monkeypatch.setitem(sys.modules, "femmesh.gmshtools", gmshtools)
    assert fem_mesh.generate_mesh(mesh) == 654
    assert recomputes == [1]
    assert fem_mesh.FINGERPRINT_PROPERTY in mesh.PropertiesList


def test_a_restored_record_with_a_different_mesh_is_found_after_an_undo(fem_mesh) -> None:
    mesh = MeshObject(shape_object(), nodes=3746)
    fem_mesh.store_fingerprint(mesh)
    assert json.loads(mesh.McpMeshedShape)["nodes"] == 3746
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    # The undo put the record back but left the newer mesh in the mesh object.
    mesh.FemMesh = FemMesh(nodes=1003)
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "mesh"
    # A shape change is named first.
    mesh.Shape.Shape = Solid(size=(60.0, 30.0, 3.0))
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"


def test_faces_are_compared_with_a_tolerance_so_no_rounding_step_can_flip_the_result(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)
    scale = 60.0
    tolerance = fem_mesh._FINGERPRINT_TOLERANCE * scale
    # Values placed on the edges of any grid cell move by far less than the tolerance: still the same shape.
    for shift in (0.0, tolerance / 4, -tolerance / 4):
        shape = Solid()
        for face in shape.Faces:
            face.CenterOfMass.x = round(face.CenterOfMass.x / (2 * tolerance)) * 2 * tolerance + shift
        mesh.Shape.Shape = shape
        mesh.McpMeshedShape = json.dumps({**json.loads(mesh.McpMeshedShape), "face_list": fem_mesh._face_list(shape)})
        again = Solid()
        for face, base in zip(again.Faces, shape.Faces):
            face.CenterOfMass.x = base.CenterOfMass.x + tolerance / 8
        mesh.Shape.Shape = again
        assert fem_mesh.mesh_changed_since_meshing(mesh) is None, shift
    # A move of several tolerances is a different shape.
    moved = Solid()
    moved.Faces[-1].CenterOfMass.x += 10 * tolerance
    mesh.Shape.Shape = moved
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"


def test_the_faces_are_read_only_when_the_cheaper_fields_all_match(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)
    reads = []

    class Counting(Face):
        @property
        def CenterOfMass(self):
            reads.append(1)
            return self._centre

        @CenterOfMass.setter
        def CenterOfMass(self, value):
            self._centre = value

    shape = Solid(size=(60.0, 30.0, 3.0))
    shape.Faces = [Counting(*("Plane", 1.0, (0.0, 0.0, 0.0))) for _ in range(6)]
    mesh.Shape.Shape = shape
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"
    assert reads == []  # the box already differed
    same_box = Solid()
    same_box.Volume += 100.0
    same_box.Faces = shape.Faces
    mesh.Shape.Shape = same_box
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"
    assert reads == []  # the volume differed


def test_a_record_that_cannot_be_compared_falls_back_on_the_node_box(fem_mesh) -> None:
    mesh = MeshObject(shape_object())
    fem_mesh.store_fingerprint(mesh)

    class Unreadable(Face):
        @property
        def CenterOfMass(self):
            raise RuntimeError("bad face")

        @CenterOfMass.setter
        def CenterOfMass(self, value):
            pass

    broken = Solid()
    broken.Faces = [Unreadable() for _ in range(6)]
    mesh.Shape.Shape = broken
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None  # the node box still fits
    thinner = Solid(size=(60.0, 30.0, 3.0))
    thinner.Faces = broken.Faces
    mesh.Shape.Shape = thinner
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "fingerprint"  # the cheaper fields already differ
    # A record that is not JSON is the same case: only the node box is left to compare.
    mesh.McpMeshedShape = "not json"
    assert fem_mesh.mesh_changed_since_meshing(mesh) == "bounding box"
    mesh.Shape.Shape = broken
    assert fem_mesh.mesh_changed_since_meshing(mesh) is None
    # No record can be made from an unreadable shape either: the mesh is then checked by its node box.
    assert fem_mesh.fingerprint(mesh.Shape) is None


def test_the_record_property_is_one_the_serializer_leaves_out(fem_mesh) -> None:
    serialize_path = ADDON / "rpc_server" / "serialize.py"
    assert f'INTERNAL_PROPERTIES = frozenset({{"{fem_mesh.FINGERPRINT_PROPERTY}"}})' in serialize_path.read_text(encoding="utf-8")
