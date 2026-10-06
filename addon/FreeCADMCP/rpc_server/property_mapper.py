"""Property assignment from JSON-friendly dicts onto FreeCAD document objects."""

# Annotations stay text, so importing this module (serialize does, for the
# quantity helpers) needs nothing of FreeCAD beyond its name.
from __future__ import annotations

import math
from dataclasses import dataclass, field
from typing import Any

import FreeCAD

from rpc_server.agent_log import agent_error
from rpc_server.errors import tool_call


@dataclass
class Object:
    name: str
    type: str | None = None
    analysis: str | None = None
    body: str | None = None
    properties: dict[str, Any] = field(default_factory=dict)


def _to_shape_color(val: Any) -> tuple[float, float, float, float]:
    """Normalise a color to a 4-float RGBA tuple.

    Accepts RGB triples (alpha defaults to 1.0) and RGBA quads, matching what
    FreeCAD's ``ShapeColor`` accepts.
    """
    if not isinstance(val, (list, tuple)) or len(val) not in (3, 4):
        raise ValueError(
            f"ShapeColor must be an RGB or RGBA sequence, got {val!r}."
        )
    r, g, b = (float(val[0]), float(val[1]), float(val[2]))
    a = float(val[3]) if len(val) == 4 else 1.0
    return (r, g, b, a)


_REFERENCE_FORMS = (
    '{"object_name": "Box", "face": "Face1"}, '
    '{"object_name": "Box", "faces": ["Face1", "Face2"]}, '
    '{"object_name": "Box", "edge": "Edge5"}, '
    '["Box", "Face1"] or ["Box", ["Face1", "Face2"]]'
)


def _sub_elements(raw: Any) -> str | list[str] | None:
    """A sub-element name or a list of names, or None when ``raw`` is neither."""
    if isinstance(raw, str) and raw:
        return raw
    if (
        isinstance(raw, (list, tuple))
        and raw
        and all(isinstance(s, str) and s for s in raw)
    ):
        return list(raw)
    return None


def parse_reference_entry(entry: Any) -> tuple[str, str | list[str]]:
    """Normalise a single ``References`` entry to ``(object_name, sub_elements)``.

    ``sub_elements`` is one name or a list of names. Accepts
    ``{"object_name": "Box", "face": "Face1"}``,
    ``{"object_name": "Box", "faces": ["Face1", "Face2"]}``, the same with "edge"
    or "edges",
    ``["Box", "Face1"]`` and ``["Box", ["Face1", "Face2"]]``.
    """
    ref_name = None
    subs = None
    if isinstance(entry, dict):
        ref_name = entry.get("object_name", entry.get("Object"))
        subs = _sub_elements(
            entry.get(
                "faces",
                entry.get("face", entry.get("Face", entry.get("edges", entry.get("edge", entry.get("Edge"))))),
            )
        )
    elif isinstance(entry, (list, tuple)) and len(entry) == 2:
        ref_name = entry[0]
        subs = _sub_elements(entry[1])
    if not isinstance(ref_name, str) or not ref_name or subs is None:
        raise ValueError(
            f"Invalid reference entry {entry!r}; expected {_REFERENCE_FORMS}."
        )
    return ref_name, subs


#: TypeIds of every link kind (App::Link, App::LinkElement, App::LinkGroup and
#: their Python variants) start with this.
_LINK_TYPE_PREFIX = "App::Link"


def reject_app_link(ref_obj: Any, what: str) -> None:
    """Refuse an App::Link where FreeCAD needs the object itself: a FEM
    constraint's References or a Gmsh mesh's Shape given a link crash FreeCAD
    (an access violation) or fail inside it, so the call never reaches it."""
    type_id = getattr(ref_obj, "TypeId", "")
    if not type_id.startswith(_LINK_TYPE_PREFIX):
        return
    try:
        target = ref_obj.getLinkedObject().Name
    except Exception:
        target = None
    if type_id.startswith("App::LinkGroup"):
        use = f"point {what} at the objects it groups"
    elif target and target != ref_obj.Name:
        use = f"point {what} at {target}, the object it links"
    else:
        use = f"point {what} at the object it links"
    raise ValueError(f"{ref_obj.Name} is an {type_id}: {use}.")


