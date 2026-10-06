# FEM from analysis to result

Solve a static stress problem with CalculiX. Create every FEM object with create_object and pass analysis_name so it joins the analysis.

## Steps

1. The solid exists. Note its object name, for example Body.
2. Create the analysis:
   {"obj_type": "Fem::AnalysisPython", "obj_name": "Analysis"}
3. Create the material. For a printed part take the values from the FEM section of its material file (material-petg.md, material-pla.md), not from steel:
   {"obj_type": "Fem::MaterialCommon", "obj_name": "Steel", "analysis_name": "Analysis", "obj_properties": {"Material": {"Name": "Steel", "YoungsModulus": "210 GPa", "PoissonRatio": 0.3, "Density": "7900 kg/m^3"}}}
4. Create the mesh. It is generated when created, second order:
   {"obj_type": "Fem::FemMeshGmsh", "obj_name": "Mesh", "analysis_name": "Analysis", "obj_properties": {"Shape": "Body", "CharacteristicLengthMax": 5, "CharacteristicLengthMin": 1}}
5. Call list_subelements on the solid. Pick the fixed face and the loaded face.
6. Create a fixed support:
   {"obj_type": "Fem::ConstraintFixed", "obj_name": "Fixed", "analysis_name": "Analysis", "obj_properties": {"References": [{"object_name": "Body", "faces": ["Face5"]}]}}
7. Create the load, a force or a pressure:
   {"obj_type": "Fem::ConstraintForce", "obj_name": "Load", "analysis_name": "Analysis", "obj_properties": {"References": [{"object_name": "Body", "faces": ["Face3"]}], "Force": "500 N"}}
   {"obj_type": "Fem::ConstraintPressure", "obj_name": "Pressure", "analysis_name": "Analysis", "obj_properties": {"References": [{"object_name": "Body", "faces": ["Face3"]}], "Pressure": "2 MPa"}}
8. Call run_fem_analysis with doc_name and analysis_name.

## Rules

- Force and Pressure are strings with units. A bare number is in base units. Read values.md.
- CharacteristicLengthMax and Min are in mm. A smaller size gives more elements and a longer run. Start near one tenth of the smallest part dimension.
- A Fem::ConstraintForce acts along the outward normal of its face: a force on a top face acts up. Set Reversed true to push down into the face. A Fem::ConstraintPressure acts into its face, against the outward normal. Reversed true flips either.
- The reply of create_object and update_object for a load states the direction as a vector and in words, for example "Force 500.00 N acts along (0, 0, 1): the outward normal of Face3." Before a face is referenced the reply says no direction is known yet. Check it against what you meant. If a load acts the wrong way, call update_object with Reversed true on it.
- The analysis needs one material, one mesh, at least one fixed support and at least one load.
- The mesh is second order by default. Keep it: first order tetrahedra lock in bending and give stress and deflection about three times too small. A mesh of first order gets a warning from run_fem_analysis. Set ElementOrder to 2nd with update_object to mesh it again.
- run_fem_analysis meshes a Gmsh mesh again when its solid changed since it was meshed, and says so. update_object on a mesh that changes its Shape or a meshing parameter (CharacteristicLengthMax, ElementOrder) meshes it at once and gives the node count.
- Constraints must reference the solid the mesh meshes. After you replace the solid (a Part::Cut of the old one, say), point the mesh Shape at the new solid with update_object (it meshes again), then set the References of each constraint to faces of the new solid from list_subelements. The run refuses constraints that still name the old one.
- References and the mesh Shape take the solid itself, never an App::Link to it: FreeCAD crashes on a link there (any link kind), so the call is refused and names the object the link points to.
- A force along an edge, or normal to a planar face: pass Direction, for example "Direction": {"object_name": "Body", "edge": "Edge5"}. The reply states the direction.

## Check the result

- Compare with a hand estimate before you trust a number. A cantilever of length L, width b and thickness h, fixed at one end, with a total load F spread along it (w = F / L): the largest bending stress is 6 M / (b h^2) with M = F L / 2, and the tip deflection is w L^4 / (8 E I) with I = b h^3 / 12. Example: 60 x 30 x 4 mm, 200 N, aluminium (70 GPa): 75 MPa and 0.48 mm. A second order mesh gives within about 6 % of that, a first order one a third.
- The fixed faces you choose change the result: fix only the face the real part is held by.
- A sharp inside corner gives a stress peak that grows as the mesh gets finer, with no limit. Round the corner, or read the stress a little away from it.
- A result of all zeros fails the run: no load reached the mesh. Check the loaded face with list_subelements.

## Read the result

- run_fem_analysis returns max and min von Mises stress in MPa, max displacement in mm, where each peak is (the position of its node, in mm: "max von Mises = 223.3 MPa at (30, 7.5, 8)"), the node count, the result object, the working directory, and every load with its magnitude and direction.
- Its screenshot is coloured by von Mises stress, the way FreeCAD shows a result, and the reply gives the range of the colour scale in MPa. The solid and the mesh are hidden so the result shows. Show one again with update_object and {"ViewObject": {"Visibility": true}}. A shown solid covers the stress colours: hide it again, or use get_view to look. The colour bar is labelled in Pa (1e6 Pa = 1 MPa).
- To look at the result, keep the solid hidden (the run hides it) and use set_view isolate on the result object, not on the solid: a shown solid covers the colours.
- The colours need a 3D view. Without one the reply carries only the numbers.
- Compare max von Mises stress with the yield strength of the material. Report the ratio.
- For a printed part the strength is the material file's, along or across the layers, divided by its safety factor.
- Tell the user the load, the material and the mesh size with the result.

## Errors

- A constraint naming a face that no longer exists is an error naming the constraint and the face. Call list_subelements on the solid and set its References again.
- A prerequisite error names what is missing. Create it with create_object and analysis_name, then run again.
- A solver error names the working directory. The solver output files are there.
- A timeout does not stop the solver. Call get_rpc_status, then run again with a larger timeout when needed.
- run_fem_analysis blocks FreeCAD's GUI thread until it finishes. Send no other call meanwhile. get_rpc_status still answers.
