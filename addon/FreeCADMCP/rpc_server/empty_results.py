"""Results that look fine to FreeCAD but hold nothing useful.

A boolean of parts that do not overlap computes cleanly into an empty
compound, and a Cut whose tool misses the base computes into a copy of the
base. FreeCAD reports both as valid, so create_object, update_object and
recompute_document ask this module instead.
"""

from typing import Any

#: Types that make a solid from solid inputs, with the properties that hold the
#: inputs: a result with no solid then means the inputs miss each other or one
#: removes the other. Any other type with a solid in its OutList (a sketch
#: attached to a face, an extrusion with Solid false) holds no solid by design.
SOLID_INPUTS = {
    "Part::Cut": ("Base", "Tool"),
    "Part::Common": ("Base", "Tool"),
    "Part::Fuse": ("Base", "Tool"),
    "Part::MultiFuse": ("Shapes",),
    "Part::MultiCommon": ("Shapes",),
    "Part::Fillet": ("Base",),
    "Part::Chamfer": ("Base",),
    "Part::Thickness": ("Faces",),
    "Part::Offset": ("Source",),
    "Part::Mirroring": ("Source",),
}

_NO_SOLID_WHY = {
    "Part::Common": "its inputs do not overlap",
    "Part::MultiCommon": "its inputs do not overlap",
    "Part::Cut": "the tool removes all of the base",
}

#: The volume rule of each boolean (see _size_problem).
_SIZE_RULES = {
    "Part::Fuse": "fuse",
    "Part::MultiFuse": "fuse",
    "Part::Common": "common",
    "Part::MultiCommon": "common",
    "Part::Cut": "cut",
}

#: How far past its bound a boolean's volume may be before it is reported.
#: Ordinary booleans (box, cylinder, sphere, cone and torus pairs, refine on and
#: off, nested, identical and touching, and swept, lofted, filleted and threaded
#: solids whose booleans were right) never passed their bound: 1e-3 is margin.
_SIZE_TOLERANCE = 1e-3

_GENERIC_WHY = "its inputs do not overlap or one removes all of the other"
_NO_OP_WHY = "the tool does not reach the base"


def _linked_objects(value: Any) -> list:
    """The objects a link value holds: one object, a list of them, or
    ``(object, sub-elements)`` pairs."""
    if isinstance(value, (tuple, list)):
        if len(value) == 2 and isinstance(value[1], (tuple, list, str)) and hasattr(value[0], "Name"):
            return [value[0]]
        return [item for entry in value for item in _linked_objects(entry)]
    return [value] if hasattr(value, "Name") else []


def _solid_count(obj: Any) -> int:
    try:
        return len(obj.Shape.Solids)
    except Exception:
        return 0


def _fingerprint(obj: Any) -> tuple | None:
    """What identifies ``obj``'s shape for the verdict cache: its hash (an O(1)
    read, but an address, so it can repeat after a rebuild) with the face and
    edge counts, the loose bounding box and the Placement as cheap
    discriminators. None when it has no hash."""
    try:
        shape = obj.Shape
        key: list = [int(shape.hashCode())]
    except Exception:
        return None

    def part(read: Any) -> Any:
        try:
            return read()
        except Exception:
            return None

    def box() -> tuple:
        b = shape.BoundBox
        return tuple(round(float(v), 6) for v in (b.XMin, b.YMin, b.ZMin, b.XMax, b.YMax, b.ZMax))

    def placement() -> tuple:
        p = obj.Placement
        return tuple(round(float(v), 6) for v in (*p.Base, *p.Rotation.Q))

    # countElement counts without building the list of faces or edges (FreeCAD 1.0 and later); older ones fall back to len.
    count = getattr(shape, "countElement", None)

    def counted(kind: str, attribute: str) -> Any:
        if count is not None:
            try:
                return count(kind)
            except Exception:
                pass
        return part(lambda: len(getattr(shape, attribute)))

    key += [counted("Face", "Faces"), counted("Edge", "Edges"), part(box), part(placement)]
    return tuple(key)


def prune_missing(store: dict) -> None:
    """Drop the entries of ``store`` (keyed by (document, object) name) of documents that are closed and of objects that are
    gone from an open one."""
    try:
        import FreeCAD

        documents = FreeCAD.listDocuments()
        for key in list(store):
            document = documents.get(key[0])
            if document is None or document.getObject(key[1]) is None:
                del store[key]
    except Exception:
        pass

class _Volumes:
    """The volumes one check reads, each integrated at most once: the base's
    volume the no-op test of a Cut needs is the one the size test needs, and
    ``known`` is the result's volume when the caller has it already."""

    def __init__(self, known: dict[str, float | None]) -> None:
        self._volumes = dict(known)

    def of(self, obj: Any) -> float | None:
        name = str(obj.Name)
        if name not in self._volumes:
            try:
                self._volumes[name] = float(obj.Shape.Volume)
            except Exception:
                self._volumes[name] = None
        return self._volumes[name]


