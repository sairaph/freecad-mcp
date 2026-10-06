"""PartDesign and sketch support for ``create_object`` and ``update_object``.

A PartDesign feature, and a sketch meant for a Body, belong in that Body the
way FreeCAD's own commands make them (``Body.newObject``, which also sets
``BaseFeature`` and ``Tip``); an object made with ``doc.addObject`` stays
outside it and the next feature cannot build on it. This module chooses the
Body, resolves a sketch's ``AttachmentSupport`` (``Support`` in older
FreeCAD), adds ``Geometry`` to a sketch, and maps ``Edges`` of a PartDesign
Fillet or Chamfer onto its ``Base``. Everything here raises ``ValueError``
with a message the caller can act on, before the document is changed.
"""

import math
import re
from dataclasses import dataclass, field
from typing import Any

import FreeCAD

from rpc_server.errors import tool_call
from rpc_server.property_mapper import parse_reference_entry

BODY_TYPE = "PartDesign::Body"
SKETCH_TYPE = "Sketcher::SketchObject"
DRESSUPS = {"PartDesign::Fillet": "Radius", "PartDesign::Chamfer": "Size"}

#: Roles of a Body's origin features. A bare role name ("XY_Plane") given as a
#: sketch's support means the origin plane of the Body the sketch goes into.
ROLE_NAMES = ("X_Axis", "Y_Axis", "Z_Axis", "XY_Plane", "XZ_Plane", "YZ_Plane")

_SUB_NAME = re.compile(r"^(Face|Edge|Vertex)[1-9]\d*$")


@dataclass
class Plan:
    """What a create or update call does beyond setting properties."""

    body: Any = None
    properties: dict[str, Any] = field(default_factory=dict)
    geometry: list | None = None
    notes: list[str] = field(default_factory=list)


def is_feature_type(obj_type: str) -> bool:
    """A PartDesign type that lives in a Body (every one but the Body itself)."""
    return obj_type.startswith("PartDesign::") and obj_type != BODY_TYPE


def bodies(doc: Any) -> list:
    return [o for o in doc.Objects if o.TypeId == BODY_TYPE]


def _body_list(doc: Any) -> str:
    return ", ".join(b.Name for b in bodies(doc)) or "none"


def owner_body(doc: Any, obj: Any) -> Any:
    """The Body that holds ``obj`` (a feature, a sketch, or a feature of its
    origin), the Body itself when ``obj`` is one, else None."""
    if obj is None:
        return None
    if obj.TypeId == BODY_TYPE:
        return obj
    for body in bodies(doc):
        if any(member.Name == obj.Name for member in body.Group):
            return body
        origin = getattr(body, "Origin", None)
        if origin is not None and obj.Name in {origin.Name, *(f.Name for f in origin.OriginFeatures)}:
            return body
    return None


def named_body(doc: Any, name: str) -> Any:
    body = doc.getObject(name) if isinstance(name, str) else None
    if body is None or body.TypeId != BODY_TYPE:
        raise ValueError(f"body_name '{name}' is not a PartDesign::Body of this document (Bodies: {_body_list(doc)}).")
    return body


def origin_feature(body: Any, role: str) -> Any:
    for feature in body.Origin.OriginFeatures:
        if feature.Role == role:
            return feature
    raise ValueError(f"Body '{body.Name}' has no origin feature {role}.")


def _link_names(value: Any) -> list[str]:
    """The object names a link value mentions: "Sketch", ["Pad", ["Edge1"]],
    {"object_name": "Pad", ...} or a list of those."""
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        name = value.get("object_name", value.get("Object"))
        return [name] if isinstance(name, str) else []
    if isinstance(value, (list, tuple)):
        if len(value) == 2 and isinstance(value[0], str) and not isinstance(value[1], str):
            return [value[0]]
        return [n for item in value for n in _link_names(item)]
    return []


