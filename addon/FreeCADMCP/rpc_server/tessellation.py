"""Shape tessellation with explicit, deterministic quality settings.

Used by export_document (mesh formats and glTF), check_printability and
solid_to_mesh. ``MeshPart.meshFromShape`` cleans the triangulation of the shape
it meshes (Mod/MeshPart/App/Mesher.cpp:227-232), so it always gets a copy of
the object's shape, and its arguments are passed as keywords only: the
function tries several signatures and a positional call can match the wrong one
(Mod/MeshPart/App/AppMeshPartPy.cpp:472-521).

GUI thread only. Part and MeshPart are imported inside the functions that need them.
"""

import math
from typing import Any

from rpc_server.errors import INVALID_INPUT, fail


# (linear deflection in mm, angular deflection in degrees)
PRESETS: dict[str, tuple[float, float]] = {
    "coarse": (0.1, 20.0),  # quick previews
    "standard": (0.02, 8.0),  # normal FDM prints
    "fine": (0.005, 3.0),  # resin, small curved parts
}
DEFAULT_QUALITY = "standard"

LINEAR_RANGE = (0.001, 100.0)  # mm
ANGULAR_RANGE = (0.5, 90.0)  # degrees


class Settings:
    """Resolved tessellation settings."""

    def __init__(
        self,
        quality: str,
        linear_deflection: float,
        angular_deflection_deg: float,
        relative: bool,
    ) -> None:
        self.quality = quality
        self.linear_deflection = linear_deflection
        self.angular_deflection_deg = angular_deflection_deg
        self.relative = relative

    @property
    def angular_deflection_rad(self) -> float:
        return math.radians(self.angular_deflection_deg)

    def as_dict(self) -> dict[str, Any]:
        return {
            "quality": self.quality,
            "linear_deflection": self.linear_deflection,
            "angular_deflection_deg": self.angular_deflection_deg,
            "relative": self.relative,
        }


def _number_in(value: Any, name: str, bounds: tuple[float, float]) -> float | dict[str, Any]:
    low, high = bounds
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return fail(INVALID_INPUT, f"{name} must be a number from {low:g} to {high:g}, not {value!r}")
    number = float(value)
    if not math.isfinite(number) or number < low or number > high:
        return fail(INVALID_INPUT, f"{name} must be from {low:g} to {high:g}, not {value!r}")
    return number


def resolve_settings(
    quality: Any = None,
    linear_deflection: Any = None,
    angular_deflection_deg: Any = None,
    relative: Any = None,
) -> Settings | dict[str, Any]:
    """Return the settings for the given options, or a failure reply.

    ``quality`` picks a preset (default ``standard``); an explicit
    ``linear_deflection`` (mm) or ``angular_deflection_deg`` overrides the
    preset's value. ``relative`` makes the linear deflection relative to the
    edge length (default false).
    """
    if quality is None or quality == "":
        quality = DEFAULT_QUALITY
    if quality not in PRESETS:
        return fail(
            INVALID_INPUT,
            f"quality must be one of {', '.join(PRESETS)}, not {quality!r}",
        )
    linear, angular = PRESETS[quality]
    if linear_deflection is not None:
        linear = _number_in(linear_deflection, "linear_deflection", LINEAR_RANGE)
        if isinstance(linear, dict):
            return linear
    if angular_deflection_deg is not None:
        angular = _number_in(angular_deflection_deg, "angular_deflection_deg", ANGULAR_RANGE)
        if isinstance(angular, dict):
            return angular
    if relative is None:
        relative = False
    if not isinstance(relative, bool):
        return fail(INVALID_INPUT, f"relative must be true or false, not {relative!r}")
    return Settings(quality, linear, angular, relative)


def is_mesh_feature(obj: Any) -> bool:
    """True for a Mesh::Feature (or a type derived from it)."""
    try:
        return bool(obj.isDerivedFrom("Mesh::Feature"))
    except Exception:
        return False


def parent_geo_feature_group(obj: Any) -> Any:
    """``obj.getParentGeoFeatureGroup()``, or None when it has none or raises.

    Works for any DocumentObject, including an App::Link, which is not
    itself a GeoFeature (App/DocumentObjectPyImp.cpp:801-818).
    """
    try:
        return obj.getParentGeoFeatureGroup()
    except Exception:
        return None


def container_placement(obj: Any) -> Any:
    """The enclosing App::Part or PartDesign Body's global placement, or None.

    ``obj``'s own Placement is applied wherever its shape or mesh is read
    from (``Part.getShape``, ``obj.Mesh``); this is only the extra transform
    an enclosing container contributes, taken from the parent group's own
    ``getGlobalPlacement()`` (App/GeoFeature.cpp:315-345, a GeoFeaturePy-only
    method that would raise on ``obj`` itself for a Link, but not on its
    parent group, which is always a Body or an App::Part). That call already
    accumulates any further nesting, so a caller applies the result once, on
    top of ``obj``'s own placement, never in place of it: nothing is applied
    twice.
    """
    grp = parent_geo_feature_group(obj)
    if grp is None:
        return None
    try:
        return grp.getGlobalPlacement()
    except Exception:
        return None


