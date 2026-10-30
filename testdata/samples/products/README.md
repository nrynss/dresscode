# Catalog product images

One reference image per entry in [`testdata/occasions/catalog.json`](../../occasions/catalog.json), named by catalog id.

The six exact products are photographs of real products, found through web image search and taken from the retailer's own product page. They are used under the user's fair-use decision (T0.4a), so no licence filtering was done. None is AI-generated or rendered. The two lip colours are approximate matches and are plain colour swatches made locally with ImageMagick.

Every image was checked by eye: no person, face or body, no watermark, no text, no logo. Three garment photos had the retailer's woven neck label showing inside the collar; that label was painted out locally (see each row). Nothing else in any photo was changed. Metadata is stripped from every file.

docs/occasions.md states no file spec for the reference images, so each is JPEG or PNG, at least 512 px on the short side and under 4 MiB.

| File | Catalog id | What it shows | Source page | Edits |
|---|---|---|---|---|
| `gold-jhumkas.jpg` | `gold-jhumkas` | One antique-gold jhumka, front view, on a plain light grey background. Lotus-and-scroll top, dome with an elephant motif, a fringe of gold beads. No ear. | https://www.kushals.com/products/antique-earring-202255 (product image 1 of 7, which shows the pair) | Cropped to the left earring of the pair, trimmed, then padded with the photo's own background colour (`#F4F4F4`) to 640 × 900. Re-encoded as JPEG q92. |
| `gold-chain.jpg` | `gold-chain` | A plain thin gold box chain with its clasp, laid out flat in a V, front view, plain light grey background. Gold-plated sterling silver. | https://www.kushals.com/products/92-5-silver-chain-188329 (product image 3) | None beyond metadata strip and JPEG q92 at the original 1200 × 1200. |
| `lip-red.png` | `lip-red` | Flat swatch, `#B0172F`, a classic blue-leaning lipstick red. | Made locally | `magick -size 512x512 xc:#B0172F` |
| `lip-nude.png` | `lip-nude` | Flat swatch, `#B8836F`, a warm rosy-beige nude. | Made locally | `magick -size 512x512 xc:#B8836F` |
| `shirt-open-collar.jpg` | `shirt-open-collar` | A short-sleeve camp-collar shirt in dark mahogany, collar open, one chest pocket, relaxed cut. Ghost-mannequin product shot on a plain light grey background. | https://www.everlane.com/products/mens-resort-shirt-butterlite-dark-mahogany (product image 2) | Woven brand label inside the back neck covered with a patch of the same shirt's inner-back fabric from 30 px below it (rectangle 468,297 to 538,325, feathered edge). Full frame kept, 1000 × 1250. |
| `shirt-buttoned.jpg` | `shirt-buttoned` | A plain light blue oxford shirt, long sleeves, button-down collar buttoned to the neck, one chest pocket. Ghost-mannequin product shot on a plain light grey background. | https://www.everlane.com/products/mens-lightweight-oxford-shirt-standard-light-blue (product image 2) | Woven brand label inside the back neck filled with the mean colour of the fabric row just above it, plus faint noise (rectangle 562,368 to 636,394, feathered edge). Full frame kept, 1200 × 1500. |
| `top-festive.jpg` | `top-festive` | A maroon kurta with a mandarin collar, gold zari lines on the collar and a zari-embroidered panel down the button placket. Product shot on a plain wooden hanger, white background. It is a boys' kurta; the shape and the festive detail are what the reference carries. | https://www.kalkifashion.com/products/maroon-monotone-kurta-set-with-zari-embroidery-and-jacquard-bundi-by-tiber-taber (product image 5, the kurta alone without the set's bundi or trousers) | Cropped the empty white space below the hem: 853 × 1200 to 853 × 960, from the top. |
| `shirt-dark-plain.jpg` | `shirt-dark-plain` | A plain dark navy oxford shirt, long sleeves, button-down collar buttoned to the neck, one chest pocket. Ghost-mannequin product shot on a plain light grey background. | https://www.everlane.com/products/mens-lightweight-oxford-shirt-slim-navy (product image 2) | Woven brand label inside the back neck filled with a per-column blend of the fabric rows just above and below it, plus faint noise (rectangle 473,302 to 541,330, feathered edge). Full frame kept, 1000 × 1250. |

## Why these

- **Jhumka:** the earring task applies placement only when the reference is a single earring, so one of the pair was cropped out. A plain background was chosen over the retailer's wood and pink-card shots.
- **Chain:** a flat front view of the whole chain. If YouCam rejects a flat shot, docs/occasions.md says the fallback is a sample person's neck, never another person.
- **Shirts:** ghost-mannequin shots show the whole garment front with the collar and shoulders, which is what the clothes task fits to. The open-collar shirt is a camp collar, so it reads relaxed; the buttoned and dark shirts are button-downs closed at the neck. Navy was chosen for the dark shirt over the black corduroy and flannel options because a plain oxford reads sober.
- **Festive top:** almost every ethnic-wear store shows kurtas on a model. This was the only mandarin-collar kurta found with a product-only shot, a plain colour, festive embroidery, and no visible label or logo.
- **Lip colours:** the makeup task paints a colour and does not composite this picture, so a swatch of the intended colour is the honest reference.
