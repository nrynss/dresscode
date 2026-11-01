/** The occasion tiles' icons, drawn for this screen: simple original line art
 * on a 24-box, a few paths each, stroked in the accent the tile already
 * carries. No icon set is embedded and no path data is copied; each figure is
 * read the same way the tile's own text is — a date lifts a glass, a family
 * function lights a diya, an interview carries a briefcase, a festival hangs
 * a lantern, a funeral lays a leaf. */
export const occasionIcons: Record<string, string[]> = {
  // A wine glass with one small sparkle beside the rim.
  date: [
    "M7.5 3.5h9v1.6c0 3.6-1.9 6.4-4.5 6.4s-4.5-2.8-4.5-6.4V3.5Z",
    "M12 11.5v6",
    "M8.5 20.5h7",
    "M19.75 4.25v3M18.25 5.75h3",
  ],
  // A diya: the flame over a shallow oil bowl on its foot.
  "family-function": [
    "M12 4.25c1.75 1.55 2.6 2.95 2.6 4.35a2.6 2.6 0 1 1-5.2 0c0-1.4.85-2.8 2.6-4.35Z",
    "M4.75 13.75h14.5",
    "M6.25 13.75c.35 2.6 2.85 4.25 5.75 4.25s5.4-1.65 5.75-4.25",
    "M9.5 20.25h5",
  ],
  // A briefcase: the body, the handle, the clasp on its belt line.
  interview: [
    "M5.5 8.75h13a1.25 1.25 0 0 1 1.25 1.25v7.75a1.25 1.25 0 0 1-1.25 1.25h-13a1.25 1.25 0 0 1-1.25-1.25V10A1.25 1.25 0 0 1 5.5 8.75Z",
    "M9.5 8.75V7.25a1.5 1.5 0 0 1 1.5-1.5h2a1.5 1.5 0 0 1 1.5 1.5v1.5",
    "M4.25 13.5h5.75M14 13.5h5.75",
    "M10 12.25h4v2.5h-4Z",
  ],
  // A hanging paper lantern: the cord, the two caps, the round body, the tail.
  festival: [
    "M12 2.75v1.75",
    "M9.25 4.5h5.5",
    "M12 5.25a4.9 6.3 0 0 1 0 12.6a4.9 6.3 0 0 1 0-12.6Z",
    "M9.25 18.75h5.5",
    "M12 18.75v2.5",
  ],
  // A single quiet leaf with its vein.
  funeral: [
    "M5.75 18.25C5.75 10.75 11.25 5.25 18.75 5.25c0 7.5-5.5 13-13 13Z",
    "M6.75 17.25C9.25 13 12.75 9.5 17 7",
  ],
};
