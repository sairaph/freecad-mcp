"""Object creation and property updates for the RPC ``create_object`` and
``edit_object`` handlers.

``create_object_gui`` selects one helper per kind of object:
``_create_fem_mesh`` for a Gmsh FEM mesh (accepting the older parameter
names), ``_create_fem_object`` for the other ``Fem::`` types,
``_create_python_object`` for the Python-implemented types that have a
factory here (``Part::Tube`` and the Draft shapes), and
``_create_generic_object`` for any type registered with ``doc.addObject``.
``edit_object_gui`` sets properties on an existing object. Both check the
object after recompute and report the actual object name.
"""

import math
import re
from typing import Any

import FreeCAD
import ObjectsFem

from rpc_server.agent_log import agent_error, agent_warning
from rpc_server.empty_results import empty_result
from rpc_server.fem_loads import load_info
from rpc_server.property_mapper import FILLET_TYPES, Object, fillet_edges_text, quantity_values, set_object_property
from rpc_server.object_validation import failed_names, newly_failed_report, object_validity_error, stale_dependents
from rpc_server.serialize import shape_summary
from rpc_server.source_visibility import hide_sources, newly_hidden, visibility_snapshot
from rpc_server.transactions import active_document, transaction


def _create_fem_mesh(doc: FreeCAD.Document, obj: Object):
    """Create a ``Fem::FemMeshGmsh`` and run Gmsh to populate it.

    Accepts both the FreeCAD 0.x and 1.x property names (``Part``/``Shape``,
    ``ElementSize{Max,Min}``/``CharacteristicLength{Max,Min}``).
    Returns the created mesh object.
    """
    from femmesh.gmshtools import GmshTools

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

    GmshTools(res).create_mesh()
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
}


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


def _create_generic_object(doc: FreeCAD.Document, obj: Object):
    if obj.type in FILLET_TYPES and not ("Base" in obj.properties and "Edges" in obj.properties):
        # Without edges FreeCAD cannot compute it ("no suitable edges"), so an object
        # made bare would only stay behind broken.
        size = FILLET_TYPES[obj.type]
        raise ValueError(
            f"{obj.type} needs Base and Edges in obj_properties, for example "
            f'{{"Base": "Box", "Edges": ["Edge1", "Edge2"], "{size}": 1}}; '
            "call list_subelements with kind edges to see the edge names."
        )
    try:
        res = doc.addObject(obj.type, obj.name)
    except Exception as e:
        if "not a document object type" in str(e):
            raise ValueError(_unregistered_type_message(obj.type)) from e
        raise
    set_object_property(doc, res, obj.properties)
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
        edges = fillet_edges_text(obj, requested)
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
    summary = shape_summary(obj.Shape)
    if summary is None:
        return {}
    if summary.get("null") and hasattr(obj, "Group") and not obj.Group:
        return {}
    fields: dict[str, Any] = {"shape": summary}
    found = empty_result(obj, summary.get("volume"))
    if found is not None:
        fields["warning"] = found[0]
    return fields


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
            requested = list(obj.properties)
            try:
                if obj.type == "Fem::FemMeshGmsh":
                    created = _create_fem_mesh(doc, obj)
                elif obj.type.startswith("Fem::"):
                    created = _create_fem_object(doc, obj)
                elif obj.type in _PYTHON_FACTORIES:
                    created = _create_python_object(doc, obj)
                else:
                    created = _create_generic_object(doc, obj)
            except Exception as e:
                return _creation_failure(doc, tx, existing, e)

            _keep_requested_label(created, obj.name)
            doc.recompute()
            problem = object_validity_error(created)
            quantities = quantity_values(created, requested)
            load = load_info(created)
            extra = _name_and_placement_fields(created, requested)
            if not problem:
                hide_sources(created, None)
                extra.update(_shape_fields(created))
                hidden = hidden_since(doc, shown)
                if hidden:
                    extra["hidden"] = hidden
            collateral = collateral_report(doc, before, created)
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
        if quantities:
            reply["quantities"] = quantities
        if load is not None:
            reply["load"] = load
        return reply
    except Exception as e:
        return str(e)


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
        with active_document(doc), transaction("update_object") as tx:
            before = failed_before(doc)
            shown = shown_before(doc)
            set_object_property(doc, obj_ins, obj.properties)
            doc.recompute()
            problem = object_validity_error(obj_ins)
            quantities = quantity_values(obj_ins, obj.properties)
            load = load_info(obj_ins)
            extra = _name_and_placement_fields(obj_ins, list(obj.properties))
            if not problem:
                hide_sources(obj_ins, obj.properties)
                extra.update(_shape_fields(obj_ins))
                hidden = hidden_since(doc, shown)
                if hidden:
                    extra["hidden"] = hidden
            collateral = collateral_report(doc, before, obj_ins)
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
        if quantities:
            reply["quantities"] = quantities
        if load is not None:
            reply["load"] = load
        return reply
    except Exception as e:
        return str(e)
