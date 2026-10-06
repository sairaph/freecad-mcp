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
- update_object with Geometry replaces all the geometry of the sketch, and its constraints with it. Constraints cannot be set with this tool.
- The reply says how many geometry elements the sketch has and whether the profile is closed. A Pad or Pocket needs a closed profile. Lines of a profile must meet at their ends.
- A face of the Pad changes its number when the part changes. Call list_subelements again before attaching a new sketch.

## Features

- Pad: Profile, Length. A Pocket cuts: Profile, Length, or Type "ThroughAll" to cut through the whole part.
- Fillet and Chamfer: Base, Edges ["Edge1"], Radius (Size for a chamfer). They take one value for all their edges: make a second feature for another size. update_object with Edges sets the edges again.
- Round edges last. Face and edge numbers change when an earlier feature changes.
- Never round the cut edge of a hole that a screw or part locates in. Read features.md.

## Errors

- "needs a Body": create the Body first, or pass body_name.
- An empty Profile or "Linked shape object is empty": the sketch has no closed geometry yet. Set its Geometry with update_object.
- A feature that does not compute stays in the Body. Fix it with update_object or remove it with delete_object.
