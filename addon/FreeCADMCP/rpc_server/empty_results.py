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


def _volume(obj: Any) -> float | None:
    try:
        return float(obj.Shape.Volume)
    except Exception:
        return None


def _same_volume(a: float, b: float) -> bool:
    return abs(a - b) <= max(1e-9 * abs(b), 1e-6)


def empty_result(obj: Any, volume: float | None = None) -> tuple[str, str] | None:
    """``(warning, short)`` when ``obj`` is a boolean or solid-making feature
    whose result holds no solid although an input does, or a Cut that removed
    nothing; None otherwise. ``warning`` is the sentence for the reply of a
    create or update call, ``short`` the reason a recompute lists. ``volume``
    is the result's volume when the caller has it already (shape_summary
    computed it), so it is not integrated a second time."""
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
        if obj.TypeId == "Part::Cut":
            base = _linked_objects(getattr(obj, "Base", None))
            tool = _linked_objects(getattr(obj, "Tool", None))
            if base and tool and _solid_count(tool[0]):
                base_volume = _volume(base[0])
                if volume is None:
                    volume = _volume(obj)
                if base_volume and volume is not None and _same_volume(volume, base_volume):
                    return f"{_NO_OP_WHY.capitalize()}: nothing was removed. Check their Placement.", _NO_OP_WHY
    except Exception:
        pass
    return None
