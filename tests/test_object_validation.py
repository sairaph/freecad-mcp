from contextlib import contextmanager
from dataclasses import dataclass, field
import importlib.util
from pathlib import Path
import sys
import types
from typing import Iterator


ADDON_DIR = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"
if str(ADDON_DIR) not in sys.path:
    sys.path.insert(0, str(ADDON_DIR))

from rpc_server.object_validation import invalid_object_row, object_validity_error

from test_gui_dispatch import reset_transactions_import


OBJECT_FACTORY_PATH = (
    ADDON_DIR / "rpc_server" / "object_factory.py"
)


class FakeConsole:
    messages: list[tuple[str, str]] = []

    @classmethod
    def PrintMessage(cls, message: str) -> None:
        cls.messages.append(("message", message))

    @classmethod
    def PrintError(cls, message: str) -> None:
        cls.messages.append(("error", message))


class FakeDocument:
    def __init__(self, obj: object):
        self.Name = "Doc"
        self.obj = obj
        self.recompute_count = 0
        # Like a real document's Objects, kept in step with addObject and
        # removeObject: create_object_gui reads it to find what a failed
        # creation left behind.
        self.Objects: list[object] = []

    def addObject(self, _obj_type: str, _name: str) -> object:
        if self.obj not in self.Objects:
            self.Objects.append(self.obj)
        return self.obj

    def removeObject(self, name: str) -> None:
        self.Objects = [o for o in self.Objects if getattr(o, "Name", None) != name]

    def getObject(self, name: str) -> object | None:
        return self.obj if name == getattr(self.obj, "Name", None) else None

    def recompute(self) -> None:
        self.recompute_count += 1


@contextmanager
def load_object_factory(
    doc: FakeDocument,
) -> Iterator[types.ModuleType]:
    """Load object_factory with minimal FreeCAD/ObjectsFem test doubles."""
    module_names = ["FreeCAD", "ObjectsFem", "rpc_server.property_mapper", "rpc_server.transactions"]
    missing = object()
    saved = {name: sys.modules.get(name, missing) for name in module_names}

    freecad = types.ModuleType("FreeCAD")
    freecad.Document = FakeDocument
    freecad.DocumentObject = object
    freecad.Vector = type("Vector", (), {})
    freecad.Console = FakeConsole
    freecad.getDocument = lambda _name: doc
    # rpc_server.transactions wraps create_object_gui/edit_object_gui in a
    # FreeCAD transaction; a real FreeCAD always has these, so the fake needs
    # them.
    freecad.getActiveTransaction = lambda: None
    freecad.setActiveTransaction = lambda _name, persist=False: 1
    freecad.closeActiveTransaction = lambda abort=False, id=0: None
    # object_factory.active_document reads FreeCAD.ActiveDocument and calls
    # FreeCAD.setActiveDocument(name); a real FreeCAD always has both, so the
    # fake needs them too.
    freecad.ActiveDocument = None

    def _set_active_document(name: str) -> None:
        # Mirrors Application::setActiveDocument (Application.cpp:1059-1074):
        # "" only clears the internal pointer, not the Python-visible
        # FreeCAD.ActiveDocument (it returns before the code that syncs that
        # attribute); an unknown name raises.
        if name == "":
            return
        elif name == doc.Name:
            freecad.ActiveDocument = doc
        else:
            raise RuntimeError(f"Try to activate unknown document '{name}'")

    freecad.setActiveDocument = _set_active_document

    sys.modules["FreeCAD"] = freecad
    sys.modules["ObjectsFem"] = types.ModuleType("ObjectsFem")
    sys.modules.pop("rpc_server.property_mapper", None)
    reset_transactions_import()

    module_name = f"_object_factory_test_{id(doc)}"
    try:
        spec = importlib.util.spec_from_file_location(module_name, OBJECT_FACTORY_PATH)
        if spec is None or spec.loader is None:
            raise ImportError(f"Cannot load object_factory from {OBJECT_FACTORY_PATH}")
        module = importlib.util.module_from_spec(spec)
        sys.modules[module_name] = module
        spec.loader.exec_module(module)
        yield module
    finally:
        sys.modules.pop(module_name, None)
        for name, value in saved.items():
            if value is missing:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = value


@dataclass
class FakeObject:
    Name: str = "Object"
    TypeId: str = "Part::Feature"
    valid: bool = True
    State: list[str] = field(default_factory=lambda: ["Up-to-date"])
    status: str = ""
    Shape: object | None = None

    def isValid(self) -> bool:
        return self.valid

    def getStatusString(self) -> str:
        return self.status


