"""List the faces and edges of an object with their names and geometry.

The names are the ``Face<N>`` and ``Edge<N>`` that measure, References and
attachments take. Shapes are read in global coordinates through
``tessellation.shape_of``, the same route measure uses. Part is imported
inside the GUI task.
"""

from typing import Any

from rpc_server.errors import INVALID_INPUT, fail, tool_call
from rpc_server.gui_task import run_on_gui
from rpc_server.lookup import require_document, require_object
from rpc_server.serialize import finite_or_none, tight_bound_box


SUBELEMENT_KINDS = ("faces", "edges", "all")

#: The filters of list_subelements. ``curve``, ``along``, ``smooth`` and
#: ``min_length`` filter edges, ``surface`` filters faces, ``on_bottom`` both.
EDGE_FILTERS = ("curve", "along", "smooth", "min_length")
FACE_FILTERS = ("surface",)
FILTER_NAMES = EDGE_FILTERS + FACE_FILTERS + ("on_bottom",)
_CURVES = ("line", "circle", "other")
_SURFACES = ("plane", "cylinder", "cone", "sphere", "torus", "other")

# Fixed budget: list_subelements takes no timeout argument.
_TIMEOUT = 60.0

# An edge shorter than this (mm) is degenerate: the pole of a rounded corner.
_DEGENERATE_MM = 1e-6

# A face or edge whose top is within this many mm of the shape's lowest point
# lies on the bottom, on the print plate.
_BOTTOM_TOLERANCE_MM = 1e-4

# Decimals kept in reported lengths, areas and coordinates (millimetres).
_DECIMALS = 4


def _num(value: float) -> float | None:
    number = finite_or_none(value)
    # Adding 0.0 turns a negative zero into a plain zero.
    return None if number is None else round(number, _DECIMALS) + 0.0


def _vector(vector: Any) -> list[float | None]:
    return [_num(vector.x), _num(vector.y), _num(vector.z)]


def _surface_kind(surface: Any, Part: Any) -> str:
    for kind, name in (
        ("plane", "Plane"),
        ("cylinder", "Cylinder"),
        ("cone", "Cone"),
        ("sphere", "Sphere"),
        ("torus", "Toroid"),
    ):
        cls = getattr(Part, name, None)
        if cls is not None and isinstance(surface, cls):
            return kind
    return "other"


def _on_bottom(part: Any, lowest: float | None) -> bool:
    """Whether ``part`` (a face or edge) lies wholly at the shape's lowest z."""
    if lowest is None:
        return False
    try:
        return tight_bound_box(part).ZMax <= lowest + _BOTTOM_TOLERANCE_MM
    except Exception:
        return False


# Two faces whose normals at the middle of their shared edge differ by less
# than this (radians) join smoothly. Measured: the boundary edge of a fillet
# gives exactly 0 and every crease of a box 1.5708, and FreeCAD's fillet and
# chamfer refuse exactly the edges below this, so any value in between serves.
_SMOOTH_ANGLE_RAD = 1e-3


def _edge_faces(faces: list) -> dict[int, list[tuple[Any, int]]]:
    """Edge hash -> the (edge, face index) pairs of the faces that hold it,
    built in one pass over ``faces`` (read once: shape.Faces builds the whole
    list on every access) instead of one search per edge."""
    held: dict[int, list[tuple[Any, int]]] = {}
    for index, face in enumerate(faces):
        for edge in face.Edges:
            held.setdefault(edge.hashCode(), []).append((edge, index))
    return held


def _join_kind(faces: list, edge: Any, held: dict[int, list[tuple[Any, int]]]) -> str | None:
    """"seam" when ``edge`` is where one face meets itself (a cylinder's seam),
    "smooth" when the two faces on it meet tangentially, else None (a crease,
    an edge on a single face boundary, or anything unreadable)."""
    try:
        indexes = sorted({index for other, index in held.get(edge.hashCode(), []) if other.isSame(edge)})
        sides = [faces[index] for index in indexes]
        if len(sides) == 1:
            return "seam" if edge.isSeam(sides[0]) else None
        if len(sides) != 2:
            return None
        point = edge.valueAt((edge.FirstParameter + edge.LastParameter) / 2)
        normals = []
        for face in sides:
            u, v = face.Surface.parameter(point)
            normals.append(face.normalAt(u, v))
        return "smooth" if normals[0].getAngle(normals[1]) < _SMOOTH_ANGLE_RAD else None
    except Exception:
        return None


