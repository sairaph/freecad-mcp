"""Object creation and property updates for the RPC ``create_object`` and
``edit_object`` handlers.

``create_object_gui`` selects one helper per kind of object:
``_create_fem_mesh`` for a Gmsh FEM mesh (accepting the older parameter
names), ``_create_fem_object`` for the other ``Fem::`` types,
``_create_python_object`` for the Python-implemented types that have a
factory here (``Part::Tube`` and the Draft shapes), and
``_create_generic_object`` for any type registered with ``doc.addObject``;
PartDesign features and sketches for a Body are made in their Body there
(``partdesign.plan_create`` chooses it).
``edit_object_gui`` sets properties on an existing object. Both check the
object after recompute and report the actual object name.
"""

import math
import os
import re
from typing import Any

import FreeCAD
import ObjectsFem

from rpc_server.agent_log import agent_error, agent_warning
from rpc_server.empty_results import empty_result
from rpc_server.fem_mesh import changes_meshing, generate_mesh, is_gmsh, mesh_info
from rpc_server.fem_loads import load_info, material_info
from rpc_server import partdesign
from rpc_server.property_mapper import FILLET_TYPES, Object, adjusted_values, fillet_edges_text, quantity_values, reject_app_link, set_object_property
from rpc_server.object_validation import failed_names, newly_failed_report, object_validity_error, stale_dependents
from rpc_server.serialize import object_shape_span, object_shape_summary
from rpc_server.shape_changes import changed_shapes, remember_summaries, snapshot
from rpc_server.source_visibility import hide_sources, newly_hidden, visibility_snapshot
from rpc_server.transactions import active_document, transaction


def _create_fem_mesh(doc: FreeCAD.Document, obj: Object):
    """Create a ``Fem::FemMeshGmsh`` and run Gmsh to populate it.

    Accepts both the FreeCAD 0.x and 1.x property names (``Part``/``Shape``,
    ``ElementSize{Max,Min}``/``CharacteristicLength{Max,Min}``).
    Returns the created mesh object.
    """
    res = getattr(doc, obj.analysis).addObject(
        ObjectsFem.makeMeshGmsh(doc, obj.name)
    )[0]
    geom_attr = "Shape" if hasattr(res, "Shape") else ("Part" if hasattr(res, "Part") else None)
    legacy_to_new = {
        "Part": geom_attr,
        "ElementSizeMax": "CharacteristicLengthMax",
        "ElementSizeMin": "CharacteristicLengthMin",
    }
    geom_key = "Part" if "Part" in obj.properties else ("Shape" if "Shape" in obj.properties else None)
    if geom_key is None:
        raise ValueError("'Part' (or 'Shape') property not found in properties.")
    target_obj = doc.getObject(obj.properties[geom_key])
    if target_obj is None:
        raise ValueError(f"Referenced object '{obj.properties[geom_key]}' not found.")
    if geom_attr is None:
        raise ValueError("Mesh object has neither 'Shape' nor 'Part' property.")
    reject_app_link(target_obj, "Shape")
    setattr(res, geom_attr, target_obj)
    del obj.properties[geom_key]

    # The remaining properties follow the same value rules as every other type
    # (expressions, links, units); a legacy name is renamed only when the mesh
    # has the new one, and a name the mesh lacks fails like anywhere else.
    properties = {}
    for param, value in obj.properties.items():
        target = legacy_to_new.get(param) or param
        if target != param and target not in res.PropertiesList:
            target = param
        properties[target] = value
    set_object_property(doc, res, properties)
    doc.recompute()

    if "ElementOrder" not in obj.properties and "ElementOrder" in res.PropertiesList:
        # FreeCAD's own command meshes second order: linear tetrahedra lock in
        # bending and give results about three times too stiff.
        res.ElementOrder = "2nd"
    generate_mesh(res)
    FreeCAD.Console.PrintMessage(
        f"FEM Mesh '{res.Name}' generated successfully in '{doc.Name}'.\n"
    )
    return res