def _same_volume(a: float, b: float) -> bool:
    return abs(a - b) <= max(1e-9 * abs(b), 1e-6)


#: The verdict of the volume checks by (document, object): the fingerprints (hash,
#: face and edge counts, loose box, Placement) of the result's and the inputs' shapes
#: it was made for, and the verdict. A boolean whose shapes did not change is not
#: integrated again by the next recompute (about 50 ms for each threaded input).
#: Entries of closed documents and deleted objects are dropped when a new one is stored.
_verdicts: dict[tuple[str, str], tuple[tuple, tuple[str, str] | None]] = {}


def empty_result(obj: Any, volume: float | None = None) -> tuple[str, str] | None:
    """``(warning, short)`` when ``obj`` is a boolean or solid-making feature
    whose result holds no solid although an input does, a Cut that removed
    nothing, or a boolean whose volume its inputs rule out; None otherwise.
    ``warning`` is the sentence for the reply of a create or update call,
    ``short`` the reason a recompute lists. ``volume`` is the result's volume
    when the caller has it already (shape_summary computed it), so it is not
    integrated a second time."""
    try:
        props = SOLID_INPUTS.get(obj.TypeId)
        if props is None:
            return None
        inputs = [item for prop in props for item in _linked_objects(getattr(obj, prop, None))]
        if not any(_solid_count(item) for item in inputs):
            return None
        if not _solid_count(obj):
            why = _NO_SOLID_WHY.get(obj.TypeId, _GENERIC_WHY)
            return f"The result holds no solid: {why}. Check their Placement.", f"no solid: {why}"
        if obj.TypeId not in _SIZE_RULES:
            return None
        hashes = tuple(_fingerprint(item) for item in [obj, *inputs])
        key = (str(getattr(getattr(obj, "Document", None), "Name", "")), str(obj.Name))
        cached = _verdicts.get(key)
        if cached is not None and cached[0] == hashes and None not in hashes:
            return cached[1]
        verdict = _volume_verdict(obj, inputs, _Volumes({str(obj.Name): volume} if volume is not None else {}))
        if None not in hashes:
            prune_missing(_verdicts)
            _verdicts[key] = (hashes, verdict)
        return verdict
    except Exception:
        pass
    return None


def _volume_verdict(obj: Any, inputs: list, volumes: _Volumes) -> tuple[str, str] | None:
    """The no-op test of a Cut, then the size test of every boolean."""
    if obj.TypeId == "Part::Cut":
        base = _linked_objects(getattr(obj, "Base", None))
        tool = _linked_objects(getattr(obj, "Tool", None))
        if base and tool and _solid_count(tool[0]):
            base_volume = volumes.of(base[0])
            result = volumes.of(obj)
            if base_volume and result is not None and _same_volume(result, base_volume):
                return f"{_NO_OP_WHY.capitalize()}: nothing was removed. Check their Placement.", _NO_OP_WHY
    return _size_problem(obj, inputs, volumes)


def _size_problem(obj: Any, inputs: list, volumes: _Volumes) -> tuple[str, str] | None:
    """``(warning, short)`` when a boolean's volume cannot be right for its solid
    inputs: a fuse is never smaller than its largest input, a common never larger
    than its smallest, a cut never larger than its base. The difference is a
    part of an input the boolean silently dropped (a thread whose fuse with its
    core went wrong), which FreeCAD still reports as valid. An input that is a
    compound of several solids is not checked: its Volume is the sum of its
    solids', which overstates their union when they overlap."""
    kind = _SIZE_RULES.get(obj.TypeId)
    if kind is None:
        return None
    solids = [item for item in inputs if _solid_count(item)]
    if kind == "cut":
        solids = solids[:1] if inputs and _solid_count(inputs[0]) else []
    elif len(solids) != len(inputs):
        return None
    if any(_solid_count(item) > 1 for item in solids):
        return None
    sizes = [volumes.of(item) for item in solids]
    if not sizes or any(v is None or v <= 0 for v in sizes):
        return None
    volume = volumes.of(obj)
    if volume is None:
        return None
    if kind == "fuse" and volume < max(sizes) * (1 - _SIZE_TOLERANCE):
        return (
            "The result is smaller than its largest input: part of an input was dropped. "
            "Check the inputs touch properly, or fuse them one at a time.",
            "smaller than its largest input: part of an input was dropped",
        )
    if kind == "common" and volume > min(sizes) * (1 + _SIZE_TOLERANCE):
        return (
            "The result is larger than its smallest input: the boolean went wrong. "
            "Check the inputs, or intersect them one at a time.",
            "larger than its smallest input",
        )
    if kind == "cut" and volume > sizes[0] * (1 + _SIZE_TOLERANCE):
        return (
            "The result is larger than its Base: the cut went wrong. "
            "Check the inputs, or cut with one tool at a time.",
            "larger than its base",
        )
    return None