def _link_owners(doc: Any, properties: dict[str, Any]) -> dict[str, tuple[str, Any]]:
    """The Bodies the link properties point into: Body name to (property, Body)."""
    owners: dict[str, tuple[str, Any]] = {}
    for key in ("Profile", "Base", "AttachmentSupport", "Support"):
        for name in _link_names(properties.get(key)):
            body = owner_body(doc, doc.getObject(name))
            if body is not None:
                owners.setdefault(body.Name, (key, body))
    return owners


def _check_named_body(body: Any, owners: dict[str, tuple[str, Any]]) -> None:
    """Refuse a body_name that differs from the Body a link points into."""
    for name, (key, _other) in owners.items():
        if name != body.Name:
            raise ValueError(f"body_name '{body.Name}' differs from the Body that {key} points into ('{name}'): use one Body.")


def _body_of_links(doc: Any, obj_type: str, properties: dict[str, Any]) -> Any:
    """The Body a new PartDesign feature belongs to: the one its links point
    into, else the document's only Body."""
    owners = {name: body for name, (_key, body) in _link_owners(doc, properties).items()}
    if len(owners) > 1:
        raise ValueError(f"The links of this '{obj_type}' point into different Bodies ({', '.join(owners)}): use one Body.")
    if owners:
        return next(iter(owners.values()))
    found = bodies(doc)
    if len(found) == 1:
        return found[0]
    if not found:
        raise ValueError(f"'{obj_type}' is a PartDesign feature and needs a Body, and the document has none: create a {BODY_TYPE} first.")
    raise ValueError(f"'{obj_type}' needs a Body and the document has several ({_body_list(doc)}): pass body_name.")


# Sketch support.

def _entries(value: Any) -> list[tuple[str, list[str]]]:
    """A support value as ``(object name, sub-elements)`` entries. The forms of
    References: "Pad", {"object_name": "Pad", "face": "Face6"}, ["Pad", "Face6"],
    ["Pad", ["Face6"]] and a list of such entries; ["Box", "Cylinder"] is two
    objects when the second is not a sub-element name."""
    if isinstance(value, str):
        return [(value, [])]
    if isinstance(value, dict):
        return [_entry(value)]
    if isinstance(value, (list, tuple)) and value:
        if len(value) == 2 and isinstance(value[0], str):
            second = value[1]
            if isinstance(second, str) and _SUB_NAME.match(second):
                return [(value[0], [second])]
            if isinstance(second, (list, tuple)) and second and all(isinstance(s, str) and _SUB_NAME.match(s) for s in second):
                return [(value[0], list(second))]
        return [(item, []) if isinstance(item, str) else _entry(item) for item in value]
    raise ValueError(
        f"Invalid AttachmentSupport {value!r}; expected \"XY_Plane\", {{\"object_name\": \"Pad\", \"face\": \"Face6\"}}, "
        '["Pad", "Face6"] or [["Pad", ["Face6"]]].'
    )


def _entry(item: Any) -> tuple[str, list[str]]:
    name, subs = parse_reference_entry(item)
    return name, [subs] if isinstance(subs, str) else list(subs)


def _check_subs(doc: Any, obj: Any, subs: list[str]) -> None:
    shape = getattr(obj, "Shape", None)
    if shape is None or not subs:
        return
    for sub in subs:
        try:
            shape.getElement(sub)
        except Exception:
            raise ValueError(
                f"'{sub}' does not exist on '{obj.Name}'. Call "
                + tool_call("list_subelements", {"doc_name": doc.Name, "obj_name": obj.Name, "kind": "faces"})
                + " to see its faces."
            ) from None