def _create_fem_object(doc: FreeCAD.Document, obj: Object):
    """Create a ``Fem::*`` object via the appropriate ``ObjectsFem.makeXxx`` factory."""
    fem_make_methods = {
        "MaterialCommon": ObjectsFem.makeMaterialSolid,
        "AnalysisPython": ObjectsFem.makeAnalysis,
    }
    obj_type_short = obj.type.split("::")[1]
    method_name = "make" + obj_type_short
    make_method = fem_make_methods.get(obj_type_short, getattr(ObjectsFem, method_name, None))

    if not callable(make_method):
        raise ValueError(f"No creation method '{method_name}' found in ObjectsFem.")

    res = make_method(doc, obj.name)
    set_object_property(doc, res, obj.properties)
    FreeCAD.Console.PrintMessage(
        f"FEM object '{res.Name}' created with '{method_name}'.\n"
    )
    if obj.type != "Fem::AnalysisPython" and obj.analysis:
        getattr(doc, obj.analysis).addObject(res)
    return res


def _require(properties: dict, key: str, obj_type: str):
    """Pop a property the factory needs as a constructor argument.

    Popping keeps it out of the later ``set_object_property`` pass, which would
    otherwise try to re-assign the raw JSON value.
    """
    if key not in properties:
        raise ValueError(
            f"'{obj_type}' requires a '{key}' property, which was not supplied."
        )
    return properties.pop(key)


def _to_vectors(raw, obj_type: str) -> list:
    """Convert a JSON point list to FreeCAD.Vector, accepting dicts or sequences."""
    if not isinstance(raw, (list, tuple)) or not raw:
        raise ValueError(f"'{obj_type}' needs 'Points' to be a non-empty list.")
    points = []
    for entry in raw:
        if isinstance(entry, dict):
            points.append(
                FreeCAD.Vector(entry.get("x", 0), entry.get("y", 0), entry.get("z", 0))
            )
        elif isinstance(entry, (list, tuple)) and len(entry) in (2, 3):
            x, y, z = (list(entry) + [0])[:3]
            points.append(FreeCAD.Vector(x, y, z))
        else:
            raise ValueError(
                f"Invalid point {entry!r} for '{obj_type}'; expected "
                "{'x': .., 'y': .., 'z': ..} or [x, y, z]."
            )
    return points


def _make_tube(doc, name, properties):
    from BasicShapes import Shapes

    return Shapes.addTube(doc, name)


def _make_draft_circle(doc, name, properties):
    import Draft

    return Draft.make_circle(_require(properties, "Radius", "Draft::Circle"))


def _make_draft_rectangle(doc, name, properties):
    import Draft

    return Draft.make_rectangle(
        _require(properties, "Length", "Draft::Rectangle"),
        _require(properties, "Height", "Draft::Rectangle"),
    )


def _make_draft_polygon(doc, name, properties):
    import Draft

    return Draft.make_polygon(
        _require(properties, "FacesNumber", "Draft::Polygon"),
        _require(properties, "Radius", "Draft::Polygon"),
    )


def _make_draft_wire(doc, name, properties):
    import Draft

    points = _to_vectors(_require(properties, "Points", "Draft::Wire"), "Draft::Wire")
    return Draft.make_wire(points, closed=bool(properties.pop("Closed", False)))


#: Where a default font is looked for when the Draft preference names none:
#: bold sans first, so text prints with strokes that hold.
_FONT_FALLBACKS = (
    "C:/Windows/Fonts/arialbd.ttf",
    "C:/Windows/Fonts/arial.ttf",
    "/System/Library/Fonts/Supplemental/Arial Bold.ttf",
    "/System/Library/Fonts/Supplemental/Arial.ttf",
    "/Library/Fonts/Arial.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
    "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    "/usr/share/fonts/TTF/DejaVuSans-Bold.ttf",
    "/usr/share/fonts/dejavu/DejaVuSans-Bold.ttf",
)


def _default_font() -> str | None:
    """The font file for a ShapeString given none: the Draft preference
    ``FontFile`` when it names an existing file, else the first of
    ``_FONT_FALLBACKS`` that exists, else None."""
    preferred = FreeCAD.ParamGet("User parameter:BaseApp/Preferences/Mod/Draft").GetString("FontFile", "")
    if preferred and os.path.isfile(preferred):
        return preferred
    return next((path for path in _FONT_FALLBACKS if os.path.isfile(path)), None)


def _number(value: Any, what: str, rule: str, valid=lambda v: True) -> Any:
    """``value`` when it is a finite number (not a bool) that ``valid`` accepts,
    else a ValueError saying it must be ``rule``."""
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or not valid(value):
        raise ValueError(f"{what} must be {rule}, not {value!r}.")
    return value


def _count(value: Any, what: str) -> int:
    return _number(value, what, "a whole number of 1 or more", lambda v: isinstance(v, int) and v >= 1)


