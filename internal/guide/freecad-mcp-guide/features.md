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

## Edges to round

- Edges on the plate: chamfer 0.3 to 0.5 at 45 degrees, never a fillet.
- Outer vertical edges: fillet freely. Top edges: fillet, or chamfer where the edge overhangs.
- Entry edges of holes and slots where a screw, part or cable goes in: chamfer 0.3 to 0.5.
- Never round the faces of a hole or slot that locate or hold a part: it changes the fit.
- Inside corners that carry load: fillet at least half the wall thickness.
- A radius under 0.5 does not print with a 0.4 nozzle: leave the edge sharp.
