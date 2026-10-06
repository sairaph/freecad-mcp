# PETG

Design rules for FDM parts in PETG with a 0.4 mm nozzle. Sizes in mm. "Per side" is the gap on each face. The values are starting points: settle them for the user's printer with the test coupon below. Keep each clearance in a Params cell (read parametric.md), so a coupon result changes the whole model.

## Accuracy

- Holes print 0.1 to 0.3 undersize on diameter; outer faces print 0.05 to 0.15 oversize.
- A hole for a bought part (screw, rod, bearing; not a heat-set insert, see Threads): model a vertical hole (axis along z) at nominal + 0.3 on diameter. A horizontal hole sags at the top: above 3 diameter model a teardrop with its 45 degree point up, its round part at the vertical hole allowance (nominal + 0.3); below 3, nominal + 0.4.
- Two printed parts that fit together: model the gaps under Fits. They already cover the print error of both parts; do not add the hole allowance as well.
- The first layer flares 0.2 to 0.5 outward: chamfer every bottom outer edge 0.5 x 45 degrees.

## Fits

- Sliding (slots, rails, dovetails): 0.25 per side.
- Rotating (a pin turning in a hole): 0.30 per side; 0.35 under side load. PETG on PETG grips.
- Faces that face each other along z in a print-in-place joint (the ends of hinge knuckles): a gap of at least two layer heights (0.4 at 0.2 mm layers) as a starting point. Print a coupon first.
- Press: aim for 0.1 to 0.2 interference on diameter at printed size, never more than 0.25. Model the hole at pin + 0.1 on diameter for a metal pin, at pin + 0.2 for a printed pin.
- Clips work in PETG: design strain 2 % (1.2 % for a clip opened often, half for a beam printed upright). Gap 0.5 per side around a latch, beam at least 5 wide, root fillet at least half the beam thickness, and the beam bending in the xy plane, never across layers. Read features.md Snap fits.

## Threads

- Profile: 45 degree flanks with a flat crest and a flat root, each at least 0.5 wide. Never a sharp V.
- Clearance on both parts, never only one: nut +0.20 and bolt -0.15 on the radius (0.35 radial in total).
- Engagement, the thread depth minus the total radial clearance, at least 0.6: thread depth at least 1.0.
- Pitch at least 2 x depth + 1.0, which leaves the two flats. M10 x 3 with depth 1.0 works; M10 x 2 with depth 1.0 leaves no flats and jams.
- Smallest reliable thread M8 x 3 with depth 1.0. Below that use heat-set inserts or captive nuts.
- Print both parts with the thread axis vertical. Chamfer both entries 1.0 x 45 degrees.
- Heat-set inserts: ask the user for the brand and length and use its drawing when given. Otherwise, for the common standard series (hole / length / minimum wall): M2 3.2 / 3.0 / 1.3, M2.5 4.0 / 4.0 / 1.6, M3 4.0 / 5.7 / 1.6, M4 5.6 / 8.1 / 2.1, M5 6.4 / 9.5 / 2.6, M6 8.0 / 12.7 / 3.3, M8 9.7 / 12.7 / 4.5. Straight hole, depth the insert length + 1.0, a 0.5 chamfer at the entry. Do not add the hole allowance: these sizes are for the printed hole. Test-fit one insert; if it will not go in, open the hole by 0.2. A part thinner than the insert length plus 1.0 needs a boss under the insert or a shorter insert.
- Captive nut: pocket at the nut's flats + 0.2 per side, loaded from the side.

## Strength

- Across layers PETG holds about 84 % of its in-plane strength and stretches less than half as far.
- Lay a part loaded in bending so the bending runs along the layer lines, not across them.
- Walls: 0.8 minimum (two lines), 1.6 for structural and threaded walls. Use multiples of 0.4.

## Heat and creep

- Softens near 70 to 80 C: keep service below 60 C. A car in the sun or a hot window is too hot.
- Bolted joints lose preload: put a washer or metal sleeve under the head. Clamps close onto a hard stop, not onto their own preload.

## Limits

- Minimum wall and feature 0.8; minimum hole 1.5 vertical, 2.5 horizontal.
- Overhangs up to 45 degrees from vertical without supports; 40 for clean faces.
- Bridges sag beyond 5: add a rib or chamfer the roof at 45 degrees.
- Outer edges rounded or chamfered at least 0.5; inside corners radius at least 0.5.

## Traps

- PETG strings across gaps under 3; keep such gaps away from faces that must stay clean.
- Large parts need a flat base at least 1 thick to come off the bed whole.
- Sliding faces stick: narrow the contact to ribs.

## FEM

- Use the brand's datasheet when the user has one. Otherwise, for a solid (100 % infill) PETG part, isotropic: {"Name": "PETG", "YoungsModulus": "1.5 GPa", "PoissonRatio": 0.39, "Density": "1270 kg/m^3"}. The Poisson's ratio is assumed.
- Strength: about 47 MPa along the layers and about 40 MPa across them at best. These are best-case datasheet values; a real part is weaker across layers.
- Check the stress against strength divided by a safety factor of 2 to 3 along the layers and 4 or more where the load pulls layers apart.
- Linear FEM shows neither creep nor heat softening. A part held under load for long (above about 30 % of its strength) needs margin beyond the result.
- Sparse infill: these numbers do not apply. Check only the walls, or ask for solid infill.
- The result is a check, not a guarantee. Read fem.md.

## Test coupon

Print these small samples on the user's printer before large parts, then set the Params cells to the ones that fit best:
- hole ladder 3, 5 and 10 diameter at +0.0 to +0.5 in steps of 0.1, vertical and horizontal
- pin and slot at 0.15, 0.25 and 0.35 per side
- two knuckle ends facing along z at 0.2, 0.4 and 0.6 gap, printed in place
- press pair: a printed pin in holes at pin + 0.0 to + 0.3 on diameter in steps of 0.1
- the part's thread: nut and bolt 10 tall at 0.30, 0.35 and 0.40 radial gap, printed upright
