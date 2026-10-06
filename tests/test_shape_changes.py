"""Which shapes a call changed: cheap detection, top-level objects only, nothing for a no-op."""

import importlib.util
import sys
import types
from pathlib import Path

import pytest

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"


class Shape:
    def __init__(self, token, null=False):
        self.token, self.null = token, null
        self.hash_reads = 0

    def isNull(self):
        return self.null

    def hashCode(self):
        self.hash_reads += 1
        return self.token


class Obj:
    def __init__(self, name, token=None, props=("Shape",)):
        self.Name = name
        self.PropertiesList = list(props) if token is not None else []
        if token is not None:
            self.Shape = Shape(token)


@pytest.fixture
def sc(monkeypatch):
    monkeypatch.syspath_prepend(str(ADDON))
    freecad = types.ModuleType("FreeCAD")
    freecad.Console = types.SimpleNamespace(PrintWarning=lambda _m: None, PrintError=lambda _m: None, PrintMessage=lambda _m: None)
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    summaries = []
    serialize = types.ModuleType("rpc_server.serialize")

    def object_shape_summary(obj):
        shape = obj.Shape
        summaries.append(shape.token)
        return {"solids": 1, "size": [1.0, 2.0, 3.0], "volume": float(getattr(obj, "vol", shape.token))}

    serialize.object_shape_summary = object_shape_summary
    monkeypatch.setitem(sys.modules, "rpc_server.serialize", serialize)
    tess = types.ModuleType("rpc_server.tessellation")
    tess.parents = {}
    tess.parent_map = lambda doc: tess.parents
    monkeypatch.setitem(sys.modules, "rpc_server.tessellation", tess)
    validation = types.ModuleType("rpc_server.object_validation")
    validation.MAX_LISTED_OBJECTS = 2
    monkeypatch.setitem(sys.modules, "rpc_server.object_validation", validation)
    spec = importlib.util.spec_from_file_location("_shape_changes_under_test", ADDON / "rpc_server" / "shape_changes.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.summaries, module.tess = summaries, tess
    return module


def doc_of(*objects):
    return types.SimpleNamespace(Objects=list(objects), getObject=lambda name: next((o for o in objects if o.Name == name), None))


def test_a_call_that_rebuilt_nothing_reports_nothing_and_computes_no_summary(sc) -> None:
    doc = doc_of(Obj("Body", 1), Obj("Pad", 2), Obj("Sheet"))
    sc.tess.parents = {"Pad": "Body"}
    before = sc.snapshot(doc)
    assert before == {"Body": 1, "Pad": 2, "Sheet": None}
    assert sc.changed_shapes(doc, before) == {}
    assert sc.summaries == []


def test_only_top_level_objects_whose_shape_changed_get_a_summary(sc) -> None:
    body, pad, other = Obj("Body", 1), Obj("Pad", 2), Obj("Other", 3)
    doc = doc_of(body, pad, other)
    sc.tess.parents = {"Pad": "Body"}
    before = sc.snapshot(doc)
    body.Shape, pad.Shape = Shape(10), Shape(20)  # the Body and the Pad inside it were rebuilt
    found = sc.changed_shapes(doc, before)
    assert found["changed_shapes"] == [{"name": "Body", "shape": {"solids": 1, "size": [1.0, 2.0, 3.0], "volume": 10.0}}]
    assert found["changed_shapes_count"] == 1 and found["changed_shapes_truncated"] is False
    assert sc.summaries == [10]  # the Pad and the untouched Other were never summarised


def test_a_new_object_counts_and_a_removed_one_does_not(sc) -> None:
    kept, gone = Obj("Kept", 1), Obj("Gone", 2)
    doc = doc_of(kept, gone)
    sc.tess.parents = {}
    before = sc.snapshot(doc)
    doc.Objects = [kept, Obj("New", 5)]
    assert [r["name"] for r in sc.changed_shapes(doc, before)["changed_shapes"]] == ["New"]


def test_the_list_is_capped_with_the_true_count(sc) -> None:
    objects = [Obj(f"B{i}", i) for i in range(5)]
    doc = doc_of(*objects)
    sc.tess.parents = {}
    before = sc.snapshot(doc)
    for o in objects:
        o.Shape = Shape(100 + int(o.Name[1:]))
    found = sc.changed_shapes(doc, before)
    assert len(found["changed_shapes"]) == 2 and found["changed_shapes_count"] == 5 and found["changed_shapes_truncated"] is True


def test_a_null_or_unreadable_shape_is_not_a_signature_and_a_failure_reports_nothing(sc) -> None:
    null = Obj("Null", 1)
    null.Shape = Shape(1, null=True)
    broken = Obj("Broken", 2)
    broken.Shape = types.SimpleNamespace(isNull=lambda: False, hashCode=lambda: (_ for _ in ()).throw(RuntimeError("x")))
    assert sc.snapshot(doc_of(null, broken)) == {"Null": None, "Broken": None}
    assert sc.snapshot(types.SimpleNamespace()) is None
    assert sc.changed_shapes(doc_of(null), None) == {}
    sc.tess.parents = {}
    sc.parent_map = lambda _d: (_ for _ in ()).throw(RuntimeError("tree"))
    doc = doc_of(Obj("A", 1))
    assert sc.changed_shapes(doc, {"A": 0}) == {}


def test_excluded_objects_are_left_out_of_the_report(sc) -> None:
    own, body = Obj("Pad", 1), Obj("Body", 2)
    doc = doc_of(own, body)
    sc.tess.parents = {"Pad": "Body"}
    before = sc.snapshot(doc)
    own.Shape, body.Shape = Shape(10), Shape(20)
    found = sc.changed_shapes(doc, before, {"Pad"})
    assert [r["name"] for r in found["changed_shapes"]] == ["Body"] and found["changed_shapes_count"] == 1
    assert sc.changed_shapes(doc, before, {"Pad", "Body"}) == {}


def test_a_member_of_a_shapeless_container_is_reported_and_its_body_stands_for_its_features(sc) -> None:
    part, body, pad, leaf = Obj("PartA"), Obj("Body", 1), Obj("Pad", 2), Obj("Leaf", 3)
    doc = doc_of(part, body, pad, leaf)
    sc.tess.parents = {"Body": "PartA", "Pad": "Body", "Leaf": "PartA"}
    before = sc.snapshot(doc)
    body.Shape, pad.Shape, leaf.Shape = Shape(10), Shape(20), Shape(30)
    found = sc.changed_shapes(doc, before)
    assert [r["name"] for r in found["changed_shapes"]] == ["Body", "Leaf"]
    assert found["changed_shapes_count"] == 2
    assert sc.summaries == [10, 30]


def test_an_input_that_changed_without_changing_its_boolean_reports_nothing(sc) -> None:
    cut, box = Obj("Cut", 1), Obj("Box", 2)
    doc = doc_of(cut, box)
    sc.tess.parents = {"Box": "Cut"}
    before = sc.snapshot(doc)
    box.Shape = Shape(20)
    assert sc.changed_shapes(doc, before) == {}


def test_a_hole_resized_inside_the_same_box_is_listed_and_a_rebuild_to_identical_geometry_is_not(sc) -> None:
    plate = Obj("Plate", 1)
    plate.vol = 100.0
    doc = doc_of(plate)
    sc.tess.parents = {}

    def rebuild(token, volume):
        before = sc.snapshot(doc)
        plate.Shape, plate.vol = Shape(token), volume
        return sc.changed_shapes(doc, before)

    # Nothing was summarised for it yet: listed, and remembered.
    assert [r["name"] for r in rebuild(2, 100.0)["changed_shapes"]] == ["Plate"]
    # The hole grew: same box, smaller volume.
    found = rebuild(3, 90.0)
    assert [r["name"] for r in found["changed_shapes"]] == ["Plate"] and found["changed_shapes"][0]["shape"]["volume"] == 90.0
    # Rebuilt to the same geometry (a touched object, an undone and redone step): nothing to say.
    assert rebuild(4, 90.0) == {}
    assert rebuild(5, 90.04) == {}  # equal as the Shape line shows it (0.1 mm^3)
    assert [r["name"] for r in rebuild(6, 90.2)["changed_shapes"]] == ["Plate"]


def test_a_summary_the_reply_of_a_create_or_update_showed_counts_as_the_stored_one(sc) -> None:
    plate = Obj("Plate", 1)
    plate.vol = 100.0
    doc = doc_of(plate)
    sc.tess.parents = {}
    sc.remember_summaries([(plate, {"solids": 1, "size": [1.0, 2.0, 3.0], "volume": 100.0})])
    before = sc.snapshot(doc)
    plate.Shape = Shape(9)  # rebuilt, same summary
    assert sc.changed_shapes(doc, before) == {}


def test_stored_summaries_of_closed_documents_and_deleted_objects_are_dropped(sc, monkeypatch) -> None:
    kept, gone = Obj("Kept", 1), Obj("Gone", 2)
    kept.Document = types.SimpleNamespace(Name="Open")
    gone.Document = types.SimpleNamespace(Name="Open")
    other = Obj("Other", 3)
    other.Document = types.SimpleNamespace(Name="Closed")
    summary = {"solids": 1, "size": [1.0, 1.0, 1.0], "volume": 1.0}
    sc._summaries.update({("Closed", "Other"): ("x",), ("Open", "Gone"): ("x",)})
    document = types.SimpleNamespace(getObject=lambda name: kept if name == "Kept" else None)
    freecad = types.ModuleType("FreeCAD")
    freecad.listDocuments = lambda: {"Open": document}
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    sc.remember_summaries([(kept, summary)])
    assert sorted(sc._summaries) == [("Open", "Kept")]


def test_summaries_stop_at_the_row_cap_and_the_store_is_pruned_once_for_the_call(sc, monkeypatch) -> None:
    objects = [Obj(f"B{i}", i) for i in range(5)]
    doc = doc_of(*objects)
    sc.tess.parents = {}
    pruned = []
    monkeypatch.setattr(sc, "prune_missing", lambda store: pruned.append(len(store)))
    before = sc.snapshot(doc)
    for o in objects:
        o.Shape = Shape(100 + int(o.Name[1:]))
    found = sc.changed_shapes(doc, before)
    assert len(found["changed_shapes"]) == 2 and found["changed_shapes_count"] == 5 and found["changed_shapes_truncated"] is True
    assert sc.summaries == [100, 101]  # the other three were never summarised
    assert len(pruned) == 1
    sc.remember_summaries([(objects[0], {"solids": 1, "size": [1.0, 1.0, 1.0], "volume": 1.0}), (objects[1], {"solids": 1, "size": [1.0, 1.0, 1.0], "volume": 1.0})])
    assert len(pruned) == 2  # once for the two of a create or update, not once each