def resolve_references(doc: FreeCAD.Document, val: Any) -> list[tuple[Any, Any]]:
    """Resolve a ``References`` list into ``(DocumentObject, sub_elements)`` tuples."""
    refs = []
    for entry in val:
        ref_name, subs = parse_reference_entry(entry)
        ref_obj = doc.getObject(ref_name)
        if ref_obj is None:
            raise ValueError(f"Referenced object '{ref_name}' not found.")
        reject_app_link(ref_obj, "References")
        refs.append((ref_obj, subs))
    return refs


def _link_object(doc: FreeCAD.Document, name: Any) -> Any:
    """The document object called ``name``, or a ValueError."""
    if not isinstance(name, str):
        raise ValueError(f"expected an object name, got {name!r}.")
    ref_obj = doc.getObject(name)
    if ref_obj is None:
        raise ValueError(f"Referenced object '{name}' not found.")
    return ref_obj


def _type_id(obj: FreeCAD.DocumentObject, prop: str) -> str:
    """The FreeCAD type of a property, or "" when it cannot be read."""
    try:
        return obj.getTypeIdOfProperty(prop)
    except Exception:
        return ""


def _is_sub_link(obj: FreeCAD.DocumentObject, prop: str) -> bool:
    """Whether ``prop`` links one object with sub-elements (App::PropertyLinkSub
    and its XLink variants), not a list of such links."""
    type_id = _type_id(obj, prop)
    return type_id.startswith(("App::PropertyLinkSub", "App::PropertyXLinkSub")) and "List" not in type_id


#: TypeIds of the origin features and datums: a link to one of them as a whole
#: needs the sub-element [""], not an empty one.
_DATUM_TYPES = ("App::Line", "App::Plane", "App::Point", "PartDesign::Line", "PartDesign::Plane", "PartDesign::Point")


def _whole_object_link(target: Any) -> Any:
    """The value of a sub-element link to all of ``target``: ``(target, [""])``
    for an origin feature or datum, else ``target`` with no sub-element."""
    if getattr(target, "TypeId", "").startswith(_DATUM_TYPES):
        return (target, [""])
    return target


def _is_whole_object(val: Any) -> bool:
    """Whether ``val`` is ``["Name", ""]`` or ``["Name", [""]]``: a sub-element
    link to the object itself, FreeCAD's own form for it."""
    return (
        isinstance(val, (list, tuple))
        and len(val) == 2
        and isinstance(val[0], str)
        and (val[1] == "" or (isinstance(val[1], (list, tuple)) and list(val[1]) == [""]))
    )


def _link_kind(obj: FreeCAD.DocumentObject, prop: str) -> str:
    """"single" for a link property that takes one object, "list" for one that
    takes several, else "".

    Every ``App::PropertyLink*`` and ``App::PropertyXLink*`` type counts,
    the sub-element ones (``PropertyLinkSub``, ``PropertyLinkSubList``)
    included: they all accept a bare object, which links it with no
    sub-element, and a list of objects for the list types.
    """
    try:
        type_id = obj.getTypeIdOfProperty(prop)
    except Exception:
        return ""
    if type_id.startswith(("App::PropertyLink", "App::PropertyXLink")):
        return "list" if "List" in type_id else "single"
    return ""


def quantity_type() -> Any:
    """FreeCAD's Quantity class, or None where FreeCAD has no Units module."""
    return getattr(getattr(FreeCAD, "Units", None), "Quantity", None)


def quantity_text(value: Any) -> str:
    """A quantity in the units FreeCAD prefers for it, for example "100.00 N",
    the way its property editor shows it."""
    try:
        return str(value.getUserPreferred()[0])
    except Exception:
        return str(value.UserString)