def _vector(raw: Any, what: str) -> Any:
    """A FreeCAD.Vector from {"x", "y", "z"} or [x, y, z] (z may be left out)."""
    if isinstance(raw, dict):
        parts = [raw.get("x", 0), raw.get("y", 0), raw.get("z", 0)]
    elif isinstance(raw, (list, tuple)) and len(raw) in (2, 3):
        parts = (list(raw) + [0])[:3]
    else:
        raise ValueError(f"{what} must be {{'x': .., 'y': .., 'z': ..}} or [x, y, z], not {raw!r}.")
    if any(isinstance(p, bool) or not isinstance(p, (int, float)) for p in parts):
        raise ValueError(f"{what} must hold numbers, not {raw!r}.")
    return FreeCAD.Vector(*parts)


def _interval(raw: Any, axis: int, what: str) -> Any:
    """The step of an array along one axis: a number is the spacing along that
    axis, a vector is the step itself."""
    if isinstance(raw, (int, float)) and not isinstance(raw, bool):
        step = [0.0, 0.0, 0.0]
        step[axis] = raw
        return FreeCAD.Vector(*step)
    return _vector(raw, what)


def _array_base(doc: FreeCAD.Document, properties: dict, obj_type: str) -> Any:
    name = _require(properties, "Base", obj_type)
    base = doc.getObject(name) if isinstance(name, str) else None
    if base is None:
        raise ValueError(f"Referenced object '{name}' not found." if isinstance(name, str) else f"Base must be an object name, not {name!r}.")
    return base


def _make_draft_shapestring(doc, name, properties):
    import Draft

    text = _require(properties, "String", "Draft::ShapeString")
    if not isinstance(text, str) or not text:
        raise ValueError(f"String must be the non-empty text to draw, not {text!r}.")
    size = _number(properties.pop("Size", 10), "Size", "a number above 0", lambda v: v > 0)
    font = properties.pop("FontFile", None)
    if font:
        if not os.path.isfile(font):
            raise ValueError(f"FontFile '{font}' does not exist on the computer running FreeCAD.")
    else:
        font = _default_font()
        if font is None:
            raise ValueError(
                "Draft::ShapeString needs FontFile, the path of a .ttf or .otf font on the computer "
                "running FreeCAD; no default font was found."
            )
    return Draft.make_shapestring(text, font, size)


def _make_draft_ortho_array(doc, name, properties):
    import Draft

    base = _array_base(doc, properties, "Draft::OrthoArray")
    counts = [_count(properties.pop(f"Number{axis}", 1), f"Number{axis}") for axis in "XYZ"]
    steps = [_interval(properties.pop(f"Interval{axis}", 10), index, f"Interval{axis}") for index, axis in enumerate("XYZ")]
    return Draft.make_ortho_array(base, steps[0], steps[1], steps[2], counts[0], counts[1], counts[2], use_link=False)


def _make_draft_polar_array(doc, name, properties):
    import Draft

    base = _array_base(doc, properties, "Draft::PolarArray")
    number = _count(_require(properties, "NumberPolar", "Draft::PolarArray"), "NumberPolar")
    angle = _number(properties.pop("Angle", 360), "Angle", "a number of degrees")
    center = _vector(properties.pop("Center", [0, 0, 0]), "Center")
    return Draft.make_polar_array(base, number, angle, center, use_link=False)


#: Types implemented in Python rather than C++, so absent from FreeCAD's type
#: registry and rejected by ``doc.addObject``. Each entry adapts the real
#: factory to a uniform ``(doc, name, properties) -> DocumentObject`` call.
#: The signatures genuinely differ: addTube takes the document and name,
#: while the Draft factories take geometry and neither, so this cannot
#: collapse into a (module, function) lookup the way the Fem:: branch does.
_PYTHON_FACTORIES = {
    "Part::Tube": _make_tube,
    "Draft::Circle": _make_draft_circle,
    "Draft::Rectangle": _make_draft_rectangle,
    "Draft::Polygon": _make_draft_polygon,
    "Draft::Wire": _make_draft_wire,
    "Draft::ShapeString": _make_draft_shapestring,
    "Draft::OrthoArray": _make_draft_ortho_array,
    "Draft::PolarArray": _make_draft_polar_array,
}


#: Draft types that are arrays of a solid, not flat profiles.
_DRAFT_ARRAYS = ("Draft::OrthoArray", "Draft::PolarArray")