def test_valid_shapeless_object_is_not_rejected() -> None:
    obj = FakeObject(Name="Body", Shape=None)

    assert object_validity_error(obj) is None


def test_invalid_object_reports_name_state_and_freecad_reason() -> None:
    obj = FakeObject(
        Name="Pad",
        valid=False,
        State=["Touched", "Invalid"],
        status="Linked shape object is empty",
    )

    error = object_validity_error(obj)

    assert error is not None
    assert "Pad" in error
    assert "Touched, Invalid" in error
    assert "Linked shape object is empty" in error


def test_object_without_validity_api_is_left_unchanged() -> None:
    obj = type("LegacyObject", (), {"Name": "Legacy"})()

    assert object_validity_error(obj) is None


def test_touched_object_is_rejected_even_when_is_valid_returns_true() -> None:
    obj = FakeObject(valid=True, State=["Touched"], status="Touched")

    error = object_validity_error(obj)

    assert error is not None
    assert "Touched" in error


def test_invalid_state_is_used_when_validity_api_is_missing() -> None:
    obj = type(
        "LegacyObject",
        (),
        {"Name": "Legacy", "State": ["Invalid"]},
    )()

    error = object_validity_error(obj)

    assert error is not None
    assert "Invalid" in error


def test_validity_check_exception_is_reported_as_failure() -> None:
    class BrokenObject:
        Name = "Broken"

        def isValid(self) -> bool:
            raise RuntimeError("invalid internal state")

    error = object_validity_error(BrokenObject())

    assert error is not None
    assert "validity could not be checked" in error
    assert "invalid internal state" in error


def test_create_object_returns_failure_with_created_object_name() -> None:
    created = FakeObject(
        Name="Pad",
        valid=False,
        State=["Touched", "Invalid"],
        status="Linked shape object is empty",
    )
    doc = FakeDocument(created)

    with load_object_factory(doc) as object_factory:
        request = object_factory.Object(
            name="Pad",
            type="PartDesign::Pad",
            properties={},
        )
        result = object_factory.create_object_gui("Doc", request)

    assert result["success"] is False
    assert result["object_name"] == "Pad"
    assert "Linked shape object is empty" in result["error"]
    assert doc.recompute_count == 1


def test_create_object_accepts_valid_shapeless_object() -> None:
    created = FakeObject(Name="Body", Shape=None)
    doc = FakeDocument(created)

    with load_object_factory(doc) as object_factory:
        request = object_factory.Object(
            name="Body",
            type="PartDesign::Body",
            properties={},
        )
        result = object_factory.create_object_gui("Doc", request)

    assert result == {
        "success": True,
        "object_name": "Body",
        "transaction": "MCP: create_object",
        "transaction_merged": False,
    }


def test_edit_object_returns_failure_with_existing_object_name() -> None:
    existing = FakeObject(
        Name="Cut",
        valid=False,
        State=["Touched", "Invalid"],
        status="Base or Tool is not set",
    )
    doc = FakeDocument(existing)

    with load_object_factory(doc) as object_factory:
        request = object_factory.Object(name="Cut", properties={})
        result = object_factory.edit_object_gui("Doc", request)

    assert result["success"] is False
    assert result["object_name"] == "Cut"
    assert "Base or Tool is not set" in result["error"]
    assert doc.recompute_count == 1


class FakeFillet(FakeObject):
    """A Part::Fillet: Base and Edges as FreeCAD keeps them."""

    def __init__(self, valid: bool = True):
        super().__init__(Name="Round", TypeId="Part::Fillet", valid=valid, State=["Up-to-date"] if valid else ["Invalid"])
        self.Label = "Round"
        self.Base = None
        self.Edges: list = []
        self.PropertiesList = ["Base", "Edges"]

    def getTypeIdOfProperty(self, name: str) -> str:
        return "Part::PropertyFilletEdges" if name == "Edges" else "App::PropertyLink"


class FakeDocumentWithSource(FakeDocument):
    def __init__(self, obj: object, source: object):
        super().__init__(obj)
        self.source = source
        self.Objects.append(source)

    def getObject(self, name: str) -> object | None:
        return self.source if name == self.source.Name else super().getObject(name)