def quantity_values(obj: FreeCAD.DocumentObject, names: Any) -> dict[str, str]:
    """The quantity properties among ``names`` with their current value and
    unit in FreeCAD's preferred units, for example ``{"Force": "100.00 N"}``.
    """
    out = {}
    quantity = quantity_type()
    for name in names:
        if quantity is None or name not in obj.PropertiesList:
            continue
        try:
            value = getattr(obj, name)
        except Exception:
            continue
        if isinstance(value, quantity):
            out[name] = quantity_text(value)
    return out


def adjusted_values(obj: FreeCAD.DocumentObject, given: Any) -> list[dict[str, float | str]]:
    """The length and angle properties among ``given`` (name -> value) that were
    set from a number and now hold another one: FreeCAD clamped or replaced it.
    Each row is ``{"name", "given", "stored"}`` in mm or degrees. Expressions and
    strings with units are not compared."""
    quantity = quantity_type()
    rows = []
    if quantity is None:
        return rows
    for name, value in given.items():
        if isinstance(value, bool) or not isinstance(value, (int, float)) or name not in obj.PropertiesList:
            continue
        try:
            stored = getattr(obj, name)
            if not isinstance(stored, quantity) or str(stored.Unit.Type) not in ("Length", "Angle"):
                continue
            now = float(stored.Value)
        except Exception:
            continue
        if not math.isclose(float(value), now, rel_tol=1e-6, abs_tol=1e-9):
            rows.append({"name": name, "given": value, "stored": now})
    return rows


def _map_text(value: Any) -> Any:
    """A number as the string a FreeCAD string map stores; anything else as is."""
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return str(value)
    return value


def _set_dotted(obj: FreeCAD.DocumentObject, path: str, val: Any) -> None:
    """Set one part of a compound property, such as ``Placement.Base.z``.

    A string starting with "=" binds an expression to the path ("=" alone
    removes it). A number is set by reading the property, changing the part
    and assigning the property back, since Placement, Vector and Rotation
    values are copies. ``Rotation.Angle`` is in degrees, as in a Placement
    given as a whole.
    """
    if isinstance(val, str) and val.startswith("="):
        obj.setExpression(path, val[1:].strip() or None)
        return
    names = path.split(".")
    values = [obj]
    for name in names[:-1]:
        values.append(getattr(values[-1], name))
    leaf = names[-1]
    new = val
    if isinstance(values[-1], FreeCAD.Rotation) and leaf == "Angle":
        # The attribute is in radians; replace the rotation itself instead.
        new = FreeCAD.Rotation(values[-1].Axis, float(val))
        values.pop()
        names.pop()
        leaf = names[-1]
    for i in range(len(values) - 1, -1, -1):
        setattr(values[i], leaf, new)
        if i == 0:
            break
        new = values[i]
        leaf = names[i - 1]


FILLET_TYPES = {"Part::Fillet": "Radius", "Part::Chamfer": "Size"}


def _has_fillet_edges(obj: FreeCAD.DocumentObject) -> bool:
    return _type_id(obj, "Edges") == "Part::PropertyFilletEdges"


def _edge_size(value: Any, what: str) -> float:
    if isinstance(value, str) and value.startswith("="):
        raise ValueError(
            f"{what} cannot be an expression: FreeCAD does not apply an expression to the "
            "size of a Part::Fillet or Part::Chamfer edge. Give a number, and set it again "
            "with update_object when the value it follows changes."
        )
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0:
        raise ValueError(f"{what} must be a number above 0, not {value!r}.")
    return float(value)


