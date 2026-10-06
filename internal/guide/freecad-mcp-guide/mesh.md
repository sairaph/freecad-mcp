# Mesh

Measure, patch and compare meshes with execute_code_headless. FreeCAD's Python has NumPy; these recipes use only that. Replace the file names in capitals.

- Mesh.BoundBox, Mesh.Points and Mesh.Facets of a Mesh::Feature already include its Placement. Never add the Placement again. A mesh read from a file has none.
- A mesh is a tessellated design. A fit describes the tessellation, not a manufacturing tolerance.
- Never convert a mesh to a solid to measure it. check_printability places a mesh by its bounding box.

## Work on a solid made from a mesh

mesh_to_solid gives a solid with one face per triangle. Booleans work on it; rounding does not.

- Cut holes and slots with Part::Cut as on any solid.
- refine merges only flat regions. Curved surfaces stay one face per triangle (a knob of 428 faces keeps them), so Part::Fillet and Part::Chamfer on those edges are not practical: list_subelements would show hundreds of edges and none picks out a clean curve. Round or chamfer the outside with a cut tool instead: for a chamfer on the bottom rim, cut away a ring minus a cone (a Part::Cylinder ring around the rim minus a Part::Cone) from the solid.
- Filter list_subelements (curve, along, on_bottom, min_length) instead of reading every edge.
- The source mesh and every earlier intermediate stay visible after a conversion or a cut. Hide them with update_object and {"ViewObject": {"Visibility": false}} before check_printability, or pass object_names to it and to export_document, so only the part counts.

## Measure a tessellated design

Select the vertices of the region, fit the shape by least squares, then derive what the mesh lacks, such as the height of a cap a flat cut off a ball.

```python
import numpy as np
import Mesh

mesh = Mesh.Mesh("DESIGN.stl")
pts = np.array([[p.x, p.y, p.z] for p in mesh.Points])

# 1. The region: the vertices above z 0.01 within 4 mm of the axis x 0, y 0.
region = pts[(pts[:, 2] > 0.01) & (np.hypot(pts[:, 0], pts[:, 1]) < 4.0)]

# 2. Sphere: |p - c|^2 = r^2 is linear in c and r^2 - |c|^2.
A = np.c_[2 * region, np.ones(len(region))]
sol = np.linalg.lstsq(A, (region ** 2).sum(axis=1), rcond=None)[0]
center = sol[:3]
radius = float(np.sqrt(sol[3] + center @ center))
error = np.abs(np.linalg.norm(region - center, axis=1) - radius).max()
print("sphere centre", center.round(6), "radius", round(radius, 6), "max error", round(float(error), 6))

# 3. Circle in the xy plane, here the rim of the flat at z 0: x^2 + y^2 = 2ax + 2by + k.
rim = pts[np.abs(pts[:, 2]) < 1e-6]
B = np.c_[2 * rim[:, 0], 2 * rim[:, 1], np.ones(len(rim))]
a, b, k = np.linalg.lstsq(B, (rim[:, :2] ** 2).sum(axis=1), rcond=None)[0]
print("circle centre", round(float(a), 6), round(float(b), 6), "radius", round(float(np.sqrt(k + a * a + b * b)), 6))

# 4. Derived: how far the ball reaches below the flat that cut it off.
floorZ = 0.0
print("missing cap height", round(radius - (center[2] - floorZ), 6))
```

- Print the selection's count and bounds before you fit. Select only vertices on the surface, and report the max error: above about 0.01 mm the region is not one sphere.

## Patch selected facets and keep the rest

Select the facets by a test, remove them, take the boundary loop, add new facets with the same direction along that loop, then prove the rest is unchanged.