def outer_box() -> types.SimpleNamespace:
    return types.SimpleNamespace(Name="Outer", Shape=types.SimpleNamespace(Edges=[object()] * 12), ViewObject=types.SimpleNamespace(Visibility=True))


def test_create_object_lists_the_fillet_edges_and_hides_the_source() -> None:
    source = outer_box()
    doc = FakeDocumentWithSource(FakeFillet(), source)

    with load_object_factory(doc) as object_factory:
        request = object_factory.Object(name="Round", type="Part::Fillet", properties={"Base": "Outer", "Edges": ["Edge1", "Edge3"], "Radius": 4})
        result = object_factory.create_object_gui("Doc", request)

    assert result["success"] is True
    assert result["edges"] == ["Edge1 r4", "Edge3 r4"]
    assert result["hidden"] == ["Outer"]
    assert source.ViewObject.Visibility is False


def test_create_object_with_a_problem_hides_nothing() -> None:
    source = outer_box()
    doc = FakeDocumentWithSource(FakeFillet(valid=False), source)

    with load_object_factory(doc) as object_factory:
        request = object_factory.Object(name="Round", type="Part::Fillet", properties={"Base": "Outer", "Edges": ["Edge1"], "Radius": 4})
        result = object_factory.create_object_gui("Doc", request)

    assert result["success"] is False and "hidden" not in result
    assert source.ViewObject.Visibility is True


def test_edit_object_lists_the_edges_and_hides_the_source_only_when_it_is_relinked() -> None:
    source = outer_box()
    fillet = FakeFillet()
    fillet.Base = source
    fillet.Edges = [(1, 2.0, 2.0)]
    doc = FakeDocumentWithSource(fillet, source)

    with load_object_factory(doc) as object_factory:
        result = object_factory.edit_object_gui("Doc", object_factory.Object(name="Round", properties={"Radius": 3}))
        assert result["edges"] == ["Edge1 r3"] and "hidden" not in result
        assert source.ViewObject.Visibility is True
        result = object_factory.edit_object_gui("Doc", object_factory.Object(name="Round", properties={"Base": "Outer"}))

    assert result["hidden"] == ["Outer"]
    assert source.ViewObject.Visibility is False


class FakeShape:
    def __init__(self, solids: int = 1, null: bool = False, volume: float = 9503.5):
        self.Solids = [types.SimpleNamespace(Faces=[object()] * 6, Volume=volume / solids)] * solids if solids else []
        self.Shells = [object()] * solids
        self.Faces = [object()] * (6 * solids)
        self.Edges = [object()] * (12 * solids)
        self.Volume = volume
        self.ShapeType = "Compound"
        self._null = null
        self.BoundBox = types.SimpleNamespace(
            isValid=lambda: solids > 0, XLength=40.0, YLength=20.0, ZLength=12.0,
            XMin=0.0, YMin=0.0, ZMin=0.0, XMax=40.0, YMax=20.0, ZMax=12.0,
        )

    def isNull(self) -> bool:
        return self._null


class FakeBoolean(FakeObject):
    def __init__(self, type_id: str, shape: FakeShape, inputs: list):
        super().__init__(Name="Clip", TypeId=type_id, Shape=shape)
        self.Label = "Clip"
        self.OutList = inputs
        self.Base = inputs[0] if inputs else None
        self.Tool = inputs[1] if len(inputs) > 1 else None
        self.PropertiesList: list = []


def shaped(name: str, solids: int = 1, visible: bool = True, volume: float = 9503.5) -> FakeObject:
    box = FakeObject(Name=name, Shape=FakeShape(solids, volume=volume))
    box.ViewObject = types.SimpleNamespace(Visibility=visible)
    return box


def test_create_object_reports_the_shape_and_the_inputs_that_went_hidden() -> None:
    block, hole = shaped("Block", volume=12000.0), shaped("Hole")
    clip = FakeBoolean("Part::Cut", FakeShape(), [block, hole])
    doc = FakeDocument(clip)
    doc.Objects.extend([block, hole])

    class HidingDoc(FakeDocument):
        def recompute(self) -> None:
            block.ViewObject.Visibility = hole.ViewObject.Visibility = False

    doc.__class__ = HidingDoc
    with load_object_factory(doc) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="Part::Cut", properties={}))

    assert result["shape"] == {"solids": 1, "shells": 1, "faces": 6, "edges": 12, "size": [40.0, 20.0, 12.0], "volume": 9503.5}
    assert result["hidden"] == ["Block", "Hole"]
    assert "warning" not in result


