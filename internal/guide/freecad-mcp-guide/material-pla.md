# PLA

Design rules for FDM parts in PLA with a 0.4 mm nozzle. Sizes in mm. "Per side" is the gap on each face. The values are starting points: settle them for the user's printer with the test coupon below. Keep each clearance in a Params cell (read parametric.md), so a coupon result changes the whole model.

## Accuracy

- PLA shrinks least of the common filaments: holes print 0.1 to 0.2 undersize on diameter; outer faces print 0.05 to 0.1 oversize.
- A hole for a bought part (screw, rod, bearing; not a heat-set insert, see Threads): model a vertical hole (axis along z) at nominal + 0.2 on diameter. A horizontal hole sags at the top: above 3 diameter model a teardrop with its 45 degree point up, its round part at the vertical hole allowance (nominal + 0.2); below 3, nominal + 0.3.
- Two printed parts that fit together: model the gaps under Fits. They already cover the print error of both parts; do not add the hole allowance as well.
- The first layer flares 0.2 to 0.5 outward: chamfer every bottom outer edge 0.5 x 45 degrees.

## Fits

- Sliding (slots, rails, dovetails): 0.20 per side.
- Rotating (a pin turning in a hole): 0.25 per side.
- Faces that face each other along z in a print-in-place joint (the ends of hinge knuckles): a gap of at least two layer heights (0.4 at 0.2 mm layers) as a starting point. Print a coupon first.
- Press: aim for 0.1 to 0.2 interference on diameter at printed size; more cracks PLA. Model the hole at pin + 0.05 on diameter for a metal pin, at pin + 0.1 for a printed pin.
- Avoid clips in PLA: it is brittle (under 2 % stretch across layers) and a held clip creeps loose. Use screws, or PETG for the clip. A PLA clip that cannot be avoided is single use, with a design strain of 1 % to 1.5 % (half for a beam printed upright): read features.md Snap fits. No latch gap is tested for PLA: use 0.5 per side and print a coupon.

## Threads

- Profile: 45 degree flanks with a flat crest and a flat root, each at least 0.5 wide. Never a sharp V.
- Clearance on both parts, never only one: nut +0.17 and bolt -0.13 on the radius (0.30 radial in total).
- Engagement, the thread depth minus the total radial clearance, at least 0.6: thread depth at least 1.0.
- Pitch at least 2 x depth + 1.0, which leaves the two flats. M10 x 3 with depth 1.0 works; M10 x 2 with depth 1.0 leaves no flats and jams.
- Smallest reliable thread M8 x 3 with depth 1.0. Below that use heat-set inserts or captive nuts.
- Print both parts with the thread axis vertical. Chamfer both entries 1.0 x 45 degrees.
- PLA threads crack at the root under torque: use heat-set inserts for anything tightened with a tool or opened often.
- Heat-set inserts: ask the user for the brand and length and use its drawing when given. Otherwise, for the common standard series (hole / length / minimum wall): M2 3.2 / 3.0 / 1.3, M2.5 4.0 / 4.0 / 1.6, M3 4.0 / 5.7 / 1.6, M4 5.6 / 8.1 / 2.1, M5 6.4 / 9.5 / 2.6, M6 8.0 / 12.7 / 3.3, M8 9.7 / 12.7 / 4.5. Straight hole, depth the insert length + 1.0, a 0.5 chamfer at the entry. Do not add the hole allowance: these sizes are for the printed hole. Test-fit one insert; if it will not go in, open the hole by 0.2. A part thinner than the insert length plus 1.0 needs a boss under the insert or a shorter insert.
- Captive nut: pocket at the nut's flats + 0.2 per side, loaded from the side.

## Strength

- Across layers PLA holds about 77 % of its in-plane strength and breaks suddenly, with almost no stretch.
- Lay a part loaded in bending so the bending runs along the layer lines, not across them. Never load a thin stem across its layers.
- Walls: 0.8 minimum (two lines), 1.6 for structural and threaded walls. Use multiples of 0.4.

## Heat and creep

- Softens near 55 to 60 C: keep service below 45 C. Never in a car, a sunny window or next to a warm motor.
- PLA creeps more than the other common filaments: a press fit, a clamp or a screw preload relaxes within days. Design clamps to close onto a hard stop and put a washer or metal sleeve under screw heads.

## Limits

- Minimum wall and feature 0.8; minimum hole 1.0 vertical, 2.0 horizontal.
- Overhangs up to 45 degrees from vertical without supports.
- Bridges up to 10 print cleanly; beyond that add a rib or chamfer the roof at 45 degrees.
- Outer edges rounded or chamfered at least 0.5; inside corners radius at least 0.5, since PLA cracks from sharp inside corners.

## FEM

- Use the brand's datasheet when the user has one. Otherwise, for a solid (100 % infill) PLA part, isotropic: {"Name": "PLA", "YoungsModulus": "2.3 GPa", "PoissonRatio": 0.36, "Density": "1240 kg/m^3"}. The Poisson's ratio is assumed.
- Strength: about 51 MPa along the layers and about 40 MPa across them at best. These are best-case datasheet values; a real part is weaker across layers.
- Check the stress against strength divided by a safety factor of 2 to 3 along the layers and 4 or more where the load pulls layers apart.
- Linear FEM shows neither creep nor heat softening. A part held under load for long, or near PLA's softening (55 to 60 C), needs margin beyond the result.
- Sparse infill: these numbers do not apply. Check only the walls, or ask for solid infill.
- The result is a check, not a guarantee. Read fem.md.

## Test coupon

Print these small samples on the user's printer before large parts, then set the Params cells to the ones that fit best:
- hole ladder 3, 5 and 10 diameter at +0.0 to +0.5 in steps of 0.1, vertical and horizontal
- pin and slot at 0.10, 0.20 and 0.30 per side
- two knuckle ends facing along z at 0.2, 0.4 and 0.6 gap, printed in place
- press pair: a printed pin in holes at pin + 0.0 to + 0.3 on diameter in steps of 0.1
- the part's thread: nut and bolt 10 tall at 0.20, 0.30 and 0.40 radial gap, printed upright
