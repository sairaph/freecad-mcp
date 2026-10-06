"""PartDesign bodies, sketch support and geometry, and PartDesign Fillet and Chamfer edges."""

import importlib
import math
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"
ROLES = ("X_Axis", "Y_Axis", "Z_Axis", "XY_Plane", "XZ_Plane", "YZ_Plane")


class Vector:
    def __init__(self, x=0.0, y=0.0, z=0.0):
        self.x, self.y, self.z = x, y, z

    def distanceToPoint(self, other):
        return math.dist((self.x, self.y, self.z), (other.x, other.y, other.z))


class Shape:
    """Shapes of a sketch's wires and of a solid's edges and faces."""

    def __init__(self, wires=(), edges=0, faces=()):
        self.Wires = [types.SimpleNamespace(isClosed=lambda closed=c: closed) for c in wires]
        self.Edges = [object()] * edges
        self.faces = set(faces)

    def getElement(self, name):
        if name not in self.faces:
            raise ValueError("Index out of range")
        return object()


class Obj:
    def __init__(self, name, type_id="Part::Feature", shape=None):
        self.Name, self.TypeId, self.Shape = name, type_id, shape


class Body(Obj):
    def __init__(self, name, number=0):
        super().__init__(name, "PartDesign::Body")
        suffix = f"{number:03d}" if number else ""
        self.Group = []
        self.Tip = None
        self.features = [types.SimpleNamespace(Name=role + suffix, Role=role, TypeId="App::Plane") for role in ROLES]
        self.Origin = types.SimpleNamespace(Name="Origin" + suffix, OriginFeatures=self.features)


class Doc:
    Name = "Doc"

    def __init__(self, *objects):
        self.Objects = []
        for o in objects:
            self.add(o)

    def add(self, obj):
        self.Objects.append(obj)
        for feature in getattr(obj, "features", []):
            self.Objects.append(feature)
        return obj

    def getObject(self, name):
        return next((o for o in self.Objects if o.Name == name), None)


class Sketch:
    def __init__(self, geometry=0):
        self.TypeId = "Sketcher::SketchObject"
        self.Name = "Sketch"
        self.GeometryCount = geometry
        self.PropertiesList = ["AttachmentSupport", "MapMode"]
        self.MapMode = "Deactivated"
        self.Shape = Shape(wires=[True])
        self.calls = []
        self.geometry = []

    def deleteAllGeometry(self):
        self.calls.append("delete")
        self.geometry = []

    def addGeometry(self, geo, construction):
        self.calls.append("add")
        self.geometry.append(geo)
        return len(self.geometry) - 1

    def addConstraint(self, constraint):
        self.calls.append(constraint)


@pytest.fixture
def pd(monkeypatch):
    freecad = types.ModuleType("FreeCAD")
    freecad.Vector = Vector
    freecad.Console = types.SimpleNamespace(PrintError=lambda _m: None)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    part = types.ModuleType("Part")
    part.LineSegment = lambda a, b: ("line", (a.x, a.y), (b.x, b.y))
    part.Circle = lambda c, n, r: ("circle", (c.x, c.y), r)
    part.ArcOfCircle = lambda circle, a, b: ("arc", circle[1], circle[2], round(math.degrees(a), 6), round(math.degrees(b), 6))
    monkeypatch.setitem(sys.modules, "Part", part)
    sketcher = types.ModuleType("Sketcher")
    sketcher.Constraint = lambda *args: args
    monkeypatch.setitem(sys.modules, "Sketcher", sketcher)
    monkeypatch.syspath_prepend(str(ADDON))
    for name in ("rpc_server.partdesign", "rpc_server.property_mapper"):
        monkeypatch.delitem(sys.modules, name, raising=False)
    return importlib.import_module("rpc_server.partdesign")


