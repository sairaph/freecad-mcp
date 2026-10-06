"""Empty and no-op boolean results, found for create/update replies, recompute_document and the object list."""

import sys
import types
from pathlib import Path

ADDON = Path(__file__).resolve().parents[1] / "addon" / "FreeCADMCP"
sys.path.insert(0, str(ADDON))

import importlib.util

import pytest

from rpc_server import empty_results, tessellation  # noqa: E402


class Shape:
    def __init__(self, solids: int, volume: float = 0.0):
        self.Solids = [object()] * solids
        self.Volume = volume


def part(name: str, solids: int = 1, volume: float = 1000.0, **links) -> types.SimpleNamespace:
    return types.SimpleNamespace(Name=name, TypeId=links.pop("TypeId", "Part::Box"), Shape=Shape(solids, volume), **links)


def cut(result_volume: float, base_volume: float = 1000.0, tool_solids: int = 1, result_solids: int = 1):
    base, tool = part("Base", volume=base_volume), part("Tool", solids=tool_solids)
    return part("NoCut", result_solids, result_volume, TypeId="Part::Cut", Base=base, Tool=tool)


def test_a_cut_whose_tool_misses_the_base_is_a_no_op() -> None:
    warning, short = empty_results.empty_result(cut(1000.0))
    assert warning == "The tool does not reach the base: nothing was removed. Check their Placement."
    assert short == "the tool does not reach the base"


def test_a_cut_volume_difference_inside_the_tolerance_is_a_no_op_and_outside_is_not() -> None:
    assert empty_results.empty_result(cut(1000.0 - 5e-7)) is not None
    assert empty_results.empty_result(cut(1e9 - 0.5, base_volume=1e9)) is not None
    assert empty_results.empty_result(cut(999.0)) is None
    assert empty_results.empty_result(cut(1e9 - 2.0, base_volume=1e9)) is None


def test_a_cut_by_a_tool_with_no_solid_is_not_reported_as_a_no_op() -> None:
    assert empty_results.empty_result(cut(1000.0, tool_solids=0)) is None


def test_a_cut_that_removes_the_base_and_an_empty_common_keep_the_no_solid_warning() -> None:
    warning, short = empty_results.empty_result(cut(0.0, result_solids=0))
    assert warning == "The result holds no solid: the tool removes all of the base. Check their Placement."
    assert short == "no solid: the tool removes all of the base"
    common = part("EmptyCommon", 0, TypeId="Part::Common", Base=part("A"), Tool=part("B"))
    assert empty_results.empty_result(common)[1] == "no solid: its inputs do not overlap"


def test_other_types_are_left_alone() -> None:
    sketch = part("Sketch", 0, TypeId="Sketcher::SketchObject", OutList=[part("Box")])
    assert empty_results.empty_result(sketch) is None
    fuse = part("Fuse", 1, 2000.0, TypeId="Part::Fuse", Base=part("A"), Tool=part("B"))
    assert empty_results.empty_result(fuse) is None


@pytest.fixture
def recompute(monkeypatch):
    # gui_task and lookup need FreeCADGui; recompute only calls them through these two names.
    gui_task = types.ModuleType("rpc_server.gui_task")
    gui_task.resolve_timeout = lambda value, default: default
    gui_task.run_on_gui = lambda task, *args, **kwargs: task()
    lookup = types.ModuleType("rpc_server.lookup")
    lookup.require_document = lambda name: (None, None)
    monkeypatch.setitem(sys.modules, "rpc_server.gui_task", gui_task)
    monkeypatch.setitem(sys.modules, "rpc_server.lookup", lookup)
    spec = importlib.util.spec_from_file_location("_recompute_under_test", ADDON / "rpc_server" / "recompute.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_recompute_lists_empty_results_after_a_clean_recompute(recompute, monkeypatch) -> None:
    broken = part("Broken", 0, TypeId="Part::Cut", Base=part("A"), Tool=part("B"))
    broken.State = ["Invalid"]
    common = part("EmptyCommon", 0, TypeId="Part::Common", Base=part("A"), Tool=part("B"))
    objects = [cut(1000.0), common, part("Fine")]
    doc = types.SimpleNamespace(Objects=objects, recompute=lambda: 3)
    monkeypatch.setattr(recompute, "require_document", lambda name: (doc, None))

    reply = recompute.recompute_document("D")

    assert reply["empty_count"] == 2 and reply["empty_truncated"] is False
    assert reply["empty_results"] == [
        {"name": "NoCut", "reason": "the tool does not reach the base"},
        {"name": "EmptyCommon", "reason": "no solid: its inputs do not overlap"},
    ]
    assert reply["invalid_count"] == 0


def test_a_shared_tool_has_every_parent_in_object_order() -> None:
    far = types.SimpleNamespace(Name="Far", Document=types.SimpleNamespace(Name="D"))
    def claims(*children):
        return types.SimpleNamespace(claimChildren=lambda: list(children))
    no_cut = types.SimpleNamespace(Name="NoCut", ViewObject=claims(far), getParentGeoFeatureGroup=lambda: None)
    fz = types.SimpleNamespace(Name="Fz", ViewObject=claims(far), getParentGeoFeatureGroup=lambda: None)
    far.ViewObject = None
    far.getParentGeoFeatureGroup = lambda: None
    doc = types.SimpleNamespace(Name="D", Objects=[far, no_cut, fz])

    assert tessellation.parent_lists(doc) == {"Far": ["NoCut", "Fz"]}
    assert tessellation.parent_map(doc) == {"Far": "NoCut"}



def test_a_volume_the_caller_already_has_is_not_computed_again() -> None:
    result = cut(1000.0)

    class Counting(Shape):
        reads = 0

        @property
        def Volume(self):
            Counting.reads += 1
            return 1000.0

        @Volume.setter
        def Volume(self, value):
            pass

    result.Shape = Counting(1)
    assert empty_results.empty_result(result, 1000.0) is not None
    assert Counting.reads == 0
    assert empty_results.empty_result(result) is not None
    assert Counting.reads == 1


def test_recompute_does_not_count_a_failed_object_as_merely_touched(recompute, monkeypatch) -> None:
    def obj(name, states, invalid=False, deps=()):
        o = types.SimpleNamespace(Name=name, Label=name, TypeId="Part::Feature", State=list(states), OutList=list(deps), InList=[],
                                  Shape=Shape(1), isValid=lambda: not invalid, getStatusString=lambda: "BRep_API: command not done" if invalid else "Valid")
        for dep in deps:
            dep.InList.append(o)
        return o

    fillet = obj("OuterFillet", ("Touched", "Invalid"), invalid=True)
    shell = obj("Shell", ("Expanded", "Up-to-date"), deps=[fillet])
    objects = [fillet, shell]
    doc = types.SimpleNamespace(Objects=objects, recompute=lambda: 1)
    monkeypatch.setattr(recompute, "require_document", lambda name: (doc, None))

    reply = recompute.recompute_document("D")

    assert reply["invalid_count"] == 1 and reply["touched_count"] == 0 and reply["touched_objects"] == []
    assert reply["stale_objects"] == [{"name": "Shell", "depends_on": "OuterFillet"}] and reply["stale_count"] == 1

    # A Touched object that the invalid list does not hold (cut off by its cap) is still counted.
    monkeypatch.setattr(recompute, "invalid_objects_report", lambda objs: {"invalid_objects": [], "invalid_count": 1, "invalid_truncated": True,
                                                                          "stale_objects": [], "stale_count": 0, "stale_truncated": False})
    reply = recompute.recompute_document("D")
    assert reply["touched_objects"] == ["OuterFillet"] and reply["touched_count"] == 1