def apply_container_placement(shape: Any, obj: Any) -> Any:
    """Return a copy of ``shape`` with ``container_placement(obj)`` applied.

    ``shape`` is assumed to already carry ``obj``'s own Placement (directly,
    or through a subname path that accumulates it and that of every object
    named in it); this applies only the further transform an enclosing
    container contributes, once, on top of that. Shared by every route that
    ends up with a shape positioned as far as ``obj`` itself but no further:
    ``shape_of`` for a whole object or a plain element name picked out of
    it, and a subname path resolved directly with ``Part.getShape``.
    """
    shape = shape.copy()
    container = container_placement(obj)
    if container is not None and not container.isIdentity():
        shape.transformShape(container.toMatrix())
    return shape


def shape_of(obj: Any) -> Any:
    """Return a copy of ``obj``'s shape in global coordinates, or None.

    ``Part.getShape`` resolves Part features, Bodies, Links and App::Part
    containers (Mod/Part/App/AppPartPy.cpp:753), but only applies obj's own
    Placement, not that of an enclosing App::Part or PartDesign Body; see
    ``container_placement``. The copy keeps meshing from stripping the
    triangulation of the document's own shape.
    """
    import Part

    try:
        shape = Part.getShape(obj)
    except Exception:
        return None
    if shape is None or shape.isNull():
        return None
    return apply_container_placement(shape, obj)


def mesh_shape(shape: Any, settings: Settings) -> Any:
    """Tessellate ``shape`` (already a copy) into a Mesh.Mesh."""
    import MeshPart

    return MeshPart.meshFromShape(
        Shape=shape,
        LinearDeflection=settings.linear_deflection,
        AngularDeflection=settings.angular_deflection_rad,
        Relative=settings.relative,
    )


def mesh_object(obj: Any, settings: Settings) -> Any:
    """Return a Mesh.Mesh of ``obj`` in global coordinates, or None.

    A Mesh::Feature is copied as it is, with its global placement (its Mesh
    carries the object's own placement as its transform,
    Mod/Mesh/App/MeshFeature.cpp:61-66). Any other object is tessellated from
    its shape; None means it has no geometry.
    """
    if is_mesh_feature(obj):
        mesh = obj.Mesh.copy()
        try:
            mesh.Placement = obj.getGlobalPlacement()
        except Exception:
            pass
        return mesh
    shape = shape_of(obj)
    if shape is None:
        return None
    return mesh_shape(shape, settings)


def contained_meshes(obj: Any, seen: set[int] | None = None) -> list:
    """Mesh::Feature objects in ``obj``'s ``Group`` tree, recursively.

    A container's own Part shape (a compound of its Part children,
    Mod/Part/App/PartFeature.cpp:1015-1035) never includes a Mesh::Feature
    child, so a mixed container passes a has-shape test on its shape alone
    and its meshes would otherwise vanish from a mesh export with no sign of
    it (unlike a mesh-only container, which has no shape of its own).
    """
    if seen is None:
        seen = set()
    found = []
    for child in getattr(obj, "Group", None) or []:
        child_id = id(child)
        if child_id in seen:
            continue
        seen.add(child_id)
        if is_mesh_feature(child):
            found.append(child)
        else:
            found.extend(contained_meshes(child, seen))
    return found


def _feature_box(obj: Any) -> Any:
    """The global bounding box of a Mesh::Feature, or None for an empty mesh.

    ``obj.Mesh.BoundBox`` already includes the object's own Placement (the
    mesh carries it as its transform), so it is read as it is; only an
    enclosing container adds a transform, and then the mesh is copied with the
    global placement, which counts every Placement once.
    """
    try:
        mesh = obj.Mesh
        if mesh is None or mesh.CountFacets <= 0:
            return None
        container = container_placement(obj)
        if container is not None and not container.isIdentity():
            mesh = mesh.copy()
            mesh.Placement = obj.getGlobalPlacement()
        box = mesh.BoundBox
        return box if box.isValid() else None
    except Exception:
        return None


def _link_mesh_target(obj: Any) -> Any:
    """The non-empty Mesh::Feature that link ``obj`` shows, or None."""
    if not callable(getattr(obj, "getLinkedObject", None)):
        return None
    try:
        target = obj.getLinkedObject(True)
        if target is None or target is obj or not is_mesh_feature(target):
            return None
        return target if target.Mesh.CountFacets > 0 else None
    except Exception:
        return None


