"""Post-recompute validity checks for FreeCAD document objects."""

from typing import Any, Iterable


_FAILED_STATES = {"invalid", "error", "touched"}

MAX_LISTED_OBJECTS = 200
"""Row cap for invalid_objects_report() and any caller-built list of the same
shape (recompute_document's touched_objects, for instance).

A document with tens of thousands of flagged objects would otherwise return
a reply of several megabytes; invalid_objects_report()'s invalid_count still
gives the true total when invalid_truncated is set.
"""


def object_states(obj: Any) -> list[str]:
    """Return FreeCAD's state labels without assuming a concrete container."""
    try:
        raw_state = obj.State
    except Exception:
        return []

    if isinstance(raw_state, str):
        return [raw_state]

    try:
        return [str(item) for item in raw_state]
    except Exception:
        return []


def object_status(obj: Any) -> str:
    """Return ``obj.getStatusString()``, stripped, or "" when it is unavailable."""
    get_status = getattr(obj, "getStatusString", None)
    try:
        return str(get_status()).strip() if callable(get_status) else ""
    except Exception:
        return ""


#: What FreeCAD says when a fillet or chamfer loses an edge it rounds (an Edges
#: entry the Base no longer has) and when its size does not fit the edges
#: (OpenCascade's "BRep_API: command not done"), or an edge it cannot round at all.
_LOST_EDGE_MARKERS = ("Missing edge link", "NCollection_IndexedMap::FindKey")
_ROUNDING_FAILED_MARKER = "BRep_API: command not done"
_NO_SUITABLE_EDGES_MARKER = "There are no suitable edges"
_ROUNDING_SIZE_WORD = {"Part::Fillet": "radius", "Part::Chamfer": "size"}


_UNSET: Any = object()


def _rounding_advice(obj: Any, reason: str) -> str:
    """What to do about a failed fillet or chamfer, or "" for any other
    object or message."""
    word = _ROUNDING_SIZE_WORD.get(str(getattr(obj, "TypeId", "")))
    if word is None:
        return ""
    base = str(getattr(getattr(obj, "Base", None), "Name", ""))
    if base and any(marker in reason for marker in _LOST_EDGE_MARKERS):
        return (
            f"An edge it rounds no longer exists in {base} after the change. "
            f"Call list_subelements on {base} and set Edges again."
        )
    if base and _NO_SUITABLE_EDGES_MARKER in reason:
        return (
            f"An edge in Edges cannot be rounded: call list_subelements on {base} and leave out "
            "edges marked degenerate or smooth."
        )
    if _ROUNDING_FAILED_MARKER in reason:
        return (
            f"The {word} is probably too large for these edges: try a smaller {word} or fewer edges, "
            "and leave out degenerate edges."
        )
    return ""


def failed_dependency(obj: Any) -> str | None:
    """The name of the nearest object ``obj`` depends on that failed, for an
    object that is only Touched: FreeCAD skips the dependents of a failed
    object and they say nothing else. None when ``obj`` failed itself, is not
    Touched, or no dependency failed."""
    states = {state.strip().casefold() for state in object_states(obj)}
    if "touched" not in states or object_validity_error(obj, exclude_touched=True, explain=False) is not None:
        return None
    seen = {id(obj)}
    level = [obj]
    while level:
        following = []
        for current in level:
            for dep in getattr(current, "OutList", None) or []:
                if id(dep) in seen:
                    continue
                seen.add(id(dep))
                if object_validity_error(dep, exclude_touched=True, explain=False) is not None:
                    return str(getattr(dep, "Name", ""))
                following.append(dep)
        level = following
    return None


def failure_reason(obj: Any, waits_for: str | None = _UNSET) -> str:
    """Why ``obj`` is flagged: FreeCAD's own status text, with what to do about
    a failed fillet or chamfer after it, or, for an object that only waits on
    a failed dependency, which one. ``waits_for`` is that dependency when the
    caller has it already (the search is not repeated)."""
    if waits_for is _UNSET:
        waits_for = failed_dependency(obj)
    if waits_for:
        return f"waits for {waits_for}, which failed."
    reason = object_status(obj)
    advice = _rounding_advice(obj, reason)
    if advice:
        return f"{reason.rstrip('.')}. {advice}" if reason else advice
    return reason