def _support(doc: Any, value: Any, body: Any) -> tuple[list[tuple[Any, list[str]]], Any]:
    """Resolve a support value to ``[(object, sub-elements)]`` and the Body the
    sketch goes into (``body`` when given, else the one the support is in)."""
    entries = _entries(value)
    objects: dict[str, Any] = {}
    for name, _subs in entries:
        if name in ROLE_NAMES:
            continue
        obj = doc.getObject(name)
        if obj is None:
            raise ValueError(f"Referenced object '{name}' not found.")
        objects[name] = obj
    if body is not None:
        found = {}
        for obj in objects.values():
            other = owner_body(doc, obj)
            if other is not None:
                found.setdefault(other.Name, ("AttachmentSupport", other))
        _check_named_body(body, found)
    if body is None:
        owners = {b.Name: b for b in (owner_body(doc, o) for o in objects.values()) if b is not None}
        if len(owners) > 1:
            raise ValueError(f"The support is in different Bodies ({', '.join(owners)}): use one Body.")
        body = next(iter(owners.values()), None)
    links = []
    for name, subs in entries:
        if name in ROLE_NAMES:
            if body is not None:
                obj = origin_feature(body, name)
            else:
                obj = doc.getObject(name)
                if obj is None:
                    raise ValueError(f"Referenced object '{name}' not found.")
                owner = owner_body(doc, obj)
                if owner is not None and len(bodies(doc)) > 1:
                    raise ValueError(f"'{name}' is ambiguous: the document has several Bodies ({_body_list(doc)}): pass body_name.")
                body = owner
        else:
            obj = objects[name]
        _check_subs(doc, obj, subs)
        links.append((obj, subs or [""]))
    return links, body


# Sketch geometry.

def _number(value: Any, what: str) -> float:
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        raise ValueError(f"{what} must be a number, not {value!r}.")
    return float(value)


def _point(value: Any, what: str) -> Any:
    if not isinstance(value, (list, tuple)) or len(value) != 2:
        raise ValueError(f"{what} must be [x, y], not {value!r}.")
    return FreeCAD.Vector(_number(value[0], what), _number(value[1], what), 0)


_GEOMETRY_FORMS = (
    '{"line": [[x1, y1], [x2, y2]]}, {"circle": {"center": [x, y], "radius": r}}, '
    '{"arc": {"center": [x, y], "radius": r, "start_angle": a, "end_angle": b}} (degrees, counter-clockwise) '
    'or {"rectangle": {"corner": [x, y], "size": [w, h]}}'
)


def _parse_geometry(entries: Any) -> list[tuple[str, Any]]:
    """The geometry entries as ``(kind, FreeCAD objects)``, all checked before
    the sketch is touched."""
    import Part

    if not isinstance(entries, (list, tuple)):
        raise ValueError(f"Geometry must be a list of entries: {_GEOMETRY_FORMS}.")
    out = []
    for entry in entries:
        if not isinstance(entry, dict) or len(entry) != 1:
            raise ValueError(f"Invalid geometry entry {entry!r}; expected {_GEOMETRY_FORMS}.")
        (kind, spec), = entry.items()
        if kind == "line":
            if not isinstance(spec, (list, tuple)) or len(spec) != 2:
                raise ValueError(f"A line is [[x1, y1], [x2, y2]], not {spec!r}.")
            start, end = _point(spec[0], "A line point"), _point(spec[1], "A line point")
            if start.distanceToPoint(end) == 0:
                raise ValueError("A line needs two different points.")
            out.append((kind, Part.LineSegment(start, end)))
        elif kind in ("circle", "arc"):
            if not isinstance(spec, dict):
                raise ValueError(f"A {kind} is an object, not {spec!r}; expected {_GEOMETRY_FORMS}.")
            allowed = {"center", "radius"} | ({"start_angle", "end_angle"} if kind == "arc" else set())
            unknown = set(spec) - allowed
            if unknown or not allowed <= set(spec):
                raise ValueError(f"A {kind} takes {', '.join(sorted(allowed))}; got {', '.join(sorted(spec)) or 'nothing'}.")
            radius = _number(spec["radius"], f"The radius of a {kind}")
            if radius <= 0:
                raise ValueError(f"The radius of a {kind} must be above 0, not {radius:g}.")
            circle = Part.Circle(_point(spec["center"], f"The centre of a {kind}"), FreeCAD.Vector(0, 0, 1), radius)
            if kind == "circle":
                out.append((kind, circle))
                continue
            start, end = _number(spec["start_angle"], "start_angle"), _number(spec["end_angle"], "end_angle")
            if (end - start) % 360 == 0:
                raise ValueError("An arc needs different start_angle and end_angle; use a circle for a full turn.")
            # Counter-clockwise from start to end, however the two are written.
            start_rad = math.radians(start)
            out.append((kind, Part.ArcOfCircle(circle, start_rad, start_rad + math.radians((end - start) % 360))))
        elif kind == "rectangle":
            if not isinstance(spec, dict) or set(spec) != {"corner", "size"}:
                raise ValueError('A rectangle is {"corner": [x, y], "size": [w, h]}.')
            corner, size = _point(spec["corner"], "The corner of a rectangle"), _point(spec["size"], "The size of a rectangle")
            if size.x == 0 or size.y == 0:
                raise ValueError("A rectangle needs a width and a height above 0.")
            x, y, w, h = corner.x, corner.y, size.x, size.y
            corners = [FreeCAD.Vector(x, y, 0), FreeCAD.Vector(x + w, y, 0), FreeCAD.Vector(x + w, y + h, 0), FreeCAD.Vector(x, y + h, 0)]
            out.append((kind, [Part.LineSegment(corners[i], corners[(i + 1) % 4]) for i in range(4)]))
        else:
            raise ValueError(f"Unknown geometry '{kind}'; expected {_GEOMETRY_FORMS}.")
    return out