def has_mesh(obj: Any, seen: set[str] | None = None) -> bool:
    """Whether ``obj`` is, links to or holds a non-empty mesh: what
    ``mesh_boxes`` would find, without copying or measuring any mesh."""
    if seen is None:
        seen = set()
    if obj.Name in seen:
        return False
    seen.add(obj.Name)
    if is_mesh_feature(obj):
        try:
            return obj.Mesh.CountFacets > 0
        except Exception:
            return False
    if _link_mesh_target(obj) is not None:
        return True
    return any(has_mesh(child, seen) for child in getattr(obj, "Group", None) or [])


def _link_box(obj: Any) -> Any:
    """The global bounding box of a mesh that link ``obj`` shows, or None.

    ``getLinkedObject(True, Matrix(), True)`` gives the transform that places
    the target as the link shows it (the link's own Placement, with the
    target's when ``LinkTransform`` is set); the enclosing container's
    placement goes on top.
    """
    target = _link_mesh_target(obj)
    if target is None:
        return None
    try:
        import FreeCAD as App

        placement = App.Placement(obj.getLinkedObject(True, App.Matrix(), True)[1])
        container = container_placement(obj)
        if container is not None:
            placement = container.multiply(placement)
        mesh = target.Mesh.copy()
        mesh.Placement = placement
        box = mesh.BoundBox
        return box if box.isValid() else None
    except Exception:
        return None


def mesh_boxes(obj: Any, seen: set[str] | None = None) -> list:
    """The global bounding boxes of the meshes ``obj`` is or holds.

    A Mesh::Feature gives its own box, a link to one the box where the link
    shows it, and a container or group the boxes of its members, recursively.
    Nothing is tessellated or converted: a mesh is only read for its bounds.
    """
    if seen is None:
        seen = set()
    if obj.Name in seen:
        return []
    seen.add(obj.Name)
    boxes = []
    if is_mesh_feature(obj):
        box = _feature_box(obj)
        if box is not None:
            boxes.append(box)
        return boxes
    box = _link_box(obj)
    if box is not None:
        boxes.append(box)
    for child in getattr(obj, "Group", None) or []:
        boxes.extend(mesh_boxes(child, seen))
    return boxes


def parent_map(doc: Any) -> dict[str, str]:
    """Return child object Name -> its first parent's Name, as FreeCAD's tree
    view nests it (``parent_lists`` has every parent).

    The single source of both ``tree_root_objects`` (a top-level object is
    one absent from this map) and a compact object list's "parent" field, so
    the two never disagree about what counts as top level.
    """
    return {name: names[0] for name, names in parent_lists(doc).items()}


def parent_lists(doc: Any) -> dict[str, list[str]]:
    """Return child object Name -> the Names of every object whose tree node
    claims it (a tool shared by two booleans has two), in FreeCAD's object
    order, the group or body first.

    Two ways a parent's tree node claims a child, checked in this order so a
    PartDesign feature's parent is its Body rather than a container the Body
    itself sits in:

    - ``getParentGeoFeatureGroup()`` (a Body's or App::Part's own contents,
      App/DocumentObjectPyImp.cpp:801-814).
    - A ViewProvider's ``claimChildren()`` when the object has one, else its
      ``Group`` (App::Part, DocumentObjectGroup). ``claimChildren()`` on a
      Link to another document's object returns that object's own children,
      in that other document (Gui/ViewProviderLink.cpp claimChildren): a bare
      name would then claim same-named objects of ``doc`` (a repeated "Body"
      or "Box"), so a claimed child is only counted when it belongs to
      ``doc`` itself.
    """
    parents: dict[str, list[str]] = {}
    for obj in doc.Objects:
        parent_group = parent_geo_feature_group(obj)
        if parent_group is not None:
            try:
                parents.setdefault(obj.Name, []).append(parent_group.Name)
            except Exception:
                pass

    for obj in doc.Objects:
        children = None
        vp = getattr(obj, "ViewObject", None)
        claim_children = getattr(vp, "claimChildren", None) if vp is not None else None
        if callable(claim_children):
            try:
                children = claim_children()
            except Exception:
                children = None
        if children is None:
            children = getattr(obj, "Group", None) or []
        for child in children:
            child_doc = getattr(child, "Document", None)
            if child_doc is None or child_doc.Name != doc.Name:
                continue
            name = getattr(child, "Name", None)
            if name:
                parent = str(getattr(obj, "Name", ""))
                if parent not in parents.setdefault(name, []):
                    parents[name].append(parent)

    return parents


def tree_root_objects(doc: Any) -> list:
    """Return doc's top-level objects, as FreeCAD's tree view shows them.

    ``doc.RootObjects`` (dependency-graph roots, App/Document.cpp:3748-3759)
    excludes anything another object references: an App::Link's target, an
    expression dependency, a TechDraw view's source, or the Base/Tool of a
    boolean. Those still get their own row in the tree, so this instead
    excludes only what ``parent_map`` claims as someone else's child.
    """
    claimed = parent_map(doc)
    return [obj for obj in doc.Objects if obj.Name not in claimed]