```python
import numpy as np
from collections import Counter
import Mesh

mesh = Mesh.Mesh("DESIGN.stl")
pts = np.array([[p.x, p.y, p.z] for p in mesh.Points])
tri = np.array([f.PointIndices for f in mesh.Facets])

# 1. The facets to replace: every point on the plane z 0.
patch = np.where(np.abs(pts[tri][:, :, 2]).max(axis=1) < 1e-6)[0]

# 2. The boundary loop: edges of one selected facet only, kept in that facet's direction.
uses = Counter()
for a, b, c in tri[patch]:
    for e in ((a, b), (b, c), (c, a)):
        uses[tuple(sorted(e))] += 1
nxt = {}
for a, b, c in tri[patch]:
    for u, v in ((a, b), (b, c), (c, a)):
        if uses[tuple(sorted((u, v)))] == 1:
            nxt[u] = v
loop = [next(iter(nxt))]
while nxt[loop[-1]] != loop[0]:
    loop.append(nxt[loop[-1]])
assert len(loop) == len(nxt), "the boundary is not one loop"

# 3. New facets on the fitted sphere (the fit of the recipe above), in rings down to the lowest point.
region = pts[(pts[:, 2] > 0.01) & (np.hypot(pts[:, 0], pts[:, 1]) < 4.0)]
sol = np.linalg.lstsq(np.c_[2 * region, np.ones(len(region))], (region ** 2).sum(axis=1), rcond=None)[0]
center = sol[:3]
radius = float(np.sqrt(sol[3] + center @ center))
apex = center - np.array([0.0, 0.0, radius])
def onSphere(p):
    v = p - center
    return center + radius * v / np.linalg.norm(v)
steps = 3
rings = [pts[loop]] + [np.array([onSphere(p + (apex - p) * s / steps) for p in pts[loop]]) for s in range(1, steps)]
new = []
for outer, inner in zip(rings, rings[1:]):
    for i in range(len(outer)):
        j = (i + 1) % len(outer)
        new.append((outer[i], outer[j], inner[j]))
        new.append((outer[i], inner[j], inner[i]))
last = rings[-1]
for i in range(len(last)):
    new.append((last[i], last[(i + 1) % len(last)], apex))

# 4. Rebuild: the kept facets as they were, then the new ones, and weld the shared points.
keep = np.setdiff1d(np.arange(len(tri)), patch)
corners = lambda facet: [tuple(float(x) for x in p) for p in facet]
result = Mesh.Mesh([corners(pts[tri[i]]) for i in keep] + [corners(f) for f in new])
result.removeDuplicatedPoints()
result.removeDuplicatedFacets()

# 5. Check: closed, consistent, outward, and every facet outside the patch identical.
print("closed", result.isSolid(), "non-uniform facets", result.countNonUniformOrientedFacets(),
      "non-manifold", result.hasNonManifolds(), "volume", round(result.Volume, 6))
def fingerprint(facet):
    # A mesh stores float32 coordinates, so new facets are compared as float32 too.
    t = [tuple(round(float(np.float32(x)), 6) for x in p) for p in facet]
    i = t.index(min(t))
    return tuple(t[i:] + t[:i])
pts2 = [(q.x, q.y, q.z) for q in result.Points]
after = Counter(fingerprint([pts2[i] for i in f.PointIndices]) for f in result.Facets)
added = Counter(fingerprint(f) for f in new)
before = Counter(fingerprint(pts[tri[i]]) for i in keep)
print("facets outside the patch identical", after - added == before, "facets", len(patch), "->", len(new))
result.write("PATCHED.stl")
```

- A negative Volume means the new facets face inward: swap two corners of each.
- Call import_file on the written file, then analyze_mesh. Defects the mesh had before stay: repairing them changes facets outside the patch.

## Compare two meshes under a translation

Two meshes of one part laid out at different places differ by an offset. Remove the offset, then take the distance from each vertex to the nearest vertex of the other.

```python
import numpy as np
import Mesh

def points(path):
    return np.array([[p.x, p.y, p.z] for p in Mesh.Mesh(path).Points])

a, b = points("A.stl"), points("B.stl")
# The offset: the lowest corner of each. Use the mean of the points instead when the corners differ.
offset = a.min(axis=0) - b.min(axis=0)
b = b + offset

def nearest(src, dst):
    chunk = max(1, 2000000 // len(dst))
    out = np.empty(len(src))
    for i in range(0, len(src), chunk):
        out[i:i + chunk] = np.linalg.norm(src[i:i + chunk, None, :] - dst[None, :, :], axis=2).min(axis=1)
    return out

ab, ba = nearest(a, b), nearest(b, a)
print("offset", offset.round(6), "max %.8f mean %.8f" % (max(ab.max(), ba.max()), (ab.mean() + ba.mean()) / 2))
```

- Look both ways: a part missing from one mesh shows only from the larger one. A rotation is not an offset.
- Above about 100000 points the brute force is slow: use scipy.spatial.cKDTree if available.