def set_geometry(sketch: Any, entries: Any) -> None:
    """Replace the geometry of ``sketch`` with ``entries``. A rectangle is four
    lines with their corners coincident. Constraints are not part of it."""
    parsed = _parse_geometry(entries)
    import Sketcher

    sketch.deleteAllGeometry()
    for kind, shapes in parsed:
        if kind != "rectangle":
            sketch.addGeometry(shapes, False)
            continue
        ids = [sketch.addGeometry(line, False) for line in shapes]
        for i in range(4):
            sketch.addConstraint(Sketcher.Constraint("Coincident", ids[i], 2, ids[(i + 1) % 4], 1))


def sketch_fields(sketch: Any) -> dict[str, Any]:
    """The reply fields for a sketch: how many geometry elements it holds and
    whether its wires are closed (a Pad needs a closed profile)."""
    count = int(sketch.GeometryCount)
    closed = False
    if count:
        wires = sketch.Shape.Wires
        closed = bool(wires) and all(w.isClosed() for w in wires)
    return {"sketch": {"geometry_count": count, "closed": closed}}


# Fillet and Chamfer.

def _dressup_edges(doc: Any, properties: dict[str, Any], current_base: Any, what: str) -> None:
    """Map ``Edges`` (names) beside ``Base`` (an object name) onto the
    ``Base`` link of a PartDesign Fillet or Chamfer, in ``properties``."""
    edges = properties.pop("Edges", None)
    if edges is None:
        return
    base = properties.get("Base")
    if isinstance(base, (list, tuple, dict)):
        names = _link_names(base)
        base = names[0] if names else None
    if base is None and current_base is not None:
        base = current_base.Name
    if not isinstance(edges, (list, tuple)) or not edges:
        raise ValueError('Edges must be a list of edge names: ["Edge1", "Edge2"].')
    if any(isinstance(e, (dict, list, tuple)) for e in edges):
        raise ValueError(
            f"A {what} takes one {DRESSUPS['PartDesign::' + what]} for all its edges: give Edges as names "
            '["Edge1", "Edge2"] and the value beside them. Make a second feature for another size.'
        )
    base_obj = doc.getObject(base) if isinstance(base, str) else None
    if base_obj is None:
        raise ValueError(f"Referenced object '{base}' not found." if base else f"A {what} needs Base, the object whose edges it rounds.")
    count = len(base_obj.Shape.Edges)
    for edge in edges:
        if not (isinstance(edge, str) and re.fullmatch(r"Edge[1-9]\d*", edge)):
            raise ValueError(f"'{edge}' is not an edge name such as Edge1.")
        if int(edge[4:]) > count:
            raise ValueError(
                f"Edge '{edge}' does not exist on '{base_obj.Name}', which has Edge1 to Edge{count}. Call "
                + tool_call("list_subelements", {"doc_name": doc.Name, "obj_name": base_obj.Name, "kind": "edges"})
                + " to see its edges."
            )
    properties["Base"] = [base_obj.Name, list(edges)]