def _face_row(name: str, face: Any, Part: Any, lowest: float | None) -> dict[str, Any]:
    row: dict[str, Any] = {"name": name}
    try:
        surface = face.Surface
    except Exception:
        surface = None
    kind = _surface_kind(surface, Part)
    row["surface"] = kind
    row["area"] = _num(face.Area)
    row["center"] = _vector(face.CenterOfMass)
    if kind == "plane":
        u0, u1, v0, v1 = face.ParameterRange
        row["normal"] = _vector(face.normalAt((u0 + u1) / 2, (v0 + v1) / 2))
    elif kind in ("cylinder", "sphere"):
        row["radius"] = _num(surface.Radius)
        row["axis"] = _vector(surface.Axis)
    if _on_bottom(face, lowest):
        row["on_bottom"] = True
    return row


def _edge_curve(edge: Any, Part: Any) -> tuple[Any, str]:
    """The curve of ``edge`` and its kind: line, circle or other."""
    try:
        curve = edge.Curve
    except Exception:
        curve = None
    if isinstance(curve, (Part.Line, Part.LineSegment)):
        return curve, "line"
    if isinstance(curve, Part.Circle):
        return curve, "circle"
    return curve, "other"


def _along(edge: Any) -> str | None:
    """The axis (x, y or z) a straight edge is parallel to, else None."""
    try:
        direction = edge.valueAt(edge.LastParameter) - edge.valueAt(edge.FirstParameter)
        if direction.Length > 0:
            direction.normalize()
            for axis, value in zip("xyz", (direction.x, direction.y, direction.z)):
                if abs(abs(value) - 1.0) < 1e-6:
                    return axis
    except Exception:
        pass
    return None


def _face_matches(face: Any, filters: dict[str, Any], Part: Any, lowest: float | None) -> bool:
    """Whether ``face`` passes the filters that apply to faces, cheapest first."""
    if "surface" in filters:
        try:
            surface = face.Surface
        except Exception:
            surface = None
        if _surface_kind(surface, Part) != filters["surface"]:
            return False
    if "on_bottom" in filters and _on_bottom(face, lowest) != filters["on_bottom"]:
        return False
    return True


def _edge_matches(edge: Any, filters: dict[str, Any], Part: Any, lowest: float | None) -> bool:
    """Whether ``edge`` passes every edge filter but ``smooth`` (which needs the
    faces around it), cheapest first."""
    if "min_length" in filters and edge.Length < filters["min_length"]:
        return False
    if "curve" in filters and _edge_curve(edge, Part)[1] != filters["curve"]:
        return False
    # Only a straight edge runs along an axis: the end points of an arc can line up with one.
    if "along" in filters and (_edge_curve(edge, Part)[1] != "line" or _along(edge) != filters["along"]):
        return False
    if "on_bottom" in filters:
        bottom = edge.Length >= _DEGENERATE_MM and _on_bottom(edge, lowest)
        if bottom != filters["on_bottom"]:
            return False
    return True


def _edge_row(name: str, edge: Any, Part: Any, lowest: float | None, join: str | None = None) -> dict[str, Any]:
    row: dict[str, Any] = {"name": name}
    curve, kind = _edge_curve(edge, Part)
    row["curve"] = kind
    row["length"] = _num(edge.Length)
    if edge.Length < _DEGENERATE_MM:
        row["degenerate"] = True
    elif join is not None:
        row["smooth"] = True
        if join == "seam":
            row["seam"] = True
    try:
        start = edge.valueAt(edge.FirstParameter)
        end = edge.valueAt(edge.LastParameter)
        row["start"] = _vector(start)
        row["end"] = _vector(end)
        if kind == "line":
            direction = end - start
            if direction.Length > 0:
                direction.normalize()
                row["direction"] = _vector(direction)
                # An edge parallel to an axis is named by it, so vertical and
                # horizontal edges tell apart at a glance.
                for axis, value in zip("xyz", (direction.x, direction.y, direction.z)):
                    if abs(abs(value) - 1.0) < 1e-6:
                        row["along"] = axis
    except Exception:
        pass
    if kind == "circle":
        row["radius"] = _num(curve.Radius)
        row["center"] = _vector(curve.Center)
    if not row.get("degenerate") and _on_bottom(edge, lowest):
        row["on_bottom"] = True
    return row