def object_validity_error(obj: Any, *, exclude_touched: bool = False, explain: bool = True) -> str | None:
    """Return a diagnostic when ``obj`` is invalid, otherwise ``None``.

    Shape presence is deliberately not used as the discriminator. Containers,
    groups, spreadsheets, and empty sketches can all be valid without a shape.

    Touched counts as a failure by default: after an actual recompute,
    ``isValid()`` only reflects the Error bit, but a dependent of an object
    whose recompute failed is skipped entirely (never reaches its own
    recompute, and is never purged of Touched), so it stays Touched with
    ``isValid() == True`` even though it did not compute. A caller that runs
    no recompute of its own before checking (a document just opened, or
    freshly loaded state before any change) passes ``exclude_touched=True``,
    since there a bare Touched only means "not recomputed yet", not broken.
    ``explain`` False leaves FreeCAD's own status text as the reason, without
    the advice and the dependency search that ``failure_reason`` adds (those
    call this function on other objects).
    """
    name = str(getattr(obj, "Name", "<unknown>"))
    states = object_states(obj)
    failed_state_names = _FAILED_STATES - {"touched"} if exclude_touched else _FAILED_STATES
    failed_states = [
        state for state in states if state.strip().casefold() in failed_state_names
    ]
    is_valid = getattr(obj, "isValid", None)

    if callable(is_valid):
        try:
            is_valid_result = bool(is_valid())
        except Exception as exc:
            return (
                f"Object '{name}' exists, but its validity could not be checked after "
                f"recompute: {type(exc).__name__}: {exc}. Fix or remove the object "
                "before building on it."
            )
    else:
        is_valid_result = True

    if is_valid_result and not failed_states:
        return None

    reason = failure_reason(obj) if explain else object_status(obj)

    state = ", ".join(states) if states else "unknown"

    detail = f": {reason}" if reason else ""
    return (
        f"Object '{name}' exists but failed to compute{detail} "
        f"(State: {state}). Fix the cause or remove the object before building "
        "on it."
    )


def invalid_object_row(obj: Any, waits_for: str | None = _UNSET) -> dict[str, Any]:
    """Build one row for ``obj``.

    ``{"name", "label", "type", "state": [str], "status": str, "waits_for": str}``
    (``waits_for`` names the failed object a Touched ``obj`` depends on, else
    ""), the shared shape every mutating and document-listing reply's
    ``invalid_objects`` uses. Does not itself decide whether ``obj`` is
    invalid; call
    ``object_validity_error`` (directly, or through ``invalid_objects_report``)
    for that.
    """
    if waits_for is _UNSET:
        waits_for = failed_dependency(obj)
    return {
        "name": str(getattr(obj, "Name", "")),
        "label": str(getattr(obj, "Label", "")),
        "type": str(getattr(obj, "TypeId", "")),
        "state": object_states(obj),
        "status": failure_reason(obj, waits_for),
        "waits_for": waits_for or "",
    }


def stale_dependents(failed: list[Any], limit: int = MAX_LISTED_OBJECTS, *, exclude_touched: bool = False) -> tuple[list[dict[str, str]], int]:
    """The objects with a shape that depend on a failed object and have not
    failed themselves, as ``({"name", "depends_on"} rows, true total)``.

    FreeCAD skips the dependents of an object whose recompute failed, and a
    dependent it does not mark Touched keeps the shape it was built with, valid
    and up to date to every check. Each is listed once, under the failed object
    that reaches it first without passing another failed object (that one is
    a source of its own). One walk over the dependency lists (``InList``) from
    all the failed objects with a visited set, so a cycle ends and the cost
    follows the dependents, not the document.
    """
    failed_names = {str(getattr(obj, "Name", "")) for obj in failed}
    seen = set(failed_names)
    rows: list[dict[str, str]] = []
    total = 0
    for source in failed:
        source_name = str(getattr(source, "Name", ""))
        queue = [source]
        while queue:
            following = []
            for current in queue:
                for dep in getattr(current, "InList", None) or []:
                    name = str(getattr(dep, "Name", ""))
                    if name in seen:
                        continue
                    seen.add(name)
                    following.append(dep)
                    if getattr(dep, "Shape", None) is None:
                        continue
                    if object_validity_error(dep, exclude_touched=exclude_touched, explain=False) is not None:
                        continue
                    total += 1
                    if len(rows) < limit:
                        rows.append({"name": name, "depends_on": source_name})
            queue = following
    return rows, total