def test_a_result_without_a_solid_warns_when_an_input_has_one() -> None:
    block, far = shaped("Block"), shaped("Far")
    clip = FakeBoolean("Part::Common", FakeShape(solids=0), [block, far])
    doc = FakeDocument(clip)

    with load_object_factory(doc) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="Part::Common", properties={}))
        clip.TypeId = "Part::Section"
        quiet = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))
        clip.TypeId = "Part::Cut"
        edit = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))

    assert result["success"] is True
    assert result["warning"] == "The result holds no solid: its inputs do not overlap. Check their Placement."
    assert result["shape"]["solids"] == 0 and result["shape"]["size"] is None
    assert "warning" not in quiet
    assert "the tool removes all of the base" in edit["warning"]


def test_no_warning_when_no_input_holds_a_solid_and_none_for_a_null_shape_summary() -> None:
    sketch = FakeObject(Name="Sketch", Shape=FakeShape(solids=0))
    wire = FakeBoolean("Part::Extrusion", FakeShape(solids=0), [sketch])
    doc = FakeDocument(wire)
    with load_object_factory(doc) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="Part::Extrusion", properties={}))
        wire.Shape = FakeShape(null=True)
        nulled = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))
    assert "warning" not in result
    assert nulled["shape"] == {"null": True}


def test_an_object_without_a_shape_has_no_shape_field() -> None:
    sheet = FakeObject(Name="Sheet", TypeId="Spreadsheet::Sheet")
    doc = FakeDocument(sheet)
    with load_object_factory(doc) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Sheet", type="Spreadsheet::Sheet", properties={}))
    assert "shape" not in result and "warning" not in result


def test_a_feature_that_is_not_a_solid_boolean_never_warns_of_an_empty_result() -> None:
    # A sketch attached to a box face and an extrusion with Solid false have a
    # solid in their OutList and none in their shape, by design.
    box = shaped("Box")
    for type_id in ("Sketcher::SketchObject", "Part::Extrusion", "Draft::Wire"):
        feature = FakeBoolean(type_id, FakeShape(solids=0), [box])
        with load_object_factory(FakeDocument(feature)) as object_factory:
            result = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))
        assert result["success"] is True and "warning" not in result, type_id
        assert result["shape"]["solids"] == 0


def test_the_inputs_of_a_multi_boolean_and_a_fillet_are_read_from_their_own_properties() -> None:
    box = shaped("Box")
    multi = FakeBoolean("Part::MultiFuse", FakeShape(solids=0), [box])
    multi.Shapes = [box]
    fillet = FakeBoolean("Part::Fillet", FakeShape(solids=0), [box])
    for feature in (multi, fillet):
        with load_object_factory(FakeDocument(feature)) as object_factory:
            result = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))
        assert result["warning"].startswith("The result holds no solid"), feature.TypeId


def test_an_empty_body_has_no_shape_but_a_feature_with_a_null_shape_keeps_it() -> None:
    body = FakeBoolean("PartDesign::Body", FakeShape(null=True), [])
    body.Group = []
    with load_object_factory(FakeDocument(body)) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="PartDesign::Body", properties={}))
    assert result["success"] is True and "shape" not in result

    cut = FakeBoolean("Part::Cut", FakeShape(null=True), [shaped("Block"), shaped("Hole")])
    with load_object_factory(FakeDocument(cut)) as object_factory:
        result = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="Part::Cut", properties={}))
    assert result["shape"] == {"null": True}
    assert "warning" in result


def test_a_cut_whose_tool_misses_the_base_warns_on_create_and_update() -> None:
    base, tool = shaped("Block", volume=9503.5), shaped("Far")
    clip = FakeBoolean("Part::Cut", FakeShape(volume=9503.5), [base, tool])
    with load_object_factory(FakeDocument(clip)) as object_factory:
        created = object_factory.create_object_gui("Doc", object_factory.Object(name="Clip", type="Part::Cut", properties={}))
        edited = object_factory.edit_object_gui("Doc", object_factory.Object(name="Clip", properties={}))
    expected = "The tool does not reach the base: nothing was removed. Check their Placement."
    assert created["warning"] == expected and edited["warning"] == expected


def failing(name: str, type_id: str = "Part::Fillet", status: str = "", states=("Touched", "Invalid"), **links) -> types.SimpleNamespace:
    obj = types.SimpleNamespace(
        Name=name, Label=name, TypeId=type_id, State=list(states), OutList=[], isValid=lambda: "Invalid" not in states,
        getStatusString=lambda: status,
    )
    for key, value in links.items():
        setattr(obj, key, value)
    return obj


