# Printing

## Material

Model holes, fits, threads and clips for the material the part prints in. Read its material file first (material-petg.md, material-pla.md) and ask the user for the material when it is not known. For a rigid material without a file, say so and use the closest file's values with the test coupon it describes. For a flexible material such as TPU, tell the user that no values exist and ask for theirs.

## Lay out

Print each part in the pose it prints in. Lay every part flat on the plate with its Placement: the face that sits on the plate at z 0, the part inside x 0 to the plate width and y 0 to the plate depth, with a few mm between parts.

- Set the pose with update_object on Placement, or in execute_code with the part's Placement.
- A part is laid flat by rotating it about a horizontal axis by an angle that is not 0, then moving it so its lowest point is z 0.
- A part that must not be laid flat because of its layers (a thin tab, a clip) is rotated to the pose the user names.
- Parts that were modelled in an assembly pose keep that pose in the assembly; make a second document or copies for the plate layout.
- For a second plate laid beside the first, keep its parts inside that plate's own rectangle and call check_printability with the same bed_x and bed_y and the plate's corner in bed_origin_x and bed_origin_y.

## Round edges

- Edges that sit on the plate get the chamfer the material file asks for: it stops the first layer flaring. Every other edge follows the user's request, so "round all edges" means round all but the bottom ones, which get the chamfer. Tell the user.
- Find them with list_subelements and kind edges: the ones that sit on the plate say "on the bottom". A "degenerate" edge has no length: never pick it. Each line also gives its start and end, and "along z" or "along x" for a line parallel to an axis.
- A hollow part: round and chamfer the outer solid first, then cut the cavity. A fillet on a hollowed solid fails, or needs the inner edges too. Read features.md for which inner edges to round.
- With the tools: create_object with obj_type Part::Fillet (or Part::Chamfer) and obj_properties {"Base": "Box", "Edges": ["Edge1", "Edge2"], "Radius": 1} (Chamfer: "Size"), or "Edges": [{"edge": "Edge1", "radius": 2}] for sizes that differ. A Fillet without Edges is refused. The reply lists the edges and sizes now, and names the objects it hid (booleans, compounds, Fillet, Chamfer, Extrusion, Revolution and Thickness hide their inputs, as FreeCAD does). update_object with a new Radius resizes the listed edges. The size is a number: FreeCAD cannot bind it to a spreadsheet cell.
- In a script: one or two makeFillet calls, one for each set of edges with the same radius. Make one makeChamfer call for the edges a fillet fails on.
- Do not retry edge by edge or search the shape for edges again after each try. Read code.md, and run the script with execute_code_headless in the background when it may take minutes.

## Check

Call check_printability before every export for a printer, with bed_x and bed_y in mm (bed_z for the build height).

It reports per part its tight size, its free margin to each plate edge, and which parts it overlaps. It does no meshing, so it is fast. It checks solids and meshes.

- printable is true only when something was checked, every part lies inside the plate and no two parts overlap.
- Space the parts apart before you call it. Overlapping complex parts, such as threads, make the intersection slow.
- Overlap of two solids is the volume of their shared solid. A solid resting in another solid's cavity does not overlap it.
- A mesh is checked by its bounding box, so a part nested in a mesh's box counts as overlapping. Space mesh parts apart. Read mesh.md for working on meshes.
- When it reports a part outside the plate or an overlap, move the part with update_object on Placement, then call it again.
- Tell the user the size of each part and whether the layout fits.

## Fix

- Invalid or open shape from a failed feature: call recompute_document, then fix the object it names with update_object.
- Mesh object with defects: call analyze_mesh. It lists the repair_mesh steps that fix them. Call repair_mesh with those steps.
- Mesh that must become a solid: call mesh_to_solid. Above 200000 facets pass force true. An open mesh gives a shell, so repair first.
- Too big for the plate: change the dimensions with update_object, or split the part.

## Export

- Call export_document with a .stl or .3mf path.
- 3MF keeps one object per part, named after its label, and declares mm. Slicers prefer it.
- Names and labels differ when a name had to be changed: FreeCAD names hold no space, so create_object with obj_name "Base Plate" gives the name Base_Plate and the label Base Plate. Tools take the name; the file and the export reply show the label, and the reply names the object too.
- For one file per part, pass per_object true, object_names, format stl (or step) and a folder as path. Each file is named after its object's label and the reply lists every file.
- quality is coarse for previews, standard for FDM, fine for resin and small curved parts. linear_deflection and angular_deflection_deg override it.
- Pass overwrite true when the file exists.
- Call solid_to_mesh to inspect the triangles a printer will get. Export does not need it.