def invalid_objects_report(
    objects: Iterable[Any], limit: int = MAX_LISTED_OBJECTS, *, exclude_touched: bool = False
) -> dict[str, Any]:
    """Return ``{"invalid_objects", "invalid_count", "invalid_truncated",
    "stale_objects", "stale_count", "stale_truncated"}`` for ``objects``.

    Every mutating and document-listing reply's invalid-object keys, built
    together in a single pass over ``objects`` so ``object_validity_error``
    runs once per object rather than once for the capped rows and again for
    the true count. ``objects`` is any iterable of FreeCAD document objects,
    most often a document's ``.Objects``, but a caller that only wants the
    objects it just created or touched may pass that narrower list instead.
    ``exclude_touched`` is passed through to ``object_validity_error``.
    ``invalid_objects`` is capped at ``limit`` rows; ``invalid_count`` is
    always the true total, and ``invalid_truncated`` is set once the cap cuts
    the list short. ``stale_objects`` (see ``stale_dependents``) follows the
    same cap rule.
    """
    rows: list[dict[str, Any]] = []
    failed: list[Any] = []
    count = 0
    for obj in objects:
        if object_validity_error(obj, exclude_touched=exclude_touched, explain=False) is None:
            continue
        count += 1
        failed.append(obj)
        if len(rows) < limit:
            rows.append(invalid_object_row(obj))
    stale, stale_total = stale_dependents(failed, limit, exclude_touched=exclude_touched) if failed else ([], 0)
    return {
        "invalid_objects": rows,
        "invalid_count": count,
        "invalid_truncated": count > len(rows),
        "stale_objects": stale,
        "stale_count": stale_total,
        "stale_truncated": stale_total > len(stale),
    }


def failed_names(objects: Iterable[Any]) -> set[str]:
    """The names of the objects that count as failed (see
    ``object_validity_error``), taken before a change so the objects the change
    makes fail can be told from the ones that already had."""
    return {
        str(getattr(obj, "Name", ""))
        for obj in objects
        if object_validity_error(obj, explain=False) is not None
    }


def newly_failed_report(
    objects: Iterable[Any], before: set[str], changed: Any, limit: int = MAX_LISTED_OBJECTS
) -> dict[str, Any]:
    """The invalid-object keys of ``invalid_objects_report`` for the objects
    that failed after a change although they had not failed before it
    (``before`` is ``failed_names`` from before), leaving out ``changed``, the
    object the call is about and reports itself (by Name; None when the call
    removed it). The objects that built on a failed one (``changed`` too, when
    it failed) and were not rebuilt come with them. ``{}`` when nothing newly
    failed. An object that failed before and still fails is not the change's
    doing, so it is left out."""
    changed_name = None if changed is None else str(getattr(changed, "Name", ""))
    newly = [
        obj
        for obj in objects
        if str(getattr(obj, "Name", "")) != changed_name
        and str(getattr(obj, "Name", "")) not in before
        and object_validity_error(obj, explain=False) is not None
    ]
    if not newly:
        return {}
    sources = list(newly)
    if changed is not None and object_validity_error(changed, explain=False) is not None:
        sources.append(changed)
    rows = [invalid_object_row(obj) for obj in newly[:limit]]
    stale, stale_total = stale_dependents(sources, limit)
    return {
        "invalid_objects": rows,
        "invalid_count": len(newly),
        "invalid_truncated": len(newly) > len(rows),
        "stale_objects": stale,
        "stale_count": stale_total,
        "stale_truncated": stale_total > len(stale),
    }
