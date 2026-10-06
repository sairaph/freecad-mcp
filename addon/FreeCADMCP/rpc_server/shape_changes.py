"""Which shapes a call changed, for the replies of undo, redo, recompute_document,
update_spreadsheet_cells and delete_object.

A sheet cell, an undo or a delete changes shapes the caller never touched
directly, and nothing in the reply said so. A snapshot before the call holds
the hash of every object's shape (one O(1) read per object, no volume or
topology walk); after the call only the objects whose hash changed are
looked at: the topmost ones that have a shape (as the tree shows them, so a Body
counts once and its features not at all, and a member of an App::Part, which has
no shape, counts itself, placed as the Part puts it) get a shape summary. A call that rebuilt nothing
reports nothing.
"""

from typing import Any

from rpc_server.agent_log import agent_warning
from rpc_server.object_validation import MAX_LISTED_OBJECTS
from rpc_server.serialize import object_shape_summary
from rpc_server.empty_results import prune_missing
from rpc_server.tessellation import parent_map


def _signature(obj: Any) -> int | None:
    """The hash of ``obj``'s shape, None when it has none or it is null."""
    try:
        if "Shape" not in obj.PropertiesList:
            return None
        shape = obj.Shape
        if shape is None or shape.isNull():
            return None
        return int(shape.hashCode())
    except Exception:
        return None


def snapshot(doc: Any) -> dict[str, int | None] | None:
    """Object name -> shape hash for every object of ``doc``, or None when it
    could not be taken (a report must never fail the call)."""
    try:
        return {str(obj.Name): _signature(obj) for obj in doc.Objects}
    except Exception as e:
        agent_warning(f"MCP RPC: could not note the shapes before the call: {type(e).__name__}: {e}\n")
        return None


#: The last summary the tool computed for each object, by (document, object), in
#: the rounding the Shape line shows. An object whose shape was rebuilt to a
#: summary equal to its stored one is not news; one with no stored summary is.
_summaries: dict[tuple[str, str], tuple] = {}


def _displayed(summary: dict[str, Any]) -> tuple:
    """``summary`` as the Shape line shows it: the counts, the size to 0.01 mm
    and the volume to 0.1 mm^3."""
    size = summary.get("size")
    volume = summary.get("volume")
    return (
        summary.get("solids"), summary.get("shells"), summary.get("faces"), summary.get("edges"),
        None if size is None else tuple(round(v, 2) if v is not None else None for v in size),
        None if volume is None else round(volume, 1),
        bool(summary.get("null")),
    )


def _key(obj: Any) -> tuple[str, str]:
    return (str(getattr(getattr(obj, "Document", None), "Name", "")), str(obj.Name))


def remember_summaries(items: list[tuple[Any, dict[str, Any] | None]]) -> None:
    """Keep each summary (what a reply just showed for its object) for the next
    comparison. Entries of closed documents and deleted objects are dropped
    first, once for the whole list, so they go the next time one is stored."""
    items = [(obj, summary) for obj, summary in items if summary is not None]
    if not items:
        return
    prune_missing(_summaries)
    for obj, summary in items:
        _summaries[_key(obj)] = _displayed(summary)


def _top_shaped(doc: Any, obj: Any, parents: dict[str, str]) -> str:
    """The Name of the highest object above ``obj`` (itself included) that has a
    shape: a Body for its feature, a Cut for its input, the top-level object
    otherwise. An App::Part has no shape, so the walk stops below it."""
    node = obj
    for _ in range(len(parents) + 1):
        parent = doc.getObject(parents[str(node.Name)]) if str(node.Name) in parents else None
        if parent is None or _signature(parent) is None:
            break
        node = parent
    return str(node.Name)


def changed_shapes(doc: Any, before: dict[str, int | None] | None, exclude: Any = ()) -> dict[str, Any]:
    """``{"changed_shapes": [{"name", "shape"}], "changed_shapes_count",
    "changed_shapes_truncated"}`` for the topmost shaped objects whose shape changed
    since ``before`` (an object that is new counts), or ``{}`` when none did or
    it could not be read. An object whose new summary, as the Shape line shows
    it, equals the last one the tool computed for it was rebuilt to the same
    geometry and is left out; one the tool never summarised is listed.
    ``exclude`` names objects to leave out, such as the one a create_object or
    update_object call is about, which has its own Shape line."""
    if before is None:
        return {}
    try:
        skip = set(exclude)
        changed = {
            str(obj.Name): obj
            for obj in doc.Objects
            if str(obj.Name) not in skip and (sig := _signature(obj)) is not None and before.get(str(obj.Name)) != sig
        }
        if not changed:
            return {}
        parents = parent_map(doc)
        reported = {name for name in (_top_shaped(doc, obj, parents) for obj in changed.values()) if name in changed}
        rows = []
        remembered = []
        count = 0
        for obj in doc.Objects:
            if str(obj.Name) not in reported:
                continue
            if len(rows) >= MAX_LISTED_OBJECTS:
                # Past the cap nothing more is shown, so nothing more is summarised: the rest are counted.
                count += 1
                continue
            summary = object_shape_summary(obj)
            if summary is None:
                continue
            remembered.append((obj, summary))
            stored = _summaries.get(_key(obj))
            if stored is not None and stored == _displayed(summary):
                continue
            count += 1
            rows.append({"name": str(obj.Name), "shape": summary})
        remember_summaries(remembered)
        if not count:
            return {}
        return {"changed_shapes": rows, "changed_shapes_count": count, "changed_shapes_truncated": count > len(rows)}
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the shapes the call changed: {type(e).__name__}: {e}\n")
        return {}
