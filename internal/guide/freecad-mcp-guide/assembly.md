# Assemblies

Build each part of an assembly in its own App::Part, then move the Parts to assemble them.

## Parts

- create_object with obj_type App::Part, one for each part, named after it.
- Put objects in a Part with Group on the Part: update_object with {"Group": ["Leaf", "Pin"]}, or the same Group in create_object. A Body goes in the same way.
- Group is the whole list: name every member each time. An object leaves a Part when its name is left out.
- An object is in one Part only. FreeCAD refuses a second one ("Object can only be in a single GeoFeatureGroup"): leave the first Part, then join the other.
- Keep each object's own Placement as modelled. Set the Part's Placement to put the part in the assembly: it moves everything in it.
- A Part is one part to check_printability and export_document, and its shape is all its members together.

## Positions

- get_object, list_subelements, measure, check_printability and the Shape line of a reply are global: they count the Placement of every Part above the object. get_object adds LocalBoundBox when a Part moves the object.
- The Placement property of an object stays its own, not the global one.

## Clearances

- measure with kind distance and the two Parts as refs (or one face of each) gives the smallest distance between them and the closest points. Compare it with the gap in the material file. It gives one distance for a pair: measure the pair of faces that matter.
- Faces that face each other along z need at least two layer heights. Read the material file.

## Print layout

- The assembly pose is not the print pose. Make the layout in a copy: save_document_as with copy true, open_document on the copy, then set each Part's Placement there so its lowest point is z 0 and the Parts are spaced out. The assembly document stays as it is. Save the copy or close it with discard_changes true.
- Or move the Parts in the one document, export, and move them back.
- check_printability lists each Part as one part and warns when a Part floats, its lowest point above z 0.
- export_document with per_object true, object_names the Parts, format step or stl and a folder as path writes one file for each Part. A .3mf of the Parts holds one object for each.
- An object inside a moved Part exports at its global place too, in every format, so a member and its Part land in the same spot. Exporting the Part keeps all its members together.
