"""Meshing and mesh checks for FEM analyses.

A Gmsh mesh object does not follow its solid: it keeps the mesh it made until
someone meshes again, so a solid that changed after meshing is solved on the old
mesh (a load face that touches no node gives an all-zero result). The tool
therefore stores a fingerprint of the shape it meshed on the mesh object, in a
hidden string property (``McpMeshedShape``, JSON), which is saved with the
document, and compares it with the shape later. A mesh made outside the tool has
none: its node bounding box is compared with the shape's box instead.
"""

import json
from typing import Any

from rpc_server.serialize import center_of_mass, tight_bound_box

#: The hidden property that holds the fingerprint of the shape a mesh was made from.
FINGERPRINT_PROPERTY = "McpMeshedShape"

#: Mesh object types the tool meshes and remeshes.
GMSH_TYPE = "Fem::FemMeshGmsh"

#: A mesh node box this much smaller or larger than the shape's box, per axis and
#: relative to the shape's size on that axis, is a different shape (a curved
#: shape's coarse mesh falls short of the exact box by a fraction of that).
_BOX_TOLERANCE = 0.02

#: Fingerprints compare equal within this relative tolerance (and 1e-9 mm).
_FINGERPRINT_TOLERANCE = 1e-6


def meshed_shape(mesh_obj: Any) -> Any:
    """The solid object a mesh object meshes (``Shape``, or ``Part`` in older
    files), or None."""
    for attr in ("Shape", "Part"):
        target = getattr(mesh_obj, attr, None)
        if target is not None and not isinstance(target, (list, tuple)):
            return target
    return None


def _box_of(shape: Any) -> list[float]:
    box = tight_bound_box(shape)
    return [box.XMin, box.YMin, box.ZMin, box.XMax, box.YMax, box.ZMax]


def _scale_of(box: list[float]) -> float:
    return max(box[3] - box[0], box[4] - box[1], box[5] - box[2], 1.0)


def _scalars_of(shape: Any) -> dict[str, Any]:
    centre = center_of_mass(shape)
    return {
        "volume": shape.Volume,
        "area": shape.Area,
        "com": None if centre is None else [centre["x"], centre["y"], centre["z"]],
    }


def _face_list(shape: Any) -> list[list[Any]]:
    """Every face as ``[surface type, area, centre x, y, z]``, to 12 significant
    digits (far below the comparison tolerance, so a saved record and the shape
    it describes agree), in the order of the faces: the comparison does not
    depend on it."""
    rows = []
    for face in shape.Faces:
        centre = face.CenterOfMass
        rows.append(
            [type(face.Surface).__name__]
            + [float(f"{value:.12g}") for value in (face.Area, centre.x, centre.y, centre.z)]
        )
    return rows


def fingerprint(shape_obj: Any) -> dict[str, Any] | None:
    """What identifies the geometry of a shape object: the tight box, volume,
    surface area, centre of mass, face count and the type, area and centre of
    every face. None when any of it cannot be read."""
    try:
        shape = shape_obj.Shape
        record = {"box": _box_of(shape), **_scalars_of(shape), "faces": len(shape.Faces)}
        record["face_list"] = _face_list(shape)
        return record
    except Exception:
        return None


def store_fingerprint(mesh_obj: Any) -> None:
    """Record on ``mesh_obj`` the shape it was just meshed from."""
    target = meshed_shape(mesh_obj)
    print_ = fingerprint(target) if target is not None else None
    if print_ is None:
        return
    # The node count of the mesh itself: an undo restores this record but not the
    # mesh object's mesh, and the two then disagree.
    print_["nodes"] = int(mesh_obj.FemMesh.NodeCount)
    if FINGERPRINT_PROPERTY not in mesh_obj.PropertiesList:
        mesh_obj.addProperty("App::PropertyString", FINGERPRINT_PROPERTY, "Base", "Shape this mesh was made from (freecad-mcp)")
        mesh_obj.setPropertyStatus(FINGERPRINT_PROPERTY, "Hidden")
    setattr(mesh_obj, FINGERPRINT_PROPERTY, json.dumps(print_, separators=(",", ":")))


