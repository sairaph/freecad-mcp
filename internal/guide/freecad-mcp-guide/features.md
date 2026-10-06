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

- Counterbore for a socket head: M3 6.5 wide 3.6 deep, M4 8.3 / 4.7, M5 9.8 / 5.7, M6 11.3 / 6.8. The clearance hole below follows the material file.
- Countersink: a 90 degree cone, top diameter M3 7.2, M4 9.5, M5 11.7, M6 14.0, down to the clearance hole. The head then sits flush or just below.
- M2, M2.5 and other countersunk heads: no confirmed values. Ask the user for the screw, or print a coupon.
- Print a recess opening up when you can. A counterbore that opens down must bridge: model one 0.2 layer across the hole at the top of the recess, which the user pierces after printing, or tell the user to flip the part.

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