def fillet_edge_entries(obj: FreeCAD.DocumentObject, val: Any, default: Any) -> list[tuple[int, float, float]]:
    """The ``(edge id, size 1, size 2)`` entries of a Part::Fillet or Part::Chamfer
    ``Edges`` value, and the edge names checked against the Base shape.

    ``val`` is a list whose entries are an edge name (``"Edge3"``, sized by
    ``default``, the Radius or Size given beside it), ``{"edge": "Edge3",
    "radius": 2}`` (``"size"`` for a chamfer; ``"radius2"``/``"size2"`` for a
    second size), or FreeCAD's own ``[3, 2, 2]``.
    """
    if not isinstance(val, (list, tuple)) or not val:
        raise ValueError(
            'Edges must be a list: ["Edge1", "Edge2"] with a Radius (Chamfer: Size), '
            'or [{"edge": "Edge1", "radius": 2}].'
        )
    word = "Size" if obj.TypeId == "Part::Chamfer" else "Radius"
    base_edges = None
    base = getattr(obj, "Base", None)
    try:
        base_edges = len(base.Shape.Edges) if base is not None else None
    except Exception:
        pass
    out = []
    for entry in val:
        if isinstance(entry, str):
            name, first, second = entry, default, default
        elif isinstance(entry, dict):
            name = entry.get("edge")
            first = entry.get("radius", entry.get("size", default))
            second = entry.get("radius2", entry.get("size2", first))
        elif isinstance(entry, (list, tuple)) and len(entry) == 3 and isinstance(entry[0], int):
            name, first, second = f"Edge{entry[0]}", entry[1], entry[2]
        else:
            raise ValueError(f"Invalid edge entry {entry!r}; expected an edge name or {{\"edge\": \"Edge1\", \"{word.lower()}\": 2}}.")
        if not (isinstance(name, str) and name.startswith("Edge") and name[4:].isdigit() and int(name[4:]) > 0):
            raise ValueError(f"'{name}' is not an edge name such as Edge1.")
        number = int(name[4:])
        if base_edges is not None and number > base_edges:
            raise ValueError(
                f"Edge '{name}' does not exist on '{base.Name}', which has Edge1 to Edge{base_edges}. "
                f"Call {tool_call('list_subelements', {'doc_name': obj.Document.Name, 'obj_name': base.Name, 'kind': 'edges'})} "
                "to see its edges."
            )
        if first is None:
            raise ValueError(f"Give the {word} for {name}: beside Edges, or as \"{word.lower()}\" in its entry.")
        out.append((number, _edge_size(first, word), _edge_size(second, word)))
    return out


def fillet_edges_text(obj: FreeCAD.DocumentObject, names: Any) -> list[str]:
    """The edges of a Part::Fillet or Part::Chamfer as they are now, for example
    ``["Edge1 r4", "Edge3 r2 to r4"]`` (``s`` for a chamfer), when ``names`` (the
    properties just set) holds the Edges or the Radius or Size; else ``[]``."""
    if not _has_fillet_edges(obj) or not {"Edges", "Radius", "Size"} & set(names):
        return []
    letter = "s" if obj.TypeId == "Part::Chamfer" else "r"

    def number(value: float) -> str:
        return f"{value:.4f}".rstrip("0").rstrip(".")

    out = []
    for edge, first, second in obj.Edges:
        size = number(first) if abs(first - second) < 1e-9 else f"{number(first)} to {letter}{number(second)}"
        out.append(f"Edge{edge} {letter}{size}")
    return out


def _take_fillet_edges(obj: FreeCAD.DocumentObject, properties: dict[str, Any]):
    """Split the Edges of a Part::Fillet or Part::Chamfer, and the Radius or Size
    that goes with it, out of ``properties``; ``(rest, edges value, default)``.
    Both names are no property of the object, so they never reach setattr."""
    if not _has_fillet_edges(obj):
        return properties, None, None
    rest = dict(properties)
    edges = rest.pop("Edges", None)
    default = None
    for key in ("Radius", "Size"):
        if key in rest and key not in obj.PropertiesList:
            default = rest.pop(key)
    return rest, edges, default


#: Types that take Base and Tool, and the types that take a list of Shapes
#: instead: the property names get swapped for the other kind.
_TWO_INPUT_TYPES = ("Part::Common", "Part::Cut", "Part::Fuse")
_MULTI_TYPES = ("Part::MultiCommon", "Part::MultiFuse")