def test_a_fillet_that_lost_an_edge_says_what_to_do() -> None:
    base = types.SimpleNamespace(Name="Stand")
    for message in ("Missing edge link: ;Edge7;:M;FUS;:H7bb:7,E.Edge26.", "NCollection_IndexedMap::FindKey"):
        fillet = failing("StandFillet", status=message, Base=base)
        row = invalid_object_row(fillet)
        assert row["status"].startswith(message.rstrip("."))
        assert row["status"].endswith("An edge it rounds no longer exists in Stand after the change. Call list_subelements on Stand and set Edges again.")
        assert "Call list_subelements on Stand" in object_validity_error(fillet)


def test_a_rounding_that_is_too_large_says_so_and_other_failures_are_left_alone() -> None:
    base = types.SimpleNamespace(Name="Box")
    fillet = failing("Round", status="BRep_API: command not done", Base=base)
    assert invalid_object_row(fillet)["status"].endswith("The radius is probably too large for these edges: try a smaller radius or fewer edges, and leave out degenerate edges.")
    chamfer = failing("Cut", type_id="Part::Chamfer", status="BRep_API: command not done", Base=base)
    assert "The size is probably too large for these edges: try a smaller size or fewer edges, and leave out degenerate edges." in invalid_object_row(chamfer)["status"]
    assert invalid_object_row(failing("Cut", type_id="Part::Cut", status="BRep_API: command not done"))["status"] == "BRep_API: command not done"
    assert invalid_object_row(failing("Round", status="Some other message", Base=base))["status"] == "Some other message"


def test_a_touched_object_names_the_nearest_failed_object_it_waits_for() -> None:
    fillet = failing("StandFillet", status="NCollection_IndexedMap::FindKey", Base=types.SimpleNamespace(Name="Stand"))
    middle = failing("Middle", type_id="Part::Fuse", status="Touched.", states=("Touched",))
    middle.OutList = [fillet]
    final = failing("Final", type_id="Part::Cut", status="Touched.", states=("Touched",))
    final.OutList = [types.SimpleNamespace(Name="Tool", State=["Up-to-date"], OutList=[], isValid=lambda: True), middle]

    row = invalid_object_row(final)
    assert row["status"] == "waits for StandFillet, which failed."
    assert row["waits_for"] == "StandFillet"
    assert "waits for StandFillet, which failed." in object_validity_error(final)
    # The failed object itself, and a Touched object with no failed dependency, say what they said before.
    assert invalid_object_row(fillet)["waits_for"] == ""
    lone = failing("Lone", type_id="Part::Cut", status="Touched.", states=("Touched",))
    assert invalid_object_row(lone) == {"name": "Lone", "label": "Lone", "type": "Part::Cut", "state": ["Touched"], "status": "Touched.", "waits_for": ""}


def test_the_dependency_search_runs_once_per_reported_object(monkeypatch) -> None:
    from rpc_server import object_validation

    fillet = failing("StandFillet", status="NCollection_IndexedMap::FindKey", Base=types.SimpleNamespace(Name="Stand"))
    final = failing("Final", type_id="Part::Cut", status="Touched.", states=("Touched",))
    final.OutList = [fillet]
    calls = []
    real = object_validation.failed_dependency
    monkeypatch.setattr(object_validation, "failed_dependency", lambda obj: calls.append(obj.Name) or real(obj))

    report = object_validation.invalid_objects_report([fillet, final])

    assert report["invalid_count"] == 2
    assert calls == ["StandFillet", "Final"]


def test_an_edge_that_cannot_be_rounded_says_which_edges_to_leave_out() -> None:
    base = types.SimpleNamespace(Name="Soft")
    expected = "An edge in Edges cannot be rounded: call list_subelements on Soft and leave out edges marked degenerate or smooth."
    for type_id in ("Part::Fillet", "Part::Chamfer"):
        row = invalid_object_row(failing("Round", type_id=type_id, status="There are no suitable edges for chamfer or fillet", Base=base))
        assert row["status"] == "There are no suitable edges for chamfer or fillet. " + expected
    other = failing("Cut", type_id="Part::Cut", status="There are no suitable edges for chamfer or fillet", Base=base)
    assert invalid_object_row(other)["status"] == "There are no suitable edges for chamfer or fillet"
