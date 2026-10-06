"""Hide the source of a feature the way FreeCAD's own command does.

FreeCAD's dialogs and commands for Fillet, Chamfer, Extrusion, Revolution and
Thickness hide the object they build on, so the result is not covered by it
(DlgFilletEdges, DlgExtrusion, DlgRevolution, CmdPartThickness). Booleans and
compounds hide their inputs in their view provider already. Loft and Sweep
profiles are hidden here too: left shown they stay behind as stray edges
inside the solid. Mirroring, Offset and Ruled Surface leave their sources
shown, so they are not listed.
"""

from typing import Any

#: Feature type -> the properties that link its sources.
SOURCE_PROPERTIES = {
    "Part::Fillet": ("Base",),
    "Part::Chamfer": ("Base",),
    "Part::Extrusion": ("Base",),
    "Part::Revolution": ("Source",),
    "Part::Thickness": ("Faces",),
    "Part::Loft": ("Sections",),
    "Part::Sweep": ("Sections", "Spine"),
}


def _source_objects(value: Any) -> list[Any]:
    """The objects a link value points at: a bare object, an
    ``(object, sub-elements)`` pair, or a list of either."""
    if hasattr(value, "Name"):
        return [value]
    if not isinstance(value, (tuple, list)):
        return []
    if len(value) == 2 and hasattr(value[0], "Name") and isinstance(value[1], (tuple, list, str)):
        return [value[0]]
    return [source for item in value for source in _source_objects(item)]


def hide_sources(obj: Any, changed: Any) -> list[str]:
    """Hide the sources of ``obj`` when its type is hidden by FreeCAD's own
    command (or is a loft or sweep) and ``changed`` (the property names just
    set, or None for a new object) includes a source link. Returns the names of
    the objects that were visible and are hidden now."""
    hidden = []
    for prop in SOURCE_PROPERTIES.get(getattr(obj, "TypeId", ""), ()):
        if changed is not None and prop not in changed:
            continue
        try:
            for source in _source_objects(getattr(obj, prop)):
                view = getattr(source, "ViewObject", None)
                if view is None or not view.Visibility:
                    continue
                view.Visibility = False
                hidden.append(source.Name)
        except Exception:
            pass
    return hidden


def visibility_snapshot(doc: Any) -> dict[str, bool]:
    """Name -> visibility of every object of ``doc`` that has a view object."""
    shown = {}
    for obj in doc.Objects:
        view = getattr(obj, "ViewObject", None)
        if view is None:
            continue
        try:
            shown[obj.Name] = bool(view.Visibility)
        except Exception:
            pass
    return shown


def newly_hidden(doc: Any, before: dict[str, bool]) -> list[str]:
    """The objects of ``doc`` that were visible in ``before`` and are hidden
    now: FreeCAD's view providers hide the inputs of a boolean or a compound
    when it is made, and the caller has to be told."""
    after = visibility_snapshot(doc)
    return [name for name, was in before.items() if was and after.get(name) is False]
