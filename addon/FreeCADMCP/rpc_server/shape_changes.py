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
    since ``before`` (an object that is new counts), or ``{}`` when none did
    or it could not be read. ``exclude`` names objects to leave out, such as
    the one a create_object or update_object call is about, which has its own
    Shape line."""
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
        count = 0
        for obj in doc.Objects:
            if str(obj.Name) not in reported:
                continue
            count += 1
            if len(rows) < MAX_LISTED_OBJECTS:
                summary = object_shape_summary(obj)
                if summary is not None:
                    rows.append({"name": str(obj.Name), "shape": summary})
        if not count:
            return {}
        return {"changed_shapes": rows, "changed_shapes_count": count, "changed_shapes_truncated": count > len(rows)}
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the shapes the call changed: {type(e).__name__}: {e}\n")
        return {}