def _create_python_object(doc: FreeCAD.Document, obj: Object):
    """Create a Python-implemented feature via its factory."""
    with active_document(doc):
        res = _PYTHON_FACTORIES[obj.type](doc, obj.name, obj.properties)
    if res is None:
        raise ValueError(f"The factory for '{obj.type}' returned no object.")
    # Only addTube honours a requested name; the Draft factories name objects
    # themselves (make_wire even yields "Line"), so carry the caller's name on
    # the Label and let the real Name go back in the response.
    res.Label = obj.name
    set_object_property(doc, res, obj.properties)
    FreeCAD.Console.PrintMessage(
        f"{obj.type} '{res.Name}' created in '{doc.Name}' via RPC.\n"
    )
    return res


#: Python-implemented features with no factory here yet. Keys must stay
#: disjoint from _PYTHON_FACTORIES, which handles its own types before
#: _create_generic_object is ever reached.
_UNREGISTERED_TYPE_HINTS = {
    "Draft::Point": "Draft.make_point(x, y, z)",
    "Draft::Ellipse": "Draft.make_ellipse(majradius, minradius)",
    "Draft::BSpline": "Draft.make_bspline(points, closed=False)",
}


def _unregistered_type_message(obj_type: str) -> str:
    """Explain an ``addObject`` type-registry miss in terms the caller can act on.

    FreeCAD's own error ("is not a document object type") gives no hint that
    the type may be perfectly real but Python-implemented, which sends callers
    hunting for a typo that isn't there.
    """
    hint = _UNREGISTERED_TYPE_HINTS.get(obj_type)
    if hint is None and obj_type.startswith("Draft::"):
        hint = "the matching Draft.make_* factory"
    suggestion = (
        f" It is implemented in Python rather than C++, so build it with "
        f"execute_code instead, e.g. {hint}."
        if hint
        else " Check the spelling, or build it with execute_code."
    )
    return (
        f"'{obj_type}' is not a type registered with FreeCAD, so create_object "
        f"cannot make it.{suggestion}"
    )


def _create_generic_object(doc: FreeCAD.Document, obj: Object, plan: partdesign.Plan):
    if obj.type in FILLET_TYPES and not ("Base" in obj.properties and "Edges" in obj.properties):
        # Without edges FreeCAD cannot compute it ("no suitable edges"), so an object
        # made bare would only stay behind broken.
        size = FILLET_TYPES[obj.type]
        raise ValueError(
            f"{obj.type} needs Base and Edges in obj_properties, for example "
            f'{{"Base": "Box", "Edges": ["Edge1", "Edge2"], "{size}": 1}}; '
            "call list_subelements with kind edges to see the edge names."
        )
    if obj.type in partdesign.DRESSUPS and not (
        isinstance(plan.properties.get("Base"), (list, tuple, dict)) or plan.properties.get("UseAllEdges")
    ):
        raise ValueError(
            f"{obj.type} needs Base and Edges in obj_properties, for example "
            f'{{"Base": "Pad", "Edges": ["Edge1", "Edge2"], "{partdesign.DRESSUPS[obj.type]}": 1}}; '
            "call list_subelements with kind edges to see the edge names."
        )
    properties = plan.properties
    try:
        if plan.body is not None and obj.type in partdesign.TRANSFORMED:
            # A pattern joins its Body after its Originals are set: FreeCAD
            # makes a Transformed feature the Tip only when it has them as it
            # is added, and newObject adds it first.
            res = doc.addObject(obj.type, obj.name)
            if "Originals" in properties:
                set_object_property(doc, res, {"Originals": properties["Originals"]})
                properties = {key: value for key, value in properties.items() if key != "Originals"}
            plan.body.addObject(res)
        else:
            # A feature goes in its Body the way FreeCAD's own commands put it
            # there, which also sets BaseFeature and Tip.
            res = plan.body.newObject(obj.type, obj.name) if plan.body is not None else doc.addObject(obj.type, obj.name)
    except Exception as e:
        if "not a document object type" in str(e):
            raise ValueError(_unregistered_type_message(obj.type)) from e
        raise
    set_object_property(doc, res, properties)
    if plan.geometry is not None:
        partdesign.set_geometry(res, plan.geometry)
    FreeCAD.Console.PrintMessage(
        f"{res.TypeId} '{res.Name}' added to '{doc.Name}' via RPC.\n"
    )
    return res


