"""Hide the source of a feature the way FreeCAD's own command does.

FreeCAD's dialogs and commands for Fillet, Chamfer, Extrusion, Revolution and
Thickness hide the object they build on, so the result is not covered by it
(DlgFilletEdges, DlgExtrusion, DlgRevolution, CmdPartThickness). Booleans and
compounds hide their inputs in their view provider already. Mirroring, Offset,
Loft, Sweep and Ruled Surface leave their sources shown, so they are not
listed.
"""

from typing import Any

#: Feature type -> the property that links its source.
SOURCE_PROPERTIES = {
    "Part::Fillet": "Base",
    "Part::Chamfer": "Base",
    "Part::Extrusion": "Base",
    "Part::Revolution": "Source",
    "Part::Thickness": "Faces",
}


def _source_object(value: Any) -> Any:
    """The object a link value points at: a bare object, or the first of the
    ``(object, sub-elements)`` pair a sub-element link holds."""
    if isinstance(value, (tuple, list)):
        value = value[0] if value else None
    return value if hasattr(value, "Name") else None


def hide_sources(obj: Any, changed: Any) -> list[str]:
    """Hide the source of ``obj`` when its type is hidden by FreeCAD's own
    command and ``changed`` (the property names just set, or None for a new
    object) includes the source link. Returns the names of the objects that
    were visible and are hidden now."""
    prop = SOURCE_PROPERTIES.get(getattr(obj, "TypeId", ""))
    if prop is None or (changed is not None and prop not in changed):
        return []
    try:
        source = _source_object(getattr(obj, prop))
        view = getattr(source, "ViewObject", None)
        if view is None or not view.Visibility:
            return []
        view.Visibility = False
        return [source.Name]
    except Exception:
        return []


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