def check_filters(kind: str, filters: Any) -> tuple[dict[str, Any], dict[str, Any] | None]:
    """The filters as given (None values dropped), or a fail() reply: an
    unknown name or value, or a filter that cannot apply to ``kind``."""
    if filters is None:
        return {}, None
    if not isinstance(filters, dict):
        return {}, fail(INVALID_INPUT, f"filters must be an object, got {filters!r}")
    given = {k: v for k, v in filters.items() if v is not None}
    for name in given:
        if name not in FILTER_NAMES:
            return {}, fail(INVALID_INPUT, f"unknown filter {name!r}; the filters are {', '.join(FILTER_NAMES)}")
    if kind == "faces":
        for name in EDGE_FILTERS:
            if name in given:
                return {}, fail(INVALID_INPUT, f"{name} filters edges: use kind edges or all", "surface and on_bottom filter faces.")
    if kind == "edges" and "surface" in given:
        return {}, fail(INVALID_INPUT, "surface filters faces: use kind faces or all", "curve, along, smooth, min_length and on_bottom filter edges.")
    number = given.get("min_length")
    checks = (
        ("curve", given.get("curve") in (None, *_CURVES), f"curve must be one of {', '.join(_CURVES)}"),
        ("surface", given.get("surface") in (None, *_SURFACES), f"surface must be one of {', '.join(_SURFACES)}"),
        ("along", given.get("along") in (None, "x", "y", "z"), "along must be x, y or z"),
        ("on_bottom", isinstance(given.get("on_bottom", True), bool), "on_bottom must be true or false"),
        ("smooth", isinstance(given.get("smooth", True), bool), "smooth must be true or false"),
        ("min_length", number is None or (isinstance(number, (int, float)) and not isinstance(number, bool) and number >= 0),
         "min_length must be a number of mm, 0 or more"),
    )
    for name, ok, message in checks:
        if name in given and not ok:
            return {}, fail(INVALID_INPUT, message)
    return given, None


def _list_subelements_gui(doc_name: str, obj_name: str, kind: str, filters: dict[str, Any]) -> dict[str, Any]:
    import Part

    from rpc_server.tessellation import shape_of

    doc, error = require_document(doc_name)
    if error is not None:
        return error
    obj, error = require_object(doc, obj_name)
    if error is not None:
        return error
    shape = shape_of(obj)
    if shape is None:
        return fail(
            INVALID_INPUT,
            f"object {obj_name!r} has no shape, so it has no faces or edges",
            "Call " + tool_call("get_object", {"doc_name": doc.Name, "obj_name": obj_name})
            + " to see its type and properties.",
        )

    result: dict[str, Any] = {
        "success": True,
        "document": doc.Name,
        "object": obj_name,
        "kind": kind,
    }
    try:
        lowest = finite_or_none(tight_bound_box(shape).ZMin)
    except Exception:
        lowest = None
    if kind in ("faces", "all"):
        faces = list(shape.Faces)
        result["face_count"] = len(faces)
        result["faces"] = [
            _face_row(f"Face{i}", face, Part, lowest)
            for i, face in enumerate(faces, 1)
            if _face_matches(face, filters, Part, lowest)
        ]
    if kind in ("edges", "all"):
        faces = shape.Faces
        edges = list(shape.Edges)
        result["edge_count"] = len(edges)
        held = _edge_faces(faces)
        rows = []
        for i, edge in enumerate(edges, 1):
            if not _edge_matches(edge, filters, Part, lowest):
                continue
            # The join of an edge (smooth, seam) costs the most, so it is looked
            # at last, and only for an edge that passed every other filter.
            join = None if edge.Length < _DEGENERATE_MM else _join_kind(faces, edge, held)
            if "smooth" in filters and (join is not None) != filters["smooth"]:
                continue
            rows.append(_edge_row(f"Edge{i}", edge, Part, lowest, join))
        result["edges"] = rows
    if filters:
        result["filters"] = filters
    return result


def list_subelements(doc_name: str, obj_name: str, kind: str = "faces", filters: Any = None) -> dict[str, Any]:
    """List the faces and/or edges of ``obj_name``.

    ``kind`` is "faces", "edges" or "all". Reply: ``{"success", "document",
    "object", "kind", "faces"?: [{"name", "surface", "area", "center",
    "normal"? (planes), "radius"?, "axis"? (cylinders and spheres),
    "on_bottom"? (true when the face lies wholly at the shape's lowest z)}],
    "edges"?: [{"name", "curve", "length", "start", "end", "direction"?
    (lines), "along"? (a line parallel to the x, y or z axis), "radius"?,
    "center"? (circles), "degenerate"? (true for an edge with no length),
    "smooth"? (true when the faces on the edge meet tangentially, or the edge
    is a seam where one face meets itself; FreeCAD cannot fillet or chamfer
    it), "seam"? (true for the second kind), "on_bottom"?}]}``.
    Units are millimetres and square millimetres. GUI thread, 60 s. No
    transaction.

    ``filters`` ({"curve", "surface", "along", "on_bottom", "smooth",
    "min_length"}) keep only the matching rows; the rows of the others are
    never built. The reply then also has "filters", and "face_count" and
    "edge_count" (always sent) hold the totals, so the matched count is the
    length of the list.
    """
    if kind not in SUBELEMENT_KINDS:
        return fail(
            INVALID_INPUT,
            f"invalid kind: {kind!r}; kind must be one of {', '.join(SUBELEMENT_KINDS)}",
        )
    given, error = check_filters(kind, filters)
    if error is not None:
        return error
    return run_on_gui(
        lambda: _list_subelements_gui(doc_name, obj_name, kind, given), _TIMEOUT, "list_subelements"
    )