def test_a_feature_goes_into_the_body_its_links_point_into_else_the_only_body(pd) -> None:
    body, other = Body("Body"), Body("Body2", 1)
    sketch = Obj("Sketch", "Sketcher::SketchObject")
    body.Group.append(sketch)
    doc = Doc(body, sketch)
    assert pd.plan_create(doc, "PartDesign::Pad", {"Profile": "Sketch", "Length": 10}, None).body is body
    assert pd.plan_create(doc, "PartDesign::Pad", {"Profile": ["Sketch", []]}, None).body is body
    doc.add(other)
    assert pd.plan_create(doc, "PartDesign::Pad", {"Profile": "Sketch"}, None).body is body
    assert pd.plan_create(doc, "PartDesign::Pad", {"Length": 3}, "Body2").body is other
    lone = Doc(Body("Only"))
    assert pd.plan_create(lone, "PartDesign::Pocket", {}, None).body.Name == "Only"


def test_a_feature_with_no_body_or_with_several_and_no_hint_is_refused(pd) -> None:
    with pytest.raises(ValueError, match="needs a Body, and the document has none"):
        pd.plan_create(Doc(), "PartDesign::Pad", {}, None)
    doc = Doc(Body("Body"), Body("Body2", 1))
    with pytest.raises(ValueError, match=r"several \(Body, Body2\): pass body_name"):
        pd.plan_create(doc, "PartDesign::Pad", {"Length": 3}, None)
    with pytest.raises(ValueError, match="body_name 'Nope' is not a PartDesign::Body"):
        pd.plan_create(doc, "PartDesign::Pad", {}, "Nope")
    with pytest.raises(ValueError, match="body_name is for PartDesign features and sketches"):
        pd.plan_create(doc, "Part::Box", {}, "Body")


def test_the_body_itself_and_other_objects_are_made_as_before(pd) -> None:
    doc = Doc(Body("Body"))
    assert pd.plan_create(doc, "PartDesign::Body", {"Label": "B"}, None).body is None
    assert pd.plan_create(doc, "Part::Box", {"Length": 2}, None).body is None


@pytest.mark.parametrize(
    "form",
    [
        {"object_name": "Pad", "face": "Face6"},
        ["Pad", "Face6"],
        ["Pad", ["Face6"]],
        [["Pad", ["Face6"]]],
        [["Pad", "Face6"]],
        [{"object_name": "Pad", "faces": ["Face6"]}],
    ],
)
def test_every_form_of_a_face_support_resolves_and_puts_the_sketch_in_its_body(pd, form) -> None:
    body = Body("Body")
    pad = Obj("Pad", "PartDesign::Pad", Shape(faces={"Face6"}))
    body.Group.append(pad)
    doc = Doc(body, pad)
    plan = pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": form}, None)
    assert plan.body is body
    assert plan.properties["AttachmentSupport"] == [(pad, ["Face6"])]
    assert plan.properties["MapMode"] == "FlatFace"


