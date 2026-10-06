# Features

Sizes in mm. Hole allowances come from the material file; these add the shapes around them.

## Screw heads

Head sizes from the standards:

| Size | Socket head (ISO 4762) diameter / height | Countersunk 90 degree (ISO 10642) diameter / height |
|---|---|---|
| M3 | 5.5 / 3.0 | 6.72 / 1.86 |
| M4 | 7.0 / 4.0 | 8.96 / 2.48 |
| M5 | 8.5 / 5.0 | 11.20 / 3.10 |
| M6 | 10.0 / 6.0 | 13.44 / 3.72 |

- A bore that locates a part (a motor boss, a bearing, a pin that must not wobble) takes the sliding or locating fit of the material file, not the screw hole allowance. Read the Fits section of the material file.
- Counterbore for a socket head: M3 6.5 wide 3.6 deep, M4 8.3 / 4.7, M5 9.8 / 5.7, M6 11.3 / 6.8. The clearance hole below follows the material file.
- Countersink: a 90 degree cone, top diameter M3 7.2, M4 9.5, M5 11.7, M6 14.0, down to the clearance hole. The head then sits flush or just below.
- M2, M2.5 and other countersunk heads: no confirmed values. Ask the user for the screw, or print a coupon.
- Print a recess opening up when you can. A counterbore that opens down must bridge: model one 0.2 layer across the hole at the top of the recess, which the user pierces after printing, or tell the user to flip the part.
- Keep the material file's minimum wall under a recess. When the part is thinner than the recess depth plus that wall, thicken the part around the screw, or use a shallower recess and tell the user the head stands proud by the difference. Never leave less than the minimum wall.
- A recess or hole that opens sideways (its axis is horizontal in the print pose) is a horizontal hole: give the recess the same pointed top. Read Teardrop below.

## Captive nuts

Hex nuts, ISO 4032 (across flats / height in mm): M2 4 / 1.6, M2.5 5 / 2, M3 5.5 / 2.4, M4 7 / 3.2, M5 8 / 4.7, M6 10 / 5.2, M8 13 / 6.8.

