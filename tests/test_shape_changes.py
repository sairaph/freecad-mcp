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

    def shape_summary(shape):
        summaries.append(shape.token)
        return {"solids": 1, "size": [1.0, 2.0, 3.0], "volume": float(shape.token)}

    serialize.shape_summary = shape_summary
    monkeypatch.setitem(sys.modules, "rpc_server.serialize", serialize)
    tess = types.ModuleType("rpc_server.tessellation")
    tess.roots = None
    tess.tree_root_objects = lambda doc: [o for o in doc.Objects if o.Name in tess.roots]
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
    return types.SimpleNamespace(Objects=list(objects))


def test_a_call_that_rebuilt_nothing_reports_nothing_and_computes_no_summary(sc) -> None:
    doc = doc_of(Obj("Body", 1), Obj("Pad", 2), Obj("Sheet"))
    sc.tess.roots = {"Body", "Sheet"}
    before = sc.snapshot(doc)
    assert before == {"Body": 1, "Pad": 2, "Sheet": None}
    assert sc.changed_shapes(doc, before) == {}
    assert sc.summaries == []


def test_only_top_level_objects_whose_shape_changed_get_a_summary(sc) -> None:
    body, pad, other = Obj("Body", 1), Obj("Pad", 2), Obj("Other", 3)
    doc = doc_of(body, pad, other)
    sc.tess.roots = {"Body", "Other"}
    before = sc.snapshot(doc)
    body.Shape, pad.Shape = Shape(10), Shape(20)  # the Body and the Pad inside it were rebuilt
    found = sc.changed_shapes(doc, before)
    assert found["changed_shapes"] == [{"name": "Body", "shape": {"solids": 1, "size": [1.0, 2.0, 3.0], "volume": 10.0}}]
    assert found["changed_shapes_count"] == 1 and found["changed_shapes_truncated"] is False
    assert sc.summaries == [10]  # the Pad and the untouched Other were never summarised


def test_a_new_object_counts_and_a_removed_one_does_not(sc) -> None:
    kept, gone = Obj("Kept", 1), Obj("Gone", 2)
    doc = doc_of(kept, gone)
    sc.tess.roots = {"Kept", "Gone", "New"}
    before = sc.snapshot(doc)
    doc.Objects = [kept, Obj("New", 5)]
    assert [r["name"] for r in sc.changed_shapes(doc, before)["changed_shapes"]] == ["New"]


def test_the_list_is_capped_with_the_true_count(sc) -> None:
    objects = [Obj(f"B{i}", i) for i in range(5)]
    doc = doc_of(*objects)
    sc.tess.roots = {o.Name for o in objects}
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
    sc.tess.roots = {"A"}
    sc.tree_root_objects = lambda _d: (_ for _ in ()).throw(RuntimeError("tree"))
    doc = doc_of(Obj("A", 1))
    assert sc.changed_shapes(doc, {"A": 0}) == {}


def test_excluded_objects_are_left_out_of_the_report(sc) -> None:
    own, body = Obj("Pad", 1), Obj("Body", 2)
    doc = doc_of(own, body)
    sc.tess.roots = {"Pad", "Body"}
    before = sc.snapshot(doc)
    own.Shape, body.Shape = Shape(10), Shape(20)
    found = sc.changed_shapes(doc, before, {"Pad"})
    assert [r["name"] for r in found["changed_shapes"]] == ["Body"] and found["changed_shapes_count"] == 1
    assert sc.changed_shapes(doc, before, {"Pad", "Body"}) == {}