def _same(a: float, b: float, scale: float) -> bool:
    return abs(a - b) <= _FINGERPRINT_TOLERANCE * max(abs(scale), 1.0) + 1e-9


def _faces_match(old: list[list[Any]], new: list[list[Any]], scale: float) -> bool:
    """Whether the two face lists describe the same faces, in any order: each
    stored face has an unmatched face of the same type whose area and centre are
    within the fingerprint tolerance. The new faces are bucketed by the grid cell
    of their centre (cell size: twice the tolerance), so each stored face looks
    at its own cell and the 26 around it, and a value on a cell boundary matches
    like any other."""
    if len(old) != len(new):
        return False
    tolerance = _FINGERPRINT_TOLERANCE * scale + 1e-9
    area_tolerance = _FINGERPRINT_TOLERANCE * scale * scale + 1e-9
    cell = 2 * tolerance
    grid: dict[tuple[str, int, int, int], list[int]] = {}
    for index, row in enumerate(new):
        key = (row[0], int(row[2] // cell), int(row[3] // cell), int(row[4] // cell))
        grid.setdefault(key, []).append(index)
    taken: set[int] = set()
    for row in old:
        cx, cy, cz = int(row[2] // cell), int(row[3] // cell), int(row[4] // cell)
        found = None
        for dx in (-1, 0, 1):
            for dy in (-1, 0, 1):
                for dz in (-1, 0, 1):
                    for index in grid.get((row[0], cx + dx, cy + dy, cz + dz), ()):
                        other = new[index]
                        if (
                            index not in taken
                            and abs(other[1] - row[1]) <= area_tolerance
                            and abs(other[2] - row[2]) <= tolerance
                            and abs(other[3] - row[3]) <= tolerance
                            and abs(other[4] - row[4]) <= tolerance
                        ):
                            found = index
                            break
                    if found is not None:
                        break
                if found is not None:
                    break
            if found is not None:
                break
        if found is None:
            return False
        taken.add(found)
    return True


def _record_differs(record: dict[str, Any], shape: Any, box: list[float]) -> bool:
    """Whether ``shape`` is not the one ``record`` describes. The cheap fields come
    first, each stage is computed only when the earlier ones matched, and the faces
    (the dearest) last. A field a record made by an earlier version lacks is not
    compared."""
    scale = _scale_of(box)
    if len(record.get("box", [])) != 6 or any(not _same(a, b, scale) for a, b in zip(record["box"], box)):
        return True
    if record.get("faces") != len(shape.Faces):
        return True
    scalars = _scalars_of(shape)
    if not _same(record.get("volume", 0.0), scalars["volume"], scalars["volume"]):
        return True
    if "area" in record and not _same(record["area"], scalars["area"], scalars["area"]):
        return True
    if record.get("com") and scalars["com"] and any(not _same(a, b, scale) for a, b in zip(record["com"], scalars["com"])):
        return True
    if "face_list" in record:
        return not _faces_match(record["face_list"], _face_list(shape), scale)
    return False


def _node_box_changed(mesh_obj: Any, box: list[float]) -> bool:
    """A mesh with no usable record: its node box against the shape's, per axis."""
    try:
        bb = mesh_obj.FemMesh.BoundBox
        node = [bb.XMin, bb.YMin, bb.ZMin, bb.XMax, bb.YMax, bb.ZMax]
    except Exception:
        return False
    for i in range(3):
        extent = box[3 + i] - box[i]
        tolerance = _BOX_TOLERANCE * extent + 1e-6
        if abs(node[i] - box[i]) > tolerance or abs(node[3 + i] - box[3 + i]) > tolerance:
            return True
    return False


def mesh_changed_since_meshing(mesh_obj: Any) -> str | None:
    """"fingerprint" or "bounding box" when the shape of ``mesh_obj`` is no
    longer the one it was meshed from, naming how that was found, or "mesh" when
    the shape is the same but the mesh object no longer holds the mesh the record
    describes (an undo restores the record, not the mesh); None when it matches or
    cannot be told (no shape, no mesh yet). A record that cannot be read or
    compared (an unreadable face, a damaged string) falls back on the node
    bounding box, like a mesh made outside the tool."""
    target = meshed_shape(mesh_obj)
    if target is None:
        return None
    try:
        shape = target.Shape
        box = _box_of(shape)
        if mesh_obj.FemMesh.NodeCount == 0:
            return None
    except Exception:
        return None
    stored = getattr(mesh_obj, FINGERPRINT_PROPERTY, "") if FINGERPRINT_PROPERTY in mesh_obj.PropertiesList else ""
    if stored:
        try:
            record = json.loads(stored)
            if _record_differs(record, shape, box):
                return "fingerprint"
            if "nodes" in record and record["nodes"] != mesh_obj.FemMesh.NodeCount:
                return "mesh"
            return None
        except Exception:
            pass
    return "bounding box" if _node_box_changed(mesh_obj, box) else None


def generate_mesh(mesh_obj: Any) -> int:
    """Mesh ``mesh_obj`` with Gmsh from its current shape and parameters, store
    the fingerprint of the shape, recompute so the mesh is not left Touched, and
    return the node count."""
    from femmesh.gmshtools import GmshTools

    GmshTools(mesh_obj).create_mesh()
    store_fingerprint(mesh_obj)
    # Storing the fingerprint touched the object: leave it up to date.
    mesh_obj.Document.recompute()
    return int(mesh_obj.FemMesh.NodeCount)


def element_order(mesh_obj: Any) -> str | None:
    """"1st" or "2nd" from the solid elements of the mesh, falling back on the
    ElementOrder property; None when it is neither."""
    try:
        fm = mesh_obj.FemMesh
        volumes = fm.Volumes
        if volumes:
            count = len(fm.getElementNodes(volumes[0]))
            return {4: "1st", 10: "2nd"}.get(count)
    except Exception:
        pass
    order = getattr(mesh_obj, "ElementOrder", None)
    return order if order in ("1st", "2nd") else None


def mesh_info(mesh_obj: Any) -> dict[str, Any]:
    """What a reply says about a mesh: its element order and node count."""
    info: dict[str, Any] = {"name": mesh_obj.Name}
    order = element_order(mesh_obj)
    if order:
        info["element_order"] = order
    try:
        info["node_count"] = int(mesh_obj.FemMesh.NodeCount)
    except Exception:
        pass
    return info


#: Properties of a Gmsh mesh object that the mesh depends on besides those in its
#: "Mesh Parameters" group.
MESHING_LINKS = ("Shape", "Part", "MeshRegionList", "MeshGroupList", "BoundaryLayerList")


def changes_meshing(mesh_obj: Any, names: Any) -> bool:
    """Whether setting the properties ``names`` changes what Gmsh reads: the
    shape or a mesh refinement it takes, or any parameter of its "Mesh
    Parameters" group."""
    for name in names:
        base = str(name).split(".", 1)[0]
        if base in MESHING_LINKS:
            return True
        try:
            if mesh_obj.getGroupOfProperty(base) == "Mesh Parameters":
                return True
        except Exception:
            pass
    return False


def is_gmsh(obj: Any) -> bool:
    """Whether ``obj`` is a Gmsh mesh object (a Python feature, so its type is
    the proxy's, as femutils.type_of_obj reads it)."""
    proxy_type = getattr(getattr(obj, "Proxy", None), "Type", None)
    return (proxy_type if isinstance(proxy_type, str) else getattr(obj, "TypeId", "")) == GMSH_TYPE