def _edit_distance(a: str, b: str) -> int:
    """The edit distance between a and b: insertions, deletions, substitutions
    and swaps of two neighbouring letters (a typo such as Lenght) count one each."""
    rows = [list(range(len(b) + 1))]
    for i, ca in enumerate(a, 1):
        row = [i]
        for j, cb in enumerate(b, 1):
            best = min(rows[i - 1][j] + 1, row[j - 1] + 1, rows[i - 1][j - 1] + (ca != cb))
            if i > 1 and j > 1 and ca == b[j - 2] and a[i - 2] == cb:
                best = min(best, rows[i - 2][j - 2] + 1)
            row.append(best)
        rows.append(row)
    return rows[-1][-1]


def _own_property_names(obj: FreeCAD.DocumentObject) -> list[str]:
    """The properties of ``obj`` that are not hidden."""
    names = []
    for name in obj.PropertiesList:
        try:
            if "Hidden" in obj.getPropertyStatus(name):
                continue
        except Exception:
            pass
        names.append(name)
    return names


def unknown_property_message(obj: FreeCAD.DocumentObject, prop: str) -> str:
    """What to tell a caller who set a property ``obj`` does not have: its
    nearest properties by edit distance (at most 3, the object's own and not
    hidden), and for the boolean types that take Base and Tool or a list of
    Shapes, the type that has the name."""
    type_id = getattr(obj, "TypeId", "")
    text = f"{type_id} '{obj.Name}' has no property '{prop}'."
    wanted = prop.lower()
    ranked = sorted((_edit_distance(wanted, name.lower()), name) for name in _own_property_names(obj))
    limit = min(3, max(1, len(prop) // 3))
    near = [name for distance, name in ranked if distance <= limit][:3]
    if prop == "Shapes" and type_id in _TWO_INPUT_TYPES:
        return text + f" {type_id} takes Base and Tool; Part::MultiCommon and Part::MultiFuse take Shapes."
    if prop in ("Base", "Tool") and type_id in _MULTI_TYPES:
        return text + f" {type_id} takes Shapes, a list of objects; Part::Common, Part::Cut and Part::Fuse take Base and Tool."
    if near:
        return text + " Nearest properties: " + ", ".join(near) + "."
    return text + " get_object lists its properties."


def set_object_property(
    doc: FreeCAD.Document, obj: FreeCAD.DocumentObject, properties: dict[str, Any]
):
    failures = []
    properties, fillet_edges, fillet_default = _take_fillet_edges(obj, properties)
    fillet_active = fillet_edges is not None or fillet_default is not None
    if fillet_active:
        # Last, so Base is set when the edges are checked against it.
        properties = {**properties, "Edges": None}
    for prop, val in properties.items():
        try:
            if prop == "Edges" and fillet_active:
                if fillet_edges is None:
                    # A new size alone resizes every edge already listed.
                    size = _edge_size(fillet_default, "Radius" if obj.TypeId == "Part::Fillet" else "Size")
                    obj.Edges = [(edge[0], size, size) for edge in obj.Edges]
                else:
                    obj.Edges = fillet_edge_entries(obj, fillet_edges, fillet_default)
                continue
            if "." in prop and prop.split(".", 1)[0] in obj.PropertiesList:
                _set_dotted(obj, prop, val)
            elif prop in obj.PropertiesList:
                if prop == "Placement" and isinstance(val, dict):
                    if "Base" in val:
                        pos = val["Base"]
                    elif "Position" in val:
                        pos = val["Position"]
                    else:
                        pos = {}
                    rot = val.get("Rotation", {})
                    # Rotation(axis, angle) takes the angle in degrees, the unit
                    # serialize_value reports it in.
                    placement = FreeCAD.Placement(
                        FreeCAD.Vector(
                            pos.get("x", 0),
                            pos.get("y", 0),
                            pos.get("z", 0),
                        ),
                        FreeCAD.Rotation(
                            FreeCAD.Vector(
                                rot.get("Axis", {}).get("x", 0),
                                rot.get("Axis", {}).get("y", 0),
                                rot.get("Axis", {}).get("z", 1),
                            ),
                            rot.get("Angle", 0),
                        ),
                    )
                    setattr(obj, prop, placement)

                elif isinstance(getattr(obj, prop), FreeCAD.Vector) and isinstance(
                    val, dict
                ):
                    vector = FreeCAD.Vector(
                        val.get("x", 0), val.get("y", 0), val.get("z", 0)
                    )
                    setattr(obj, prop, vector)

                elif isinstance(val, dict) and _type_id(obj, prop) == "App::PropertyMap":
                    # A string map, such as a FEM material: numbers become the
                    # strings the map stores.
                    setattr(obj, prop, {str(k): _map_text(v) for k, v in val.items()})

                elif isinstance(val, str) and val.startswith("="):
                    # An expression, as in the spreadsheet tool; a bare "="
                    # removes the binding.
                    obj.setExpression(prop, val[1:].strip() or None)

                elif prop == "References":
                    # Always a list of entries: anything else is an error, never
                    # a link to the whole object.
                    if val is None:
                        # null clears the property, as before.
                        setattr(obj, prop, None)
                    elif isinstance(val, (list, tuple)):
                        setattr(obj, prop, resolve_references(doc, list(val)))
                    else:
                        raise ValueError(
                            f"References must be a list; each entry is {_REFERENCE_FORMS}."
                        )

                elif _is_sub_link(obj, prop) and isinstance(val, str):
                    # A bare object name is the whole object: an origin feature
                    # or datum is stored as (object, [""]), the form a pattern's
                    # Direction "X_Axis" computes in; any other object as is.
                    target = _link_object(doc, val)
                    reject_app_link(target, prop)
                    setattr(obj, prop, _whole_object_link(target))

                elif _is_sub_link(obj, prop) and isinstance(val, (list, tuple, dict)):
                    # One sub-element link (a force's Direction): the forms
                    # References takes for one sub-element, and ["Name", ""] or
                    # ["Name", [""]] for the whole object, as a bare name.
                    if _is_whole_object(val):
                        target = _link_object(doc, val[0])
                        reject_app_link(target, prop)
                        setattr(obj, prop, _whole_object_link(target))
                        continue
                    try:
                        ref_name, subs = parse_reference_entry(val)
                    except ValueError as e:
                        raise ValueError(
                            f"{str(e).rstrip('.')}; or the bare object name for a whole object, "
                            'such as an origin axis "X_Axis".'
                        ) from None
                    target = _link_object(doc, ref_name)
                    reject_app_link(target, prop)
                    setattr(obj, prop, (target, [subs] if isinstance(subs, str) else subs))

                elif isinstance(val, str) and _link_kind(obj, prop) == "single":
                    target = _link_object(doc, val)
                    if prop in ("Shape", "Part") and {"CharacteristicLengthMax", "MaxSize"} & set(obj.PropertiesList):
                        reject_app_link(target, "Shape")
                    setattr(obj, prop, target)

                elif _link_kind(obj, prop) == "list" and (
                    isinstance(val, str)
                    or (
                        isinstance(val, (list, tuple))
                        and all(isinstance(n, str) for n in val)
                    )
                ):
                    names = [val] if isinstance(val, str) else val
                    setattr(obj, prop, [_link_object(doc, n) for n in names])

                else:
                    setattr(obj, prop, val)
            # ShapeColor is a property of the ViewObject
            elif prop == "ShapeColor" and isinstance(val, (list, tuple)):
                setattr(obj.ViewObject, prop, _to_shape_color(val))

            elif prop == "ViewObject" and isinstance(val, dict):
                for k, v in val.items():
                    if k == "ShapeColor":
                        setattr(obj.ViewObject, k, _to_shape_color(v))
                    else:
                        setattr(obj.ViewObject, k, v)

            else:
                setattr(obj, prop, val)

        except Exception as e:
            if isinstance(e, AttributeError) and prop.split(".")[0] not in obj.PropertiesList and prop not in ("ShapeColor", "ViewObject"):
                e = ValueError(unknown_property_message(obj, prop.split(".")[0]))
            agent_error(f"Property '{prop}' assignment error: {e}\n")
            failures.append(f"{prop}: {e}")

    if failures:
        raise ValueError(
            "Failed to set propert" + ("y" if len(failures) == 1 else "ies")
            + ": " + "; ".join(failures)
        )
