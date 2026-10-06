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


def boolean(typ: str, result_volume: float, volumes: list[float], solids: list[int] | None = None, **extra):
    inputs = [part(f"In{i}", (solids[i] if solids else 1), v) for i, v in enumerate(volumes)]
    links = {"Shapes": inputs} if typ in ("Part::MultiFuse", "Part::MultiCommon") else {"Base": inputs[0], "Tool": inputs[1]}
    return part("Result", 1, result_volume, TypeId=typ, **links, **extra)


def test_a_fuse_smaller_than_its_largest_input_lost_part_of_it() -> None:
    for typ in ("Part::Fuse", "Part::MultiFuse"):
        warning, short = empty_results.empty_result(boolean(typ, 119.5, [472.9, 119.5]))
        assert warning == (
            "The result is smaller than its largest input: part of an input was dropped. "
            "Check the inputs touch properly, or fuse them one at a time."
        )
        assert short == "smaller than its largest input: part of an input was dropped"


def test_ordinary_fuses_are_not_reported() -> None:
    assert empty_results.empty_result(boolean("Part::Fuse", 6125.0, [6000.0, 125.0])) is None
    assert empty_results.empty_result(boolean("Part::MultiFuse", 6000.0, [6000.0, 125.0, 6000.0])) is None  # one inside the other
    assert empty_results.empty_result(boolean("Part::Fuse", 6000.0 * (1 - 1e-6), [6000.0, 6000.0])) is None  # noise
    assert empty_results.empty_result(boolean("Part::Fuse", 5000.0, [6000.0, 125.0], solids=[1, 0])) is None  # a face input


def test_a_common_larger_than_its_smallest_input_and_a_cut_larger_than_its_base_are_reported() -> None:
    assert empty_results.empty_result(boolean("Part::Common", 400.0, [1000.0, 300.0]))[1] == "larger than its smallest input"
    assert empty_results.empty_result(boolean("Part::MultiCommon", 400.0, [1000.0, 300.0, 900.0]))[1] == "larger than its smallest input"
    warning, short = empty_results.empty_result(boolean("Part::Cut", 1200.0, [1000.0, 50.0]))
    assert warning == "The result is larger than its Base: the cut went wrong. Check the inputs, or cut with one tool at a time."
    assert short == "larger than its base"


def test_ordinary_commons_and_cuts_are_not_reported() -> None:
    assert empty_results.empty_result(boolean("Part::Common", 125.0, [6000.0, 125.0])) is None
    assert empty_results.empty_result(boolean("Part::Common", 300.0, [1000.0, 300.0 * (1 + 1e-6)])) is None
    assert empty_results.empty_result(boolean("Part::Cut", 5875.0, [6000.0, 125.0])) is None


class CountingShape(Shape):
    """A shape that counts its volume integrals and has a hash a test can change."""

    def __init__(self, solids: int, volume: float, token: int):
        super().__init__(solids, volume)
        self.token, self.integrals = token, 0

    @property
    def Volume(self) -> float:  # noqa: N802
        self.integrals += 1
        return self._volume

    @Volume.setter
    def Volume(self, value: float) -> None:  # noqa: N802
        self._volume = value

    def hashCode(self) -> int:  # noqa: N802
        return self.token


def counted(name: str, solids: int, volume: float, token: int, **extra) -> types.SimpleNamespace:
    return types.SimpleNamespace(Name=name, TypeId=extra.pop("TypeId", "Part::Box"), Shape=CountingShape(solids, volume, token),
                                 Document=types.SimpleNamespace(Name="Doc"), **extra)


def test_an_unchanged_boolean_is_not_integrated_again_and_a_changed_one_is() -> None:
    base, tool = counted("CB", 1, 6000.0, 1), counted("CT", 1, 125.0, 2)
    fuse = counted("CachedFuse", 1, 6125.0, 3, TypeId="Part::Fuse", Base=base, Tool=tool)
    assert empty_results.empty_result(fuse) is None
    first = base.Shape.integrals + tool.Shape.integrals + fuse.Shape.integrals
    assert first == 3
    for _ in range(3):
        assert empty_results.empty_result(fuse) is None
    assert base.Shape.integrals + tool.Shape.integrals + fuse.Shape.integrals == first
    # The result lost its core: new hash, so the verdict is worked out again and warns.
    fuse.Shape.token, fuse.Shape.Volume = 4, 125.0
    assert empty_results.empty_result(fuse)[1].startswith("smaller than its largest input")
    assert empty_results.empty_result(fuse)[1].startswith("smaller than its largest input")
    assert fuse.Shape.integrals == 2  # once for each verdict, not for each call
    # An input changed, the result did not: worked out again too.
    base.Shape.token, base.Shape.Volume = 5, 100.0
    assert empty_results.empty_result(fuse) is None