def _creation_failure(doc: FreeCAD.Document, tx, existing: set, error: Exception) -> str:
    """Undo a creation that raised and return the error text for the caller.

    When this call opened the transaction it is aborted, which removes
    everything created since. When it joined an open transaction (aborting
    would discard the user's own pending edit) the objects created since the
    call started are removed by hand instead.
    """
    leftover = []
    if tx.opened:
        try:
            tx.abort()
        except Exception as e:
            agent_warning(
                f"MCP RPC: could not abort transaction '{tx.name}': {type(e).__name__}: {e}\n"
            )
    for created in [o for o in doc.Objects if o.Name not in existing]:
        try:
            doc.removeObject(created.Name)
        except Exception:
            leftover.append(created.Name)
    message = str(error).strip().rstrip(".") or type(error).__name__
    if leftover:
        return (
            f"{message}. Creation failed, but {', '.join(leftover)} could not be "
            "removed; delete it with delete_object."
        )
    return f"{message}. Nothing was created."


def _keep_requested_label(created: Any, requested: str) -> None:
    """Give ``created`` the Label that was asked for when FreeCAD only changed
    the characters its names cannot hold (a space becomes an underscore): the
    Name has to be sanitised, the Label does not, and a 3MF or STEP file carries
    the Label. A rename for another reason (Box taken, so Box001) is left alone."""
    try:
        if (
            requested
            and requested != created.Name
            and created.Label == created.Name
            and re.sub(r"[\W_]", "", requested) == re.sub(r"[\W_]", "", created.Name)
        ):
            created.Label = requested
    except Exception:
        pass


def _name_and_placement_fields(obj: Any, requested: list) -> dict[str, Any]:
    """The reply fields that show what the caller cannot see otherwise: the
    Label when it differs from the Name, the Placement now when one of the
    properties set was (part of) it, and the edges of a fillet or chamfer."""
    fields: dict[str, Any] = {}
    try:
        edges = fillet_edges_text(obj, requested) or partdesign.dressup_edges_text(obj, requested)
        if edges:
            fields["edges"] = edges
    except Exception:
        pass
    try:
        if obj.Label != obj.Name:
            fields["label"] = obj.Label
        if any(key == "Placement" or key.startswith("Placement.") for key in requested):
            place = obj.Placement
            base, rotation = place.Base, place.Rotation

            def n(value: float) -> str:
                # Fixed decimals, not %g, which drops digits of a large value.
                text = f"{value:.4f}".rstrip("0").rstrip(".")
                return "0" if text in ("-0", "") else text

            text = f"Base ({n(base.x)}, {n(base.y)}, {n(base.z)})"
            angle = math.degrees(rotation.Angle)
            if abs(angle) > 1e-9:
                axis = rotation.Axis
                text += f", rotated {n(angle)} deg about ({n(axis.x)}, {n(axis.y)}, {n(axis.z)})"
            fields["placement"] = text
    except Exception:
        pass
    return fields


def _shape_fields(obj: Any) -> dict[str, Any]:
    """The reply fields for an object with a Shape: what the shape holds, and a
    warning when a boolean or a solid-making feature holds no solid although
    one of its inputs does, or a Cut removed nothing. A container with no
    feature in it yet (an empty PartDesign::Body) has a null shape and nothing
    to report."""
    if getattr(obj, "Shape", None) is None:
        return {}
    if obj.TypeId == partdesign.SKETCH_TYPE and getattr(obj, "GeometryCount", None) == 0:
        # "no geometry yet" is in the sketch fields; a null shape here reads as a failure.
        return {}
    summary = object_shape_summary(obj)
    # The Body this object is the Tip of has the same shape, so the same summary.
    twins = _reading("the Body of a Tip", lambda: [obj.Document.getObject(name) for name in _own_names(obj.Document, obj) if name != str(obj.Name)], [])
    remember_summaries([(item, summary) for item in [obj, *twins] if item is not None])
    if summary is None:
        return {}
    if summary.get("null") and hasattr(obj, "Group") and not obj.Group:
        return {}
    fields: dict[str, Any] = {"shape": summary}
    span = _reading("where the shape lies", lambda: object_shape_span(obj), None) if summary.get("size") else None
    if span:
        # Kept out of the remembered summary: shape_changes compares those.
        fields["shape"] = {**summary, "span": span}
    found = empty_result(obj, summary.get("volume"))
    if found is not None:
        fields["warning"] = found[0]
    return fields