- A captive-nut pocket is a hexagonal prism: Part::Prism with Polygon 6 and Circumradius = (across flats / 2 + the material file's per-side allowance) / cos 30 degrees. For an M3 nut in PETG: (5.5 / 2 + 0.2) / 0.866 = 3.406.
- Depth: the nut height + 0.2. Load the nut from the side or from the open face.

## D-shaft hole

A motor shaft with a flat: build the hole tool as a cylinder intersected with a box, then cut it from the part.

- Part::Cylinder with the shaft radius plus the allowance and Height longer than the part.
- Part::Box that keeps the flat: its width across the flats is the shaft's across-flats size plus the same allowance. Place it so one face lies at the flat.
- Part::Common with Base the cylinder and Tool the box. The result is the D-shaped tool.
- Part::Cut with Base the part and Tool the Common.
- The allowance applies to the flat as well as the round: across-flats plus allowance, not the exact across-flats.

## Teardrop

A horizontal hole needs a pointed top: a round top sags. Build the tool, then cut it from the part with Part::Cut. r is the hole radius with its allowance, L the hole length, (x0, y0, z0) the centre of the hole's first end.

- Hole along +x: a Part::Cylinder with Radius r, Height L, Placement Base (x0, y0, z0) and Rotation Axis (0, 1, 0) Angle 90. A Part::Box with Length L, Width r, Height r, Placement Base (x0, y0, z0) and Rotation Axis (1, 0, 0) Angle 45.
- Hole along +y: the Cylinder with Rotation Axis (1, 0, 0) Angle -90. The Box with Length r, Width L, Height r, Rotation Axis (0, 1, 0) Angle -45.
- Fuse the two with Part::Fuse and cut the fused tool.
- The box corner sits on the axis and its opposite corner points up. The tool is one solid, r below the axis and r * sqrt(2) above it, with its sides tangent to the circle. get_object on the fuse shows that size.

## Cable channels

A round cable pushed into a channel and held by its lips. No measured values exist for the opening: tell the user and print a coupon.

- Channel bore: cable diameter + 0.5 to 1.0.
- More than half of the cable sits below the lips: the bore centre lies below the opening.
- Opening: start 10 to 20 % under the cable diameter. Print a coupon with three openings and keep the one that holds.
- The lips flex, not the cable. Make them bend in the xy plane (layers along the lip), lip length about 8 to 10 times its thickness in PLA and 5 to 8 times in PETG, at least 1.0 thick, a fillet at the lip root of at least half its thickness, and a 25 to 35 degree lead-in at the opening.
- PLA lips are for one install. Clips the user opens often go in PETG.

## Snap fits

A cantilever clip: a beam with a hook that flexes past a lip and snaps back. No measured allowable strain exists for printed PETG or PLA: use the conservative design strain below and print a coupon pair first.

- Strain of a constant-section beam = 1.5 * t * Y / L^2 (t root thickness, Y deflection = the hook undercut, L beam length). So Y max = strain * L^2 / (1.5 * t).
- A beam tapered to half its thickness at the tip bends about 60 % further for the same strain (Y = 1.09 * strain * L^2 / t): prefer it.
- Design strain: PETG 2 %, PLA 1 % to 1.5 %. Half of that for a beam printed upright (layers across its length). Use 60 % of it for a clip opened often.
- Print the beam flat on the bed, layers along its length. Length 8 to 10 times t. Root at least 1.2 thick (3 perimeters), all perimeters, and a root fillet of 0.5 to 1 x t, at least 0.8: never a sharp root.
- Hook: undercut 0.5 to 1.2. Insertion face 30 degrees. Retention face about 60 degrees for a lid that opens again, 90 for a permanent catch.
- Window clearance around the hook: the material file's latch gap (PETG 0.5 per side); 0.3 per side on faces that do not lock.
- Worked PETG example: t 1.6, L 16, width 8, undercut 1.0. Strain = 1.5 * 1.6 * 1.0 / 256 = 0.94 %, under the 2 %. The largest undercut at 2 % is 0.02 * 256 / 2.4 = 2.1.

## Text

Emboss or engrave text with Draft::ShapeString.

- create_object {"obj_type": "Draft::ShapeString", "obj_properties": {"String": "PSU 12V", "Size": 8}}. Size is roughly the cap height in mm. FontFile is optional: the default is a bold sans font, and the reply names it. A script font or a serif font does not print.
- The text lies in the XY plane of its Placement: set Placement to the plane of the target face.
- Make it solid with Part::Extrusion {"Base": "ShapeString", "Dir": {"x": 0, "y": 0, "z": 0.6}, "Solid": true}. Dir is in global axes: point it along the face normal for the emboss height, against it for the engrave depth.
- Join it to the part: Part::Fuse to emboss, Part::Cut (the part as Base, the extrusion as Tool) to engrave. A Body works as the Base of a Part boolean: build the text after the Body is finished.
- Height at least 5, bold; 10 reads reliably. Stroke at least 2 x the nozzle (0.8, 1.0 is safer). Normal letter spacing.
- Emboss 0.4 to 0.6. Engrave 0.6, or 0.6 to 1.0 for contrast. Engraving prints more reliably than embossing.
- Text on a vertical wall resolves less well across the layers: make it larger there.

## Edges to round

- Edges on the plate: chamfer 0.3 to 0.5 at 45 degrees, never a fillet.
- Outer vertical edges: fillet freely. Top edges: fillet, or chamfer where the edge overhangs.
- Entry edges of holes and slots where a screw, part or cable goes in: chamfer 0.3 to 0.5.
- Never round the faces of a hole or slot that locate or hold a part: it changes the fit.
- Inside corners that carry load: fillet at least half the wall thickness.
- A radius under 0.5 does not print with a 0.4 nozzle: leave the edge sharp.