def test_a_missing_face_or_object_is_an_error(pd) -> None:
    body = Body("Body")
    pad = Obj("Pad", "PartDesign::Pad", Shape(faces={"Face6"}))
    doc = Doc(body, pad)
    with pytest.raises(ValueError, match=r"'Face60' does not exist on 'Pad'. Call list_subelements"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": ["Pad", "Face60"]}, None)
    with pytest.raises(ValueError, match="Referenced object 'Gone' not found"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": "Gone"}, None)
    with pytest.raises(ValueError, match="Invalid AttachmentSupport"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": []}, None)


def test_a_bare_plane_name_is_the_origin_plane_of_the_sketchs_body(pd) -> None:
    first, second = Body("Body"), Body("Body2", 1)
    doc = Doc(first, second)
    with pytest.raises(ValueError, match="'XY_Plane' is ambiguous"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": "XY_Plane"}, None)
    plan = pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": "XY_Plane"}, "Body2")
    assert plan.body is second
    assert [(o.Name, subs) for o, subs in plan.properties["AttachmentSupport"]] == [("XY_Plane001", [""])]
    alone = Doc(first)
    plan = pd.plan_create(alone, "Sketcher::SketchObject", {"AttachmentSupport": "XZ_Plane"}, None)
    assert plan.body is first and plan.properties["AttachmentSupport"][0][0].Name == "XZ_Plane"
    # A sketch with no support, or with no body asked for, stays outside any Body.
    assert pd.plan_create(alone, "Sketcher::SketchObject", {}, None).body is None


def test_a_plane_name_with_no_body_in_the_document_is_looked_up_as_an_object(pd) -> None:
    origin = Obj("XY_Plane", "App::Plane")
    doc = Doc(origin)
    plan = pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": "XY_Plane"}, None)
    assert plan.body is None and plan.properties["AttachmentSupport"] == [(origin, [""])]
    with pytest.raises(ValueError, match="Referenced object 'YZ_Plane' not found"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": "YZ_Plane"}, None)


def test_support_is_the_older_name_of_attachment_support_and_a_set_map_mode_is_kept(pd) -> None:
    doc = Doc(Body("Body"))
    plan = pd.plan_create(doc, "Sketcher::SketchObject", {"Support": "XY_Plane", "MapMode": "ObjectXY"}, None)
    assert "Support" not in plan.properties and plan.properties["MapMode"] == "ObjectXY"
    assert len(plan.notes) == 1 and "AttachmentSupport" in plan.notes[0]
    with pytest.raises(ValueError, match="not both"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"Support": "XY_Plane", "AttachmentSupport": "XY_Plane"}, None)


def test_an_update_resolves_the_support_in_the_sketchs_own_body_and_leaves_an_attached_map_mode(pd) -> None:
    first, second = Body("Body"), Body("Body2", 1)
    sketch = Sketch()
    second.Group.append(sketch)
    doc = Doc(first, second)
    doc.add(sketch)
    plan = pd.plan_update(doc, sketch, {"AttachmentSupport": "YZ_Plane", "Geometry": []})
    assert plan.properties["AttachmentSupport"][0][0].Name == "YZ_Plane001"
    assert plan.properties["MapMode"] == "FlatFace" and plan.geometry == []
    sketch.MapMode = "ObjectXY"
    assert "MapMode" not in pd.plan_update(doc, sketch, {"AttachmentSupport": "XY_Plane"}).properties


def test_geometry_is_replaced_and_a_rectangle_is_four_joined_lines(pd) -> None:
    sketch = Sketch()
    pd.set_geometry(
        sketch,
        [
            {"rectangle": {"corner": [0, 0], "size": [40, 20]}},
            {"circle": {"center": [20, 10], "radius": 5}},
            {"line": [[0, 0], [1, 1]]},
            {"arc": {"center": [0, 0], "radius": 5, "start_angle": 0, "end_angle": 90}},
        ],
    )
    assert sketch.calls[0] == "delete"
    lines = [g for g in sketch.geometry if g[0] == "line"]
    assert lines[:4] == [
        ("line", (0, 0), (40, 0)), ("line", (40, 0), (40, 20)), ("line", (40, 20), (0, 20)), ("line", (0, 20), (0, 0)),
    ]
    assert [c for c in sketch.calls if isinstance(c, tuple)] == [
        ("Coincident", 0, 2, 1, 1), ("Coincident", 1, 2, 2, 1), ("Coincident", 2, 2, 3, 1), ("Coincident", 3, 2, 0, 1),
    ]
    assert ("circle", (20, 10), 5) in sketch.geometry
    assert ("arc", (0, 0), 5, 0.0, 90.0) in sketch.geometry


def test_an_arc_runs_counter_clockwise_from_start_to_end(pd) -> None:
    sketch = Sketch()
    pd.set_geometry(sketch, [{"arc": {"center": [0, 0], "radius": 1, "start_angle": 270, "end_angle": 90}}])
    kind, _, _, start, end = sketch.geometry[0]
    assert (start, end) == (270.0, 450.0)


@pytest.mark.parametrize(
    "entries, message",
    [
        ("rect", "Geometry must be a list"),
        ([{"line": [[0, 0], [0, 0]]}], "two different points"),
        ([{"line": [[0, 0]]}], "A line is"),
        ([{"line": [[0, 0], [1, "a"]]}], "must be a number"),
        ([{"line": [[0, 0], [1, True]]}], "must be a number"),
        ([{"circle": {"center": [0, 0], "radius": 0}}], "above 0"),
        ([{"circle": {"center": [0, 0]}}], "A circle takes"),
        ([{"circle": {"center": [0, 0], "radius": 1, "start_angle": 1}}], "A circle takes"),
        ([{"arc": {"center": [0, 0], "radius": 1, "start_angle": 0, "end_angle": 360}}], "different start_angle"),
        ([{"rectangle": {"corner": [0, 0], "size": [0, 5]}}], "width and a height above 0"),
        ([{"rectangle": {"corner": [0, 0]}}], "A rectangle is"),
        ([{"polygon": []}], "Unknown geometry 'polygon'"),
        ([{"line": [[0, 0], [1, 1]], "circle": {}}], "Invalid geometry entry"),
        (["line"], "Invalid geometry entry"),
    ],
)
def test_bad_geometry_is_refused_before_the_sketch_is_touched(pd, entries, message) -> None:
    sketch = Sketch(geometry=3)
    with pytest.raises(ValueError, match=message):
        pd.set_geometry(sketch, entries)
    assert sketch.calls == []
    doc = Doc(Body("Body"))
    with pytest.raises(ValueError, match=message):
        pd.plan_create(doc, "Sketcher::SketchObject", {"Geometry": entries}, None)


def test_a_sketch_reports_its_geometry_and_whether_it_is_closed(pd) -> None:
    sketch = Sketch(geometry=4)
    assert pd.sketch_fields(sketch) == {"sketch": {"geometry_count": 4, "closed": True}}
    sketch.Shape = Shape(wires=[True, False])
    assert pd.sketch_fields(sketch)["sketch"]["closed"] is False
    assert pd.sketch_fields(Sketch()) == {"sketch": {"geometry_count": 0, "closed": False}}


def test_a_fillet_takes_base_and_edges_like_part_fillet(pd) -> None:
    body = Body("Body")
    pad = Obj("Pad", "PartDesign::Pad", Shape(edges=12))
    doc = Doc(body, pad)
    body.Group.append(pad)
    plan = pd.plan_create(doc, "PartDesign::Fillet", {"Base": "Pad", "Edges": ["Edge1", "Edge12"], "Radius": 1.5}, None)
    assert plan.body is body
    assert plan.properties == {"Base": ["Pad", ["Edge1", "Edge12"]], "Radius": 1.5}
    chamfer = pd.plan_create(doc, "PartDesign::Chamfer", {"Base": "Pad", "Edges": ["Edge2"], "Size": 1}, None)
    assert chamfer.properties["Base"] == ["Pad", ["Edge2"]]


@pytest.mark.parametrize(
    "edges, message",
    [
        ([{"edge": "Edge1", "radius": 2}], "takes one Radius for all its edges"),
        ([["Edge1", 2]], "takes one Radius for all its edges"),
        (["Edge13"], "Edge 'Edge13' does not exist on 'Pad', which has Edge1 to Edge12. Call list_subelements"),
        (["Face1"], "'Face1' is not an edge name"),
        ([], "Edges must be a list"),
        ("Edge1", "Edges must be a list"),
    ],
)
def test_bad_fillet_edges_are_refused(pd, edges, message) -> None:
    pad = Obj("Pad", "PartDesign::Pad", Shape(edges=12))
    doc = Doc(Body("Body"), pad)
    with pytest.raises(ValueError, match=message):
        pd.plan_create(doc, "PartDesign::Fillet", {"Base": "Pad", "Edges": edges, "Radius": 1}, None)


def test_an_update_takes_edges_against_the_base_it_has(pd) -> None:
    pad = Obj("Pad", "PartDesign::Pad", Shape(edges=12))
    fillet = Obj("Fillet", "PartDesign::Fillet")
    fillet.Base = (pad, ["Edge1"])
    doc = Doc(Body("Body"), pad)
    assert pd.plan_update(doc, fillet, {"Edges": ["Edge3", "Edge4"]}).properties == {"Base": ["Pad", ["Edge3", "Edge4"]]}
    assert pd.plan_update(doc, fillet, {"Radius": 2}).properties == {"Radius": 2}
    with pytest.raises(ValueError, match="does not exist on 'Pad'"):
        pd.plan_update(doc, fillet, {"Edges": ["Edge40"]})


def test_the_fillet_reply_lists_its_edges_with_the_size(pd) -> None:
    fillet = Obj("Fillet", "PartDesign::Fillet")
    fillet.Base = (Obj("Pad"), ["Edge1", "Edge4"])
    fillet.Radius = types.SimpleNamespace(Value=1.5)
    assert pd.dressup_edges_text(fillet, ["Base", "Edges", "Radius"]) == ["Edge1 r1.5", "Edge4 r1.5"]
    assert pd.dressup_edges_text(fillet, ["Label"]) == []
    chamfer = Obj("Chamfer", "PartDesign::Chamfer")
    chamfer.Base = (Obj("Pad"), ["Edge2"])
    chamfer.Size = types.SimpleNamespace(Value=0.5)
    assert pd.dressup_edges_text(chamfer, ["Size"]) == ["Edge2 s0.5"]
    assert pd.dressup_edges_text(Obj("Box", "Part::Box"), ["Edges"]) == []


def test_body_fields_name_the_body_and_its_tip(pd) -> None:
    body = Body("Body")
    assert pd.body_fields(body) == {"body": {"name": "Body", "tip": None}}
    body.Tip = Obj("Pad")
    assert pd.body_fields(body) == {"body": {"name": "Body", "tip": "Pad"}}


# create_object and update_object through the factory.

from test_object_validation import FakeDocument, FakeObject, load_object_factory  # noqa: E402


class BodyDocument(FakeDocument):
    """A document with one Body whose newObject makes ``created``, as FreeCAD's does."""

    def __init__(self, created):
        super().__init__(created)
        self.body = Body("Body")
        self.body.newObject = self.new_object
        self.created = created
        self.made_with = None
        self.Objects = [self.body, *self.body.features]
        self.add_object_calls = 0

    def new_object(self, type_id, name):
        self.made_with = (type_id, name)
        self.Objects.append(self.created)
        self.body.Group.append(self.created)
        self.body.Tip = self.created if type_id != "Sketcher::SketchObject" else self.body.Tip
        return self.created

    def addObject(self, *_args):
        self.add_object_calls += 1
        return super().addObject(*_args)

    def getObject(self, name):
        return next((o for o in self.Objects if o.Name == name), None)


class SketchObject(FakeObject):
    def __init__(self, geometry=0):
        super().__init__(Name="Sketch", TypeId="Sketcher::SketchObject", Shape=types.SimpleNamespace(
            Wires=[types.SimpleNamespace(isClosed=lambda: True)], isNull=lambda: geometry == 0))
        self.GeometryCount = geometry
        self.PropertiesList = []


def test_a_new_feature_is_made_in_its_body_and_the_reply_names_the_body_and_tip() -> None:
    pad = FakeObject(Name="Pad", TypeId="PartDesign::Pad")
    doc = BodyDocument(pad)
    with load_object_factory(doc) as factory:
        result = factory.create_object_gui("Doc", factory.Object(name="Pad", type="PartDesign::Pad", properties={}))
    assert doc.made_with == ("PartDesign::Pad", "Pad") and doc.add_object_calls == 0
    assert result["success"] is True
    assert result["body"] == {"name": "Body", "tip": "Pad"}


def test_an_empty_new_sketch_says_it_has_no_geometry_instead_of_a_null_shape() -> None:
    sketch = SketchObject(geometry=0)
    doc = BodyDocument(sketch)
    with load_object_factory(doc) as factory:
        result = factory.create_object_gui(
            "Doc", factory.Object(name="Sketch", type="Sketcher::SketchObject", properties={"Support": "XY_Plane"})
        )
    assert "shape" not in result
    assert result["sketch"] == {"geometry_count": 0, "closed": False}
    assert result["body"] == {"name": "Body", "tip": None}
    assert result["notes"] and "AttachmentSupport" in result["notes"][0]


def test_an_update_of_a_sketch_reports_its_geometry_and_stays_where_it_is() -> None:
    sketch = SketchObject(geometry=4)
    doc = BodyDocument(sketch)
    doc.Objects.append(sketch)
    with load_object_factory(doc) as factory:
        result = factory.edit_object_gui("Doc", factory.Object(name="Sketch", properties={}))
    assert result["sketch"] == {"geometry_count": 4, "closed": True}
    assert "body" not in result


def test_body_name_that_differs_from_the_body_a_link_points_into_is_refused(pd) -> None:
    first, second = Body("Body"), Body("Body2", 1)
    sketch = Obj("Sketch", "Sketcher::SketchObject")
    first.Group.append(sketch)
    doc = Doc(first, second, sketch)
    with pytest.raises(ValueError, match=r"body_name 'Body2' differs from the Body that Profile points into \('Body'\)"):
        pd.plan_create(doc, "PartDesign::Pad", {"Profile": "Sketch"}, "Body2")
    pad = Obj("Pad", "PartDesign::Pad", Shape(faces={"Face6"}))
    first.Group.append(pad)
    doc.add(pad)
    with pytest.raises(ValueError, match=r"differs from the Body that AttachmentSupport points into \('Body'\)"):
        pd.plan_create(doc, "Sketcher::SketchObject", {"AttachmentSupport": ["Pad", "Face6"]}, "Body2")
    assert pd.plan_create(doc, "PartDesign::Pad", {"Profile": "Sketch"}, "Body").body is first


def test_an_update_of_geometry_says_how_many_constraints_it_replaced(pd) -> None:
    sketch = SketchObject(geometry=4)
    sketch.ConstraintCount = 4
    sketch.deleteAllGeometry = lambda: None
    sketch.addGeometry = lambda *_a: 0
    sketch.addConstraint = lambda *_a: 0
    doc = BodyDocument(sketch)
    doc.Objects.append(sketch)
    with load_object_factory(doc) as factory:
        result = factory.edit_object_gui("Doc", factory.Object(name="Sketch", properties={"Geometry": []}))
    assert result["notes"] == ["Replaced the geometry and its 4 constraints."]


# update_object is all or nothing.

class Strict(FakeObject):
    """An object that refuses one property, as FreeCAD does for an unknown one."""

    def __setattr__(self, name, value):
        if name == "Bad":
            raise AttributeError("no attribute Bad")
        object.__setattr__(self, name, value)


def _update_with_a_bad_property(monkeypatch, opened):
    obj = Strict(Name="Beam", TypeId="Part::Box")
    obj.PropertiesList = ["Length", "Bad"]
    obj.Length = 60
    doc = FakeDocument(obj)
    doc.Objects = [obj]
    with load_object_factory(doc) as factory:
        freecad = sys.modules["FreeCAD"]
        closed = []
        freecad.closeActiveTransaction = lambda abort=False, id=0: closed.append(abort)
        if not opened:
            freecad.getActiveTransaction = lambda: ("Edit", 7)
        monkeypatch.setattr(factory, "generate_mesh", lambda *_: pytest.fail("remesh ran"))
        monkeypatch.setattr(factory, "hide_sources", lambda *_: pytest.fail("hide ran"))
        monkeypatch.setattr(factory, "collateral_report", lambda *_: pytest.fail("collateral ran"))
        result = factory.edit_object_gui("Doc", factory.Object(name="Beam", properties={"Length": 99, "Bad": 1}))
    return result, closed, doc


def test_a_failed_update_aborts_its_transaction_and_says_nothing_was_changed(monkeypatch) -> None:
    result, closed, doc = _update_with_a_bad_property(monkeypatch, opened=True)
    assert isinstance(result, str)
    assert result.endswith("Nothing was changed.") and "Bad" in result
    assert closed == [True]  # aborted, never committed
    assert doc.recompute_count == 1  # only the one after the abort: no reporting ran


def test_a_failed_update_inside_the_users_open_transaction_does_not_claim_a_rollback(monkeypatch) -> None:
    result, closed, _ = _update_with_a_bad_property(monkeypatch, opened=False)
    assert isinstance(result, str) and "Nothing was changed" not in result
    assert closed == []


# Reporting never fails the user's change.

def _boom(*_args, **_kwargs):
    raise RuntimeError("reader broke")


READERS = ["quantity_values", "load_info", "_name_and_placement_fields", "_shape_fields", "hide_sources", "mesh_info"]


@pytest.mark.parametrize("reader", READERS)
def test_a_failing_reader_drops_its_field_and_keeps_the_change(monkeypatch, reader) -> None:
    obj = FakeObject(Name="Beam", TypeId="Part::Box")
    obj.PropertiesList = ["Length"]
    obj.Length = 1
    doc = FakeDocument(obj)
    doc.Objects = [obj]
    with load_object_factory(doc) as factory:
        monkeypatch.setattr(factory, reader, _boom)
        monkeypatch.setattr(factory, "is_gmsh", lambda _o: True)
        created = factory.create_object_gui("Doc", factory.Object(name="Beam", type="Part::Box", properties={"Length": 5}))
        edited = factory.edit_object_gui("Doc", factory.Object(name="Beam", properties={"Length": 7}))
    assert created["success"] is True and edited["success"] is True
    assert obj.Length == 7


def test_a_failing_reader_of_partdesign_state_keeps_the_change(monkeypatch) -> None:
    pad = FakeObject(Name="Pad", TypeId="PartDesign::Pad")
    doc = BodyDocument(pad)
    with load_object_factory(doc) as factory:
        monkeypatch.setattr(factory.partdesign, "body_fields", _boom)
        result = factory.create_object_gui("Doc", factory.Object(name="Pad", type="PartDesign::Pad", properties={}))
    assert result["success"] is True and "body" not in result


def test_a_failed_mesh_generation_says_the_mesh_may_hold_a_partial_result(monkeypatch) -> None:
    mesh = FakeObject(Name="Mesh", TypeId="Fem::FemMeshGmsh")
    mesh.PropertiesList = ["CharacteristicLengthMax"]
    mesh.CharacteristicLengthMax = 3
    mesh.FemMesh = types.SimpleNamespace(NodeCount=100)
    doc = FakeDocument(mesh)
    doc.Objects = [mesh]
    with load_object_factory(doc) as factory:
        sys.modules["FreeCAD"].closeActiveTransaction = lambda abort=False, id=0: None
        monkeypatch.setattr(factory, "is_gmsh", lambda _o: True)
        monkeypatch.setattr(factory, "changes_meshing", lambda *_: True)
        monkeypatch.setattr(factory, "generate_mesh", lambda _o: (_ for _ in ()).throw(RuntimeError("gmsh failed")))
        result = factory.edit_object_gui("Doc", factory.Object(name="Mesh", properties={"CharacteristicLengthMax": 6}))
    assert result == "gmsh failed. Nothing was changed; the mesh itself may hold a partial result: run_fem_analysis remeshes it."