def dressup_edges_text(obj: Any, names: Any) -> list[str]:
    """The edges of a PartDesign Fillet or Chamfer as they are now, for example
    ``["Edge1 r1.5"]`` (``s`` for a chamfer), when ``names`` (the properties
    just set) holds Base, Edges or its size; else ``[]``."""
    size_name = DRESSUPS.get(obj.TypeId)
    if size_name is None or not {"Base", "Edges", size_name} & set(names):
        return []
    base = obj.Base
    if not base or base[0] is None:
        return []
    value = f"{getattr(obj, size_name).Value:.4f}".rstrip("0").rstrip(".")
    letter = "s" if size_name == "Size" else "r"
    return [f"{edge} {letter}{value}" for edge in base[1]]


# Plans.

def plan_create(doc: Any, obj_type: str, properties: dict[str, Any], body_name: str | None) -> Plan:
    """What creating ``obj_type`` needs: its Body, the properties with the
    sketch and Fillet forms resolved, and sketch geometry."""
    sketch = obj_type == SKETCH_TYPE
    feature = is_feature_type(obj_type)
    if body_name is not None and not (sketch or feature):
        raise ValueError(f"body_name is for PartDesign features and sketches, not '{obj_type}'.")
    plan = Plan(properties=dict(properties))
    body = named_body(doc, body_name) if body_name is not None else None
    if sketch:
        plan.body = _plan_sketch(doc, plan, body, None)
    elif feature:
        if obj_type in DRESSUPS:
            _dressup_edges(doc, plan.properties, None, obj_type.split("::")[1])
        if body is not None:
            _check_named_body(body, _link_owners(doc, properties))
        plan.body = body or _body_of_links(doc, obj_type, properties)
    return plan


def plan_update(doc: Any, obj: Any, properties: dict[str, Any]) -> Plan:
    """What updating ``obj`` needs: the sketch and Fillet forms resolved and
    sketch geometry. The object stays where it is."""
    plan = Plan(properties=dict(properties))
    if obj.TypeId == SKETCH_TYPE:
        _plan_sketch(doc, plan, owner_body(doc, obj), obj)
    elif obj.TypeId in DRESSUPS:
        base = obj.Base[0] if obj.Base else None
        _dressup_edges(doc, plan.properties, base, obj.TypeId.split("::")[1])
    return plan


def _plan_sketch(doc: Any, plan: Plan, body: Any, existing: Any) -> Any:
    """Fill ``plan`` for a sketch (a new one when ``existing`` is None) and
    return the Body it goes into."""
    props = plan.properties
    if "Support" in props and not (existing is not None and "Support" in existing.PropertiesList):
        if "AttachmentSupport" in props:
            raise ValueError("Give AttachmentSupport or Support, not both: Support is the older name of AttachmentSupport.")
        props["AttachmentSupport"] = props.pop("Support")
        plan.notes.append("Support is AttachmentSupport in this FreeCAD; it was set as AttachmentSupport.")
    if "Geometry" in props:
        plan.geometry = props.pop("Geometry")
        _parse_geometry(plan.geometry)
    support = props.get("AttachmentSupport")
    if support is None:
        return body
    links, body = _support(doc, support, body)
    props["AttachmentSupport"] = links
    if "MapMode" not in props and (existing is None or existing.MapMode == "Deactivated"):
        props["MapMode"] = "FlatFace"
    return body


def body_fields(body: Any) -> dict[str, Any]:
    tip = getattr(body, "Tip", None)
    return {"body": {"name": body.Name, "tip": tip.Name if tip is not None else None}}