def _reading(what: str, read, default):
    """``read()``, or ``default`` when it raises: the reply fields are extra
    information, and a reader that fails (an odd object, a FreeCAD error) logs
    a warning and drops its field instead of failing the user's change."""
    try:
        return read()
    except Exception as e:
        agent_warning(f"MCP RPC: could not read {what}: {type(e).__name__}: {e}\n")
        return default


def _partdesign_fields(created: Any, plan: partdesign.Plan) -> dict[str, Any]:
    """The reply fields of a PartDesign call: the Body a new object went into
    and its Tip, what a sketch holds, and the notes about forms that were mapped."""
    fields: dict[str, Any] = {}
    try:
        if plan.body is not None:
            fields.update(partdesign.body_fields(plan.body))
        if created.TypeId == partdesign.SKETCH_TYPE:
            fields.update(partdesign.sketch_fields(created))
    except Exception as e:
        agent_warning(f"MCP RPC: could not read the PartDesign state: {type(e).__name__}: {e}\n")
    if plan.notes:
        fields["notes"] = plan.notes
    return fields


def _own_names(doc: FreeCAD.Document, obj: Any) -> set[str]:
    """The objects whose shape the reply of a call about ``obj`` already gives:
    ``obj`` itself, and the Body it is the Tip of (the same shape)."""
    names = {str(obj.Name)}
    try:
        body = partdesign.owner_body(doc, obj)
        if body is not None and getattr(body, "Tip", None) is not None and body.Tip.Name == obj.Name:
            names.add(str(body.Name))
    except Exception:
        pass
    return names


def shown_before(doc: FreeCAD.Document) -> dict[str, bool]:
    """``visibility_snapshot`` of the document before a change, or {} when it
    could not be taken: the report of what went hidden must never fail the
    user's change."""
    try:
        return visibility_snapshot(doc)
    except Exception as e:
        agent_warning(f"MCP RPC: could not read which objects are shown: {type(e).__name__}: {e}\n")
        return {}


def hidden_since(doc: FreeCAD.Document, shown: dict[str, bool]) -> list[str]:
    """The objects that went from visible to hidden since ``shown``, or [] when
    that could not be read."""
    try:
        return newly_hidden(doc, shown)
    except Exception as e:
        agent_warning(f"MCP RPC: could not read which objects went hidden: {type(e).__name__}: {e}\n")
        return []


def failed_before(doc: FreeCAD.Document) -> set[str] | None:
    """``failed_names`` of the document before a change, or None when it could
    not be taken: the report about other objects must never fail the call."""
    try:
        return failed_names(doc.Objects)
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the failed objects: {type(e).__name__}: {e}\n")
        return None


def collateral_report(doc: FreeCAD.Document, before: set[str] | None, changed: Any) -> dict[str, Any]:
    """The objects a change made fail or left stale (``newly_failed_report``),
    or ``{}`` when there are none or the report itself failed: it is extra
    information about other objects, and a failure of it must not turn the
    user's create, update or delete into an error."""
    if before is None:
        return {}
    try:
        return newly_failed_report(doc.Objects, before, changed)
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the objects the change broke: {type(e).__name__}: {e}\n")
        return {}


def _stale_fields(failed: Any) -> dict[str, Any]:
    """The objects built on ``failed`` that FreeCAD did not rebuild, for the
    reply about a failed object (see object_validation.stale_dependents)."""
    try:
        rows, total = stale_dependents([failed])
    except Exception as e:
        agent_warning(f"MCP RPC: could not list the objects built on the failed one: {type(e).__name__}: {e}\n")
        return {}
    if not rows:
        return {}
    return {"stale_objects": rows, "stale_count": total, "stale_truncated": total > len(rows)}


