"""Which shapes a call changed, for the replies of undo, redo, recompute_document,
update_spreadsheet_cells and delete_object.

A sheet cell, an undo or a delete changes shapes the caller never touched
directly, and nothing in the reply said so. A snapshot before the call holds
the hash of every object's shape (one O(1) read per object, no volume or
topology walk); after the call only the objects whose hash changed are
looked at: the top-level ones (as the tree shows them, so a Body counts once
and its features not at all) get a shape summary. A call that rebuilt nothing
reports nothing.
"""

from typing import Any

from rpc_server.agent_log import agent_warning
from rpc_server.object_validation import MAX_LISTED_OBJECTS
from rpc_server.serialize import shape_summary
from rpc_server.tessellation import tree_root_objects


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


def changed_shapes(doc: Any, before: dict[str, int | None] | None) -> dict[str, Any]:
    """``{"changed_shapes": [{"name", "shape"}], "changed_shapes_count",
    "changed_shapes_truncated"}`` for the top-level objects whose shape changed
    since ``before`` (an object that is new counts), or ``{}`` when none did
    or it could not be read."""
    if before is None:
        return {}
    try:
        changed = {
            str(obj.Name): obj
            for obj in doc.Objects
            if (sig := _signature(obj)) is not None and before.get(str(obj.Name)) != sig
        }
        if not changed:
            return {}
        rows = []
        count = 0
        for obj in tree_root_objects(doc):
            if str(obj.Name) not in changed:
                continue
            count += 1
            if len(rows) < MAX_LISTED_OBJECTS:
                summary = shape_summary(obj.Shape)
                if summary is not None:
                    rows.append({"name": str(obj.Name), "shape": summary})
        if not count:
            return {}
        return {"changed_shapes": rows, "changed_shapes_count": count, "changed_shapes_truncated": count > len(rows)}
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the shapes the call changed: {type(e).__name__}: {e}\n")
        return {}