def test_a_cut_integrates_its_base_and_result_once_for_both_tests() -> None:
    base, tool = counted("KB", 1, 1000.0, 11), counted("KT", 1, 10.0, 12)
    result = counted("OnceCut", 1, 990.0, 13, TypeId="Part::Cut", Base=base, Tool=tool)
    assert empty_results.empty_result(result) is None
    assert base.Shape.integrals == 1 and result.Shape.integrals == 1 and tool.Shape.integrals == 0


def test_a_compound_input_is_not_size_checked() -> None:
    # Two overlapping solids of 1000 each: the compound reports 2000 while their union is smaller than that.
    compound = part("Pair", 2, 2000.0)
    assert empty_results.empty_result(part("Fuse", 1, 1500.0, TypeId="Part::Fuse", Base=compound, Tool=part("Small", 1, 10.0))) is None
    assert empty_results.empty_result(part("Fuse", 1, 1500.0, TypeId="Part::MultiFuse", Shapes=[part("A"), compound])) is None
    # The same fuse of single solids is reported.
    assert empty_results.empty_result(part("Fuse", 1, 1500.0, TypeId="Part::Fuse", Base=part("Whole", 1, 2000.0), Tool=part("Small", 1, 10.0))) is not None


def test_a_repeated_hash_with_other_geometry_does_not_hit_the_cache() -> None:
    # The hash is an address and can come back after a rebuild: the counts, the box and the Placement tell the shapes apart.
    class Box:
        def __init__(self, x_max: float) -> None:
            self.XMin = self.YMin = self.ZMin = 0.0
            self.XMax, self.YMax, self.ZMax = x_max, 1.0, 1.0

    class Located(CountingShape):
        faces, edges, x_max = 6, 12, 1.0

        @property
        def Faces(self):  # noqa: N802
            return [0] * self.faces

        @property
        def Edges(self):  # noqa: N802
            return [0] * self.edges

        @property
        def BoundBox(self):  # noqa: N802
            return Box(self.x_max)

    def located(name: str, solids: int, volume: float, **extra):
        shape = Located(solids, volume, 7)
        return types.SimpleNamespace(Name=name, TypeId=extra.pop("TypeId", "Part::Box"), Shape=shape,
                                     Document=types.SimpleNamespace(Name="Doc"), Placement=None, **extra)

    base, tool = located("LB", 1, 6000.0), located("LT", 1, 125.0)
    fuse = located("RepeatFuse", 1, 6125.0, TypeId="Part::Fuse", Base=base, Tool=tool)
    assert empty_results.empty_result(fuse) is None
    fuse.Shape.Volume = 125.0  # lost its core, the hash is the same address again
    assert empty_results.empty_result(fuse) is None  # same fingerprint: the cached verdict (the hole the discriminators close)
    fuse.Shape.faces = 4
    assert empty_results.empty_result(fuse)[1].startswith("smaller than its largest input")
    fuse.Shape.Volume, fuse.Shape.faces = 6125.0, 6
    assert empty_results.empty_result(fuse) is None
    fuse.Shape.Volume, fuse.Shape.x_max = 125.0, 2.0  # only the box differs
    assert empty_results.empty_result(fuse) is not None


def test_verdicts_of_closed_documents_and_deleted_objects_are_dropped(monkeypatch) -> None:
    empty_results._verdicts.clear()
    empty_results._verdicts.update({("Gone", "A"): ((1,), None), ("Open", "Deleted"): ((1,), None), ("Open", "Kept"): ((1,), None)})
    document = types.SimpleNamespace(getObject=lambda name: object() if name == "Kept" else None)
    freecad = types.ModuleType("FreeCAD")
    freecad.listDocuments = lambda: {"Open": document}
    monkeypatch.setitem(sys.modules, "FreeCAD", freecad)
    empty_results.prune_missing(empty_results._verdicts)
    assert list(empty_results._verdicts) == [("Open", "Kept")]
    empty_results._verdicts.clear()


def test_the_fingerprint_counts_with_countelement_and_falls_back_to_len_when_it_raises() -> None:
    class Plain(CountingShape):
        Faces, Edges = [0] * 6, [0] * 12

    class Counting(Plain):
        def countElement(self, kind: str) -> int:  # noqa: N802
            return {"Face": 6, "Edge": 12}[kind]

    class Broken(Plain):
        def countElement(self, kind: str) -> int:  # noqa: N802
            raise RuntimeError("no such element type")

    def fingerprint(shape_class):
        obj = types.SimpleNamespace(Name="X", Shape=shape_class(1, 1.0, 9), Placement=None)
        return empty_results._fingerprint(obj)

    assert fingerprint(Counting)[:3] == (9, 6, 12)
    assert fingerprint(Plain)[:3] == (9, 6, 12)
    assert fingerprint(Broken)[:3] == (9, 6, 12)