def create_object_gui(doc_name: str, obj: Object):
    """Create an object in ``doc_name`` according to ``obj.type``.

    Returns the created object's actual ``Name`` on success (FreeCAD
    sanitises and de-duplicates requested names - ``Box`` may come back as
    ``Box001`` - and every later get_object/update_object call needs the real
    one), or an error string on failure.
    """
    try:
        doc = FreeCAD.getDocument(doc_name)
    except Exception:
        agent_error(f"Document '{doc_name}' not found.\n")
        return f"Document '{doc_name}' not found.\n"
    try:
        # active_document holds doc active for as long as its transaction can
        # be open, so FreeCAD attaches the transaction to doc itself instead
        # of opening an empty linked one in whatever document the GUI has
        # focused (transactions.active_document docstring).
        with active_document(doc), transaction("create_object") as tx:
            if obj.type == "Fem::FemMeshGmsh" and not obj.analysis:
                return (
                    "Fem::FemMeshGmsh requires an 'analysis_name' naming the "
                    "Fem::AnalysisPython container to add the mesh to."
                )
            existing = {o.Name for o in doc.Objects}
            before = failed_before(doc)
            shown = shown_before(doc)
            shapes_before = snapshot(doc)
            requested = list(obj.properties)
            given = dict(obj.properties)
            plan = partdesign.Plan()
            try:
                if obj.type == "Fem::FemMeshGmsh":
                    created = _create_fem_mesh(doc, obj)
                elif obj.type.startswith("Fem::"):
                    created = _create_fem_object(doc, obj)
                elif obj.type in _PYTHON_FACTORIES:
                    created = _create_python_object(doc, obj)
                else:
                    plan = partdesign.plan_create(doc, obj.type, obj.properties, obj.body)
                    created = _create_generic_object(doc, obj, plan)
            except Exception as e:
                return _creation_failure(doc, tx, existing, e)

            _keep_requested_label(created, obj.name)
            doc.recompute()
            problem = object_validity_error(created)
            quantities = _reading("the quantities", lambda: quantity_values(created, requested), {})
            adjusted = _reading("the adjusted values", lambda: adjusted_values(created, given), [])
            load = _reading("the load", lambda: load_info(created) or material_info(created), None)
            extra = _reading("the name and placement", lambda: _name_and_placement_fields(created, requested), {})
            if _reading("the mesh type", lambda: is_gmsh(created), False):
                extra.update(_reading("the mesh", lambda: {"mesh": mesh_info(created)}, {}))
            extra.update(_partdesign_fields(created, plan))
            if plan.body is not None:
                tip = _reading("the Body's Tip", lambda: partdesign.tip_note(plan.body, created), None)
                if tip:
                    extra["notes"] = [*extra.get("notes", []), tip]
            if obj.type.startswith("Draft::") and obj.type not in _DRAFT_ARRAYS:
                extra["notes"] = [*extra.get("notes", []), f"{obj.type} is a flat profile: extrude it with Part::Extrusion to make a solid."]
            if obj.type == "Draft::ShapeString":
                font = _reading("the font", lambda: f"Font: {created.FontFile}", None)
                if font:
                    extra["notes"] = [*extra.get("notes", []), font]
            if not problem:
                _reading("which sources to hide", lambda: hide_sources(created, None), None)
                extra.update(_reading("the shape", lambda: _shape_fields(created), {}))
                hidden = hidden_since(doc, shown)
                if hidden:
                    extra["hidden"] = hidden
            collateral = collateral_report(doc, before, created)
            if not problem:
                # Other top-level shapes this call rebuilt (a Body built on the new feature).
                extra.update(_reading("the shapes the call changed", lambda: changed_shapes(doc, shapes_before, _own_names(doc, created)), {}))
        # The transaction commits above regardless of problem, so an object
        # that failed to compute stays in the document; undo removes it, or
        # the caller can fix it with update_object or remove it with
        # delete_object.
        if problem:
            agent_error(problem + "\n")
            return {
                "success": False,
                "object_name": created.Name,
                "error": problem,
                **(collateral or _stale_fields(created)),
            }
        reply = {"success": True, "object_name": created.Name, **tx.reply_fields(), **extra, **collateral}
        if adjusted:
            reply["adjusted"] = adjusted
        if quantities:
            reply["quantities"] = quantities
        if load is not None:
            reply["load"] = load
        return reply
    except Exception as e:
        return str(e)


def _update_failure(doc: FreeCAD.Document, tx, error: Exception, meshing: bool = False) -> str:
    """Undo an update that raised and return the error text for the caller.

    The call's own transaction is aborted, which restores every property it
    set and leaves no undo step; the document is recomputed so nothing is
    left touched. When the call joined a transaction that was already open
    (aborting would discard the user's own edit) nothing is rolled back and
    the message does not claim it.
    """
    message = str(error).strip().rstrip(".") or type(error).__name__
    if not tx.opened:
        return message
    try:
        tx.abort()
        doc.recompute()
    except Exception as e:
        agent_warning(f"MCP RPC: could not discard transaction '{tx.name}': {type(e).__name__}: {e}\n")
        return message
    if meshing:
        return f"{message}. Nothing was changed; the mesh itself may hold a partial result: run_fem_analysis remeshes it."
    return f"{message}. Nothing was changed."


