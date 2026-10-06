# PartDesign

Build one solid from sketches: a Body holds the sketches and features in order, and each feature builds on the one before. Use it for a part made of a profile, pads, pockets and rounded edges. Use Part (Part::Box, Part::Cut, Part::Fillet) for primitives, booleans of separate solids and anything without a sketch.

## Steps

1. Create the Body:
   {"obj_type": "PartDesign::Body", "obj_name": "Body"}
2. Create the sketch with its geometry, on an origin plane of the Body:
   {"obj_type": "Sketcher::SketchObject", "obj_name": "Sketch", "obj_properties": {"AttachmentSupport": "XY_Plane", "Geometry": [{"rectangle": {"corner": [0, 0], "size": [40, 40]}}]}}
3. Create the Pad:
   {"obj_type": "PartDesign::Pad", "obj_name": "Pad", "obj_properties": {"Profile": "Sketch", "Length": 10}}
4. Call list_subelements on the Pad. Create a sketch on its top face with a circle, then cut it:
   {"obj_type": "Sketcher::SketchObject", "obj_name": "HoleSketch", "obj_properties": {"AttachmentSupport": {"object_name": "Pad", "face": "Face6"}, "Geometry": [{"circle": {"center": [20, 20], "radius": 7.5}}]}}
   {"obj_type": "PartDesign::Pocket", "obj_name": "Pocket", "obj_properties": {"Profile": "HoleSketch", "Type": "ThroughAll"}}
5. Round the top edges of the last feature. Call list_subelements with kind edges first:
   {"obj_type": "PartDesign::Fillet", "obj_name": "Fillet", "obj_properties": {"Base": "Pocket", "Edges": ["Edge4", "Edge7", "Edge10", "Edge13"], "Radius": 1.5}}

## Body

- Every PartDesign feature and every sketch for a Body goes into a Body. The reply names the Body and its Tip, the feature the next one builds on.
- The Body is body_name when you pass it. Else it is the Body the Profile, Base or AttachmentSupport points into. Else it is the document's only Body. With several Bodies and nothing to tell them apart, the call is refused and lists them: pass body_name.
- A sketch goes into a Body when body_name is given or its AttachmentSupport is a feature of the Body, a face of one, or its origin plane. Any other sketch stays outside.
- A new feature becomes the Tip. Build the next feature on it.

## Sketch

- AttachmentSupport takes the forms of References: "XY_Plane", {"object_name": "Pad", "face": "Face6"}, ["Pad", "Face6"] or [["Pad", ["Face6"]]]. Support is accepted as the older name.
- "XY_Plane", "XZ_Plane" and "YZ_Plane" mean the origin planes of the Body the sketch goes into. With several Bodies pass body_name too.
- MapMode is FlatFace unless you set it.
- Geometry is a list, in mm in the sketch's own x and y:
  - {"line": [[x1, y1], [x2, y2]]}
  - {"circle": {"center": [x, y], "radius": r}}
  - {"arc": {"center": [x, y], "radius": r, "start_angle": 0, "end_angle": 90}} runs counter-clockwise, in degrees.
  - {"rectangle": {"corner": [x, y], "size": [w, h]}} adds four lines with their corners joined.
  - {"slot": {"center1": [x, y], "center2": [x, y], "width": w}} adds two lines and two arcs, joined: a closed slot whose caps are centred on the two points. The centres must differ and width must be above 0.
- update_object with Geometry replaces all the geometry of the sketch, and its constraints with it. Constraints cannot be set with this tool.
- The reply says how many geometry elements the sketch has and whether the profile is closed. A Pad or Pocket needs a closed profile. Lines of a profile must meet at their ends.
- A face of the Pad changes its number when the part changes. Call list_subelements again before attaching a new sketch.

## Features

- Pad: Profile, Length. A Pocket cuts: Profile, Length, or Type "ThroughAll" to cut through the whole part.
- A Pocket cuts against the sketch's normal, so a sketch on the top face of the part cuts down into it. A sketch on XY_Plane at the part's bottom (normal up, part above) cuts nothing: set Reversed true and it cuts up into the part. Measured: Length 3 from the top face removes 300 mm^3 of a 10 x 10 profile, the same sketch from XY_Plane under the part removes 0 and with Reversed true 300.
- Fillet and Chamfer: Base, Edges ["Edge1"], Radius (Size for a chamfer). They take one value for all their edges: make a second feature for another size. update_object with Edges sets the edges again.
- Round edges last. Face and edge numbers change when an earlier feature changes.
- Never round the cut edge of a hole that a screw or part locates in. Read features.md.

## Placing a Body

- Lay the whole part out with the Body's Placement: update_object on the Body, for example to stand it up or move it onto the plate. Never move or rotate its features one by one: they follow the Body and the sketches stay on their planes.

## Driving a part from a sheet

- Sketch Geometry is fixed numbers: the tools set no dimensional constraints, so a cell cannot move a sketch line or change a circle.
- To drive a part from a Spreadsheet::Sheet, build it from the primitives, which take expressions: PartDesign::AdditiveBox, AdditiveCylinder, AdditiveSphere, AdditiveCone, AdditiveEllipsoid, AdditiveTorus, AdditivePrism, AdditiveWedge and the Subtractive ones with the same names. create_object with body_name (or the only Body) puts them in the Body and each cuts or adds at once.
- Bind their size and place: {"Length": "=Params.platelength", "Width": "=Params.platewidth", "Height": "=Params.thick"} for a box, {"Radius": "=Params.holeradius", "Height": "=Params.thick"} for a cylinder, and {"Placement.Base.x": "=Params.holex", "Placement.Base.y": "=Params.holey"} to place it. Read parametric.md.
- Bind a sketch the same way through its AttachmentOffset: {"AttachmentOffset.Base.x": "=Params.dx"} moves the whole sketch, so a profile slides with a cell.
- A plate with two holes: an AdditiveBox for the plate and a SubtractiveCylinder for each hole, with the hole positions in cells. Changing a cell and update_spreadsheet_cells then moves the holes and resizes the plate; the reply gives the Body's new shape.

## Errors

- "needs a Body": create the Body first, or pass body_name.
- An empty Profile or "Linked shape object is empty": the sketch has no closed geometry yet. Set its Geometry with update_object.
- A feature that does not compute stays in the Body. Fix it with update_object or remove it with delete_object.