def edit_object_gui(doc_name: str, obj: Object):
    """Apply properties to an existing object and verify the recomputed state."""
    try:
        doc = FreeCAD.getDocument(doc_name)
    except Exception:
        agent_error(f"Document '{doc_name}' not found.\n")
        return f"Document '{doc_name}' not found.\n"

    obj_ins = doc.getObject(obj.name)
    if obj_ins is None:
        agent_error(
            f"Object '{obj.name}' not found in document '{doc_name}'.\n"
        )
        return f"Object '{obj.name}' not found in document '{doc_name}'.\n"

    try:
        # See create_object_gui: hold doc active for the transaction's whole
        # life so FreeCAD does not open an empty linked transaction elsewhere.
        meshing = False  # True while the mesh is being generated
        with active_document(doc), transaction("update_object") as tx:
            try:
                before = failed_before(doc)
                shown = shown_before(doc)
                shapes_before = snapshot(doc)
                plan = partdesign.plan_update(doc, obj_ins, obj.properties)
                set_object_property(doc, obj_ins, plan.properties)
                if plan.geometry is not None:
                    replaced = int(getattr(obj_ins, "ConstraintCount", 0))
                    partdesign.set_geometry(obj_ins, plan.geometry)
                    if replaced:
                        plan.notes.append(
                            f"Replaced the geometry and its {replaced} constraint{'s' if replaced != 1 else ''}."
                        )
                doc.recompute()
                problem = object_validity_error(obj_ins)
                quantities = _reading("the quantities", lambda: quantity_values(obj_ins, obj.properties), {})
                adjusted = _reading("the adjusted values", lambda: adjusted_values(obj_ins, obj.properties), [])
                load = _reading("the load", lambda: load_info(obj_ins) or material_info(obj_ins), None)
                extra = _reading("the name and placement", lambda: _name_and_placement_fields(obj_ins, list(obj.properties)), {})
                extra.update(_partdesign_fields(obj_ins, plan))
                if is_gmsh(obj_ins) and not problem and changes_meshing(obj_ins, obj.properties):
                    # The mesh does not follow its solid or its parameters by
                    # itself: mesh again, as create_object does.
                    nodes_before = obj_ins.FemMesh.NodeCount
                    meshing = True
                    generate_mesh(obj_ins)
                    meshing = False
                    extra.update(_reading("the mesh", lambda: {"mesh": {**mesh_info(obj_ins), "remeshed_from_nodes": int(nodes_before)}}, {}))
                if not problem:
                    _reading("which sources to hide", lambda: hide_sources(obj_ins, obj.properties), None)
                    extra.update(_reading("the shape", lambda: _shape_fields(obj_ins), {}))
                    # The object itself cannot go hidden as a consequence of its own update: it is the Visibility the caller set
                    # (as a property or as {"ViewObject": {"Visibility": false}}).
                    hidden = [name for name in hidden_since(doc, shown) if name != str(obj_ins.Name)]
                    if hidden:
                        extra["hidden"] = hidden
                collateral = collateral_report(doc, before, obj_ins)
                if not problem:
                    # Other top-level shapes this call rebuilt: a sketch changed under a Pad moves the Body.
                    extra.update(_reading("the shapes the call changed", lambda: changed_shapes(doc, shapes_before, _own_names(doc, obj_ins)), {}))
            except Exception as e:
                # transaction() commits on exit even when the block raises: discard it so a
                # valid property set beside a bad one is not left applied.
                return _update_failure(doc, tx, e, meshing)
        # Commits above regardless of problem, so a property change that left
        # the object invalid stays applied; undo reverts it.
        if problem:
            agent_error(problem + "\n")
            return {
                "success": False,
                "object_name": obj_ins.Name,
                "error": problem,
                **(collateral or _stale_fields(obj_ins)),
            }
        FreeCAD.Console.PrintMessage(f"Object '{obj_ins.Name}' updated via RPC.\n")
        reply = {"success": True, "object_name": obj_ins.Name, **tx.reply_fields(), **extra, **collateral}
        if adjusted:
            reply["adjusted"] = adjusted
        if quantities:
            reply["quantities"] = quantities
        if load is not None:
            reply["load"] = load
        return reply
    except Exception as e:
        return str(e)
