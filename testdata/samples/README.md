# Sample people

Every image in this folder is AI-generated. The people in it do not exist. No photo of a real person was used to make them, and no prompt names or describes a real person. Each person starts as a text-only anchor, and every variant is an edit of that anchor.

The demo, the video, the evaluation (T3.9) and the probes (T0.5, T0.6) use four sample people: an Indian man, an Indian woman, a white woman and an East Asian man. The Indian woman carries the family-function second-visit story. The two men cover the beard criteria; the two women cover the earring and lip criteria.

## Layout

| Path | What it is |
|---|---|
| `anchors/man.jpg` | The Indian man (candidate `man-1`) |
| `anchors/woman.jpg` | The Indian woman (candidate `woman-1`) |
| `anchors/woman-white.jpg` | The white woman (candidate `woman-white-3`) |
| `anchors/man-eastasian.jpg` | The East Asian man (candidate `man-eastasian-1`) |
| `man/`, `woman/`, `woman-white/`, `man-eastasian/` | Each person's variants, `<id>-<occasion>.jpg` |

## Anchors

**The user approved `man-1` and `woman-1` as the first pair's anchors, and `woman-white-3` and `man-eastasian-1` as the second pair's.** They are kept unchanged under the names above.

All anchors and candidates were made the same way:

- **Model:** `bytedance/seedream/v5/pro/text-to-image` on FAL, through Chalachithram's `txt2img` (`--model seedream --size portrait_4_3`).
- **Size returned:** 1536 × 2048 JPEG, kept as returned, each under 0.5 MB.
- **Seed:** none. The endpoint's input schema has no seed parameter, so none was passed and an anchor cannot be reproduced exactly from its prompt. The file is the anchor.
- **Negative prompt:** Chalachithram sends its default with every call: `text, captions, subtitles, watermark, title card, distorted face, extra limbs, extra fingers, blurry, low resolution`. The endpoint's schema does not list `negative_prompt`, so it may be ignored.
- **Cost:** $0.135 each. FAL lists $0.0675 per unit for this endpoint, and an output above 1536 × 1536 in area counts as two units.

Every prompt is a person text, a space, then this shared text:

> Phone selfie taken with the front camera held at arm's length, face straight to the camera, chest-up framing with the shirt collar and both shoulders fully in frame, both ears clearly visible, the neck visible and unobstructed. Plain neutral off-white wall behind, ordinary indoor ceiling light, slightly soft smartphone image quality. Natural skin texture with visible pores and slight unevenness, unretouched, no beauty filter, no makeup, no jewellery, no text, no logos. An original fictional person, not resembling anyone real. Relaxed neutral expression, mouth closed.

| File | Person text | Cost |
|---|---|---|
| `anchors/man.jpg` (was `man-1`) | An Indian man in his early 30s with medium brown skin, an oval face, short neat black hair, and a medium-length black beard and moustache whose density is slightly uneven along the cheeks. He wears a plain light blue cotton everyday shirt with the top button open. | $0.135 |
| `anchors/woman.jpg` (was `woman-1`) | An Indian woman in her late 20s with medium brown skin, an oval face, and long straight black hair past her shoulders, tucked behind both ears and falling down her back. Bare lips. She wears a plain sage green cotton everyday shirt with a simple collar. | $0.135 |
| `anchors/woman-white.jpg` (was `woman-white-3`) | A white woman in her early 30s with pale skin, a rounder face, and long straight auburn hair past her shoulders, tucked behind both ears. Bare lips. She wears a plain light grey cotton everyday shirt with a simple collar. | $0.135 |
| `anchors/man-eastasian.jpg` (was `man-eastasian-1`) | An East Asian man in his early 30s with light warm skin, an oval face, short straight black hair, and a light, sparse black beard and moustache that is thin on the cheeks and slightly uneven. He wears a plain light blue cotton everyday shirt with the top button open. | $0.135 |

### Candidates not chosen

These eight were made with the anchors and deleted from the repository after the user chose. Their prompts are kept here so the choice can be revisited.

| Candidate | Person text | Cost |
|---|---|---|
| `man-2` | An Indian man around 30 with wheatish skin, a squarer jaw, short wavy black hair, and a full black beard and moustache, a little thinner on the upper cheeks. He wears a plain white cotton everyday shirt with the top button open. | $0.135 |
| `man-3` | An Indian man in his mid 30s with deep brown skin, a rounder face, close-cropped black hair, and a short black beard and moustache with slightly uneven density on the cheeks. He wears a plain grey cotton everyday shirt with the top button open. | $0.135 |
| `woman-2` | An Indian woman around 30 with wheatish skin, a heart-shaped face, and long wavy dark brown-black hair past her shoulders, tucked behind both ears. Bare lips. She wears a plain white cotton everyday shirt with a simple collar. | $0.135 |
| `woman-3` | An Indian woman in her early 30s with deep brown skin, a rounder face, and long thick black hair past her shoulders, tucked behind both ears. Bare lips. She wears a plain light grey cotton everyday shirt with a simple collar. | $0.135 |
| `woman-white-1` | A white woman in her late 20s with fair skin and light freckles across the nose, an oval face, and long straight light brown hair past her shoulders, tucked behind both ears. Bare lips. She wears a plain navy cotton everyday shirt with a simple collar. | $0.135 |
| `woman-white-2` | A white woman around 30 with light olive skin, a squarer face, and long wavy dark brown hair past her shoulders, tucked behind both ears. Bare lips. She wears a plain white cotton everyday shirt with a simple collar. | $0.135 |
| `man-eastasian-2` | An East Asian man around 30 with medium warm skin, a squarer jaw, short textured black hair, and a short black beard along the jaw and chin that is sparse on the upper cheeks. He wears a plain white cotton everyday shirt with the top button open. | $0.135 |
| `man-eastasian-3` | An East Asian man in his mid 30s with light skin, a rounder face, close-cropped black hair, and a thin black moustache and chin beard with light, uneven stubble on the cheeks. He wears a plain grey cotton everyday shirt with the top button open. | $0.135 |

## Variants

37 variants, each an edit of an anchor with `fal-ai/nano-banana-2/edit` through Chalachithram's `edit` (`--aspect 3:4 --resolution 1K`). The model returns an 896 × 1200 PNG. Each is committed as JPEG at quality 92 with metadata stripped (ImageMagick), at the same size, each under 0.4 MB. FAL lists $0.08 per image at 1K.

The reference is the person's anchor, with two exceptions. V05 is an edit of V04, so the first visit and the kept look are the same look. V21 is an edit of an earlier V08 attempt that had the patchy beard (see "Rows changed").

Each row is judged against its occasion's criteria only (`testdata/occasions/<occasion>.json`). **Yes** is the criteria the image should pass; **No** is the criteria it should fail. Every criterion of that occasion is in exactly one of the two lists. Those lists are the evaluation's ground truth (T3.9).

### Indian man and Indian woman

| Id | Person | Occasion | The shot | Yes | No |
|---|---|---|---|---|---|
| V01 | man | date | Relaxed open-collar shirt, short even beard, closed-mouth smile, even light | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-beard-even`, `date-lip-clear`, `date-expression-engaged`, `date-light-see` |  |
| V02 | man | date | Buttoned shirt and tie under a suit jacket, flat stare, colour photo with flat dull frontal light | `date-hair-clear`, `date-beard-even`, `date-lip-clear` | `date-collar-open`, `date-garment-easy`, `date-expression-engaged`, `date-light-see` |
| V21 | man | date | Relaxed open-collar shirt, thin patchy beard, engaged smile, soft light | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-lip-clear`, `date-expression-engaged`, `date-light-see` | `date-beard-even` |
| V03 | woman | date | Open-collar relaxed shirt, hair loose across the eyes, smeared red lip, relaxed smile, even light | `date-collar-open`, `date-garment-easy`, `date-beard-even`, `date-expression-engaged`, `date-light-see` | `date-hair-clear`, `date-lip-clear` |
| V04 | woman | family-function | First visit: festive top, hair tied, calm, even light, bare ears, bare lips | `family-garment-festive`, `family-hair-finished`, `family-beard-even`, `family-expression-present`, `family-light-even` | `family-earring-festive`, `family-lip-colour` |
| V05 | woman | family-function | V04 with gold jhumkas and a clear red lip | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-hair-finished`, `family-beard-even`, `family-expression-present`, `family-light-even` |  |
| V06 | woman | family-function | Festive top, gold jhumkas, red lip, hair loose across the eyes, flat stare | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-beard-even`, `family-light-even` | `family-hair-finished`, `family-expression-present` |
| V07 | man | family-function | Festive top with mandarin collar, neat beard, calm, even light | `family-garment-festive`, `family-hair-finished`, `family-beard-even`, `family-expression-present`, `family-light-even` | `family-earring-festive`, `family-lip-colour` |
| V08 | man | family-function | Office shirt, thin patchy beard, wide grin, shadow on one side | `family-hair-finished` | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-beard-even`, `family-expression-present`, `family-light-even` |
| V09 | man | interview | Plain white shirt buttoned with a tie, short even beard, calm, plain light | `interview-collar-buttoned`, `interview-garment-plain`, `interview-hair-close`, `interview-beard-short`, `lip-quiet`, `interview-expression-calm`, `interview-light-plain` |  |
| V10 | man | interview | Tropical-print open-collar shirt, long scraggly beard to the collar, wide grin, orange cast | `interview-hair-close`, `lip-quiet` | `interview-collar-buttoned`, `interview-garment-plain`, `interview-beard-short`, `interview-expression-calm`, `interview-light-plain` |
| V11 | woman | interview | Plain buttoned shirt, hair loose across the eyes, bright red lip, calm, plain light | `interview-collar-buttoned`, `interview-garment-plain`, `interview-beard-short`, `interview-expression-calm`, `interview-light-plain` | `interview-hair-close`, `lip-quiet` |
| V12 | woman | interview | Plain buttoned shirt, hair tied back close, quiet nude lip, calm, plain light | `interview-collar-buttoned`, `interview-garment-plain`, `interview-hair-close`, `interview-beard-short`, `lip-quiet`, `interview-expression-calm`, `interview-light-plain` |  |
| V13 | woman | festival | Festive top, gold jhumkas, gold chain, red lip, hair tied, open smile, warm even light | `festival-garment-festive`, `festival-earring`, `festival-necklace`, `festival-lip`, `festival-hair`, `festival-beard`, `festival-expression`, `festival-light` |  |
| V14 | woman | festival | Plain office shirt, bare ears, bare neck, bare lips, hair across the face, flat stare, dark room | `festival-beard` | `festival-garment-festive`, `festival-earring`, `festival-necklace`, `festival-lip`, `festival-hair`, `festival-expression`, `festival-light` |
| V15 | man | festival | Festive kurta top, beard with a clean edge, open smile, warm even light | `festival-garment-festive`, `festival-hair`, `festival-beard`, `festival-expression`, `festival-light` | `festival-earring`, `festival-necklace`, `festival-lip` |
| V16 | man | festival | Plain office shirt, patchy stubble, present expression, dim room | `festival-hair`, `festival-expression` | `festival-garment-festive`, `festival-earring`, `festival-necklace`, `festival-lip`, `festival-beard`, `festival-light` |
| V17 | man | funeral | Black shirt buttoned to the top, short close beard, still closed mouth, plain light | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |  |
| V18 | man | funeral | Bright patterned open-collar shirt, long scraggly beard, wide smile, pink-purple light washing the wall behind | `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain` | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |
| V19 | woman | funeral | Black shirt buttoned, one small stud each ear, bare lips, hair tied, still, plain light | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |  |
| V20 | woman | funeral | Bright festive top with open neck, gold jhumkas, red lip, hair across the eyes, wide smile, hard side shadow | `funeral-beard-close` | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain`, `funeral-expression-still`, `funeral-light-plain` |

### White woman and East Asian man

| Id | Person | Occasion | The shot | Yes | No |
|---|---|---|---|---|---|
| P01 | woman-white | date | Relaxed open-collar shirt, hair tied back, one even nude lip, relaxed smile, even light | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-beard-even`, `date-lip-clear`, `date-expression-engaged`, `date-light-see` |  |
| P02 | woman-white | date | Shirt buttoned to the neck under a stiff blazer, hair across the eyes, smeared lip, relaxed smile, even light | `date-beard-even`, `date-expression-engaged`, `date-light-see` | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-lip-clear` |
| P03 | woman-white | family-function | Festive top, gold jhumkas, red lip, hair tied, calm, even light | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-hair-finished`, `family-beard-even`, `family-expression-present`, `family-light-even` |  |
| P04 | woman-white | family-function | Office shirt, small plain studs, bare lips, hair loose across the eyes, wide grin, hard side shadow | `family-beard-even` | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-hair-finished`, `family-expression-present`, `family-light-even` |
| P05 | woman-white | interview | Plain shirt buttoned to the top, hair loose across the eyes, bare lips, calm, plain light | `interview-collar-buttoned`, `interview-garment-plain`, `interview-beard-short`, `lip-quiet`, `interview-expression-calm`, `interview-light-plain` | `interview-hair-close` |
| P06 | woman-white | festival | Festive top, gold hoops, gold chain, berry lip, hair loose across the face, flat stare, warm even light | `festival-garment-festive`, `festival-earring`, `festival-necklace`, `festival-lip`, `festival-beard`, `festival-light` | `festival-hair`, `festival-expression` |
| P07 | woman-white | funeral | Dark plain shirt buttoned, small plain studs, quiet nude lip, hair tied, still, plain light | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |  |
| P08 | woman-white | funeral | Dark plain top with an open neck, large festive drop earrings and a big necklace, bright pink lip, hair loose across the eyes, still, plain light | `funeral-garment-dark`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` | `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain` |
| P09 | man-eastasian | date | Relaxed open-collar shirt, short even beard, engaged smile, even light | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-beard-even`, `date-lip-clear`, `date-expression-engaged`, `date-light-see` |  |
| P10 | man-eastasian | date | Relaxed open-collar shirt, patchy beard, flat stare, shadow on one side | `date-collar-open`, `date-garment-easy`, `date-hair-clear`, `date-lip-clear` | `date-beard-even`, `date-expression-engaged`, `date-light-see` |
| P11 | man-eastasian | family-function | Festive top with mandarin collar, neat short beard, calm, even light | `family-garment-festive`, `family-hair-finished`, `family-beard-even`, `family-expression-present`, `family-light-even` | `family-earring-festive`, `family-lip-colour` |
| P12 | man-eastasian | family-function | Office shirt, patchy beard, flat stare, even light | `family-hair-finished`, `family-light-even` | `family-earring-festive`, `family-lip-colour`, `family-garment-festive`, `family-beard-even`, `family-expression-present` |
| P13 | man-eastasian | interview | Plain crew-neck tee, patchy beard, wide grin, orange cast | `interview-hair-close`, `lip-quiet` | `interview-collar-buttoned`, `interview-garment-plain`, `interview-beard-short`, `interview-expression-calm`, `interview-light-plain` |
| P14 | man-eastasian | festival | Festive kurta top, patchy stubble, open smile, warm even light | `festival-garment-festive`, `festival-hair`, `festival-expression`, `festival-light` | `festival-earring`, `festival-necklace`, `festival-lip`, `festival-beard` |
| P15 | man-eastasian | funeral | Dark plain shirt buttoned, short close beard, still closed mouth, plain light | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain`, `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |  |
| P16 | man-eastasian | funeral | Black shirt and tie, patchy beard, wide smile, pink-purple light washing the wall behind | `funeral-garment-dark`, `funeral-collar-plain`, `funeral-jewellery-quiet`, `lip-quiet`, `funeral-hair-plain` | `funeral-beard-close`, `funeral-expression-still`, `funeral-light-plain` |

Notes on the ground truth:

- **The second-visit story** is V04 then V05. The first visit misses `family-earring-festive` and `family-lip-colour`. V05 is the same look with gold jhumkas and a red lip, for the evaluation only; in the product those come from T0.5 renders of V04.
- **Beard** criteria fail only on the two men. A woman passes them as "absent".
- **Short hair** counts as finished, close and off the eyes for the men. The hair misses are the women's. Where strands cross the eyes but the eyes still show (V03, V06, V11, P02, P05, P06, P08), the light criteria stay a yes.
- **Bare lip** passes `lip-quiet` and fails `family-lip-colour` and `festival-lip`. That is the dress code, not a judgement on the person.
- **Weakest light misses:** V08 and P10. Each has a shadow across one side of the face and the wall behind, not the near-black half the prompt asked for, and both eyes still show. The edit model softens a hard side shadow; P04 is the one that came out with a hard edge.
- **Coloured light** in V18 and P16 falls on the wall behind, and the skin keeps its natural colour. Both fail `funeral-light-plain` as a bright coloured wash. V18's prompt also asked for a hard side light, and it came back soft.
- **The East Asian man's beard** is light. P09, P11 and P15 read as a short even beard. P10, P12, P13, P14 and P16 read as patchy, with bare cheeks.
- **V15's beard** came back as the anchor's medium beard with a clean edge, not a thick full one. It is marked a yes for `festival-beard` ("absent or a full shape with a clean edge").

### Rows changed from the plan

- **V02** was planned to fail `date-beard-even` with a patchy beard. Three attempts kept the anchor's even beard, so its beard is a **yes**. The review then found the kept attempt nearly monochrome, like a black-and-white filter. It was redone as a colour photo, with the face lit flat, dull and shadowless, and the beard left as it is.
- **V21** is new, to keep a `date-beard-even` no. It edits V08's third attempt, the first that read as patchy, into a date look. It passes every date criterion except the beard.
- **V09** was planned as a plain shirt buttoned to the top. Two attempts left the top button open; the third adds a plain navy tie, which closes the collar. The tie does not change any interview criterion.
- **V18** first came back with the skin itself tinted magenta, which reads as an edit. It was redone with the coloured light on the wall and the skin in its natural colour.
- **P16** was planned with a coloured wash on the face. To avoid V18's tinted skin, the wash is on the wall behind. The first attempt asked for a hard side shadow instead, and it came back too soft.

## Prompts

Every variant prompt is one of two preambles, then the row's change text.

**Preamble A** (rows whose miss is hair over the face, so the ears may be covered):

> Edit the reference photo of this person. Keep the same person: the identical face, face shape, jaw, nose, eyes, eyebrows, skin tone, natural skin texture with visible pores and slight unevenness, and the same build. It stays an ordinary unretouched phone selfie from the front camera at arm's length: face straight to the camera, chest-up framing with the collar and both shoulders in frame, the neck visible, plain indoor wall behind. No beauty filter, no skin smoothing, no face reshaping, no text, no logos, no brand marks.

**Preamble B** (every other row): the same text with `both ears visible, ` inserted before `the neck visible`.

The full prompt is the preamble, a space, then the change text. The text below is the kept attempt's prompt exactly. "Calls" counts every attempt for that row, including the ones discarded, at $0.08 each.

### Indian man and Indian woman

#### V01: `man/V01-date.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a relaxed plain soft olive cotton shirt with the collar open and the top two buttons undone. His beard is trimmed to a short, even, neat shape with full, uniform density on both cheeks and a clean outline. He gives a relaxed closed-mouth smile, engaged with the camera. Soft even indoor light shows both eyes clearly.

#### V02: `man/V02-date.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 4 ($0.32).

> Change: he wears a white formal shirt buttoned to the neck with a dark tie, under a dark formal suit jacket. Keep his beard as it is. He gives a flat, blank stare with no expression. It is a normal full-colour photo with natural skin colour, but the face is lit flat and dull: a weak, even, shadowless light from straight in front, low contrast, no modelling on the face, a dim greyish room, so the face looks flat and lifeless rather than lit.

#### V21: `man/V21-date.jpg`

Reference: V08's third attempt, as returned (kept as `man/V08-family-function.jpg`). Preamble B. Calls: 1 ($0.08).

> Change only this: he now wears a relaxed plain soft grey-green cotton shirt with the collar open and the top two buttons undone, he gives a relaxed, engaged closed-mouth smile, and the light is soft and even on both sides of his face with both eyes clearly visible. Keep his thin, patchy, uneven beard exactly as it is, with the bare patches on the cheeks.

#### V03: `woman/V03-date.jpg`

Reference: `anchors/woman.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a relaxed plain rust-coloured cotton shirt with the collar open. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She wears red lipstick that is smeared: the colour goes past the lip line at one corner and the edge is uneven and blotchy. She has a relaxed, warm, engaged look with a slight smile. Soft even indoor light. No jewellery.

#### V04: `woman/V04-family-function.jpg`

Reference: `anchors/woman.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a festive deep magenta silk top with a gold woven border and a mandarin collar. Her hair is tied back neatly into a low bun, off the face, with both ears visible and bare: no earrings. Her lips are bare with no lipstick. No necklace, no jewellery. She has a calm, present expression with the mouth gently closed. Soft even indoor light on both eyes.

#### V05: `woman/V05-family-function.jpg`

Reference: the V04 render, as returned (kept as `woman/V04-family-function.jpg`). Preamble B. Calls: 1 ($0.08).

> Change only this: add a pair of traditional gold jhumka earrings (bell-shaped dangling gold earrings) to both ears, and a clear, even red lipstick neatly applied. Keep the top, the hair, the expression, the light and everything else exactly as it is.

#### V06: `woman/V06-family-function.jpg`

Reference: `anchors/woman.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a festive teal silk top with a gold woven border and a mandarin collar, traditional gold jhumka earrings (bell-shaped dangling gold earrings) clearly visible below the hair, and a clear, even red lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She gives a flat, blank, absent stare. Soft even indoor light.

#### V07: `man/V07-family-function.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a festive maroon raw-silk kurta top with a mandarin collar and a subtle woven border. His beard is trimmed to a short, even, neat shape with uniform density and a clean outline. No jewellery. He has a calm, present expression with the mouth gently closed. Soft even indoor light on both eyes.

#### V08: `man/V08-family-function.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 3 ($0.24).

> Change: he wears a plain pale blue formal office shirt with a buttoned collar. Remove his full beard. In its place he has a thin, patchy, uneven beard that grew in badly: the cheeks are mostly bare skin with only a few scattered isolated hairs, a small sparse tuft on the chin, thin disconnected patches along the jaw that do not join up, and a weak moustache with a bare gap between it and the chin. He gives a wide, open-mouthed grin showing his teeth. Dramatic split lighting from one bare lamp at his left side, the room otherwise dark: the left half of his face is brightly lit and the right half, including the right eye, is in near-black shadow, with a hard edge down the middle of the nose.

#### V09: `man/V09-interview.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 3 ($0.24).

> Change: he wears a plain white structured formal shirt buttoned all the way to the neck, worn with a plain dark navy tie knotted at the closed collar. His beard is trimmed short and even with a neat edge, well above the collar. He has a calm, relaxed expression with the mouth closed. Plain, neutral, even light with no colour cast.

#### V10: `man/V10-interview.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a loud short-sleeved shirt with a bold tropical leaf print (no logos, no text) and an open collar. His beard is grown out long and scraggly, past the jaw and down to touch the collar, uneven with patchy gaps. He gives a wide, open-mouthed grin showing his teeth. A strong orange colour cast from a warm lamp covers his face.

#### V11: `woman/V11-interview.jpg`

Reference: `anchors/woman.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a plain white structured formal shirt buttoned to the top button. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She wears a bright glossy red lipstick. No jewellery. She has a calm, relaxed expression with the mouth closed. Plain, neutral, even light with no colour cast.

#### V12: `woman/V12-interview.jpg`

Reference: `anchors/woman.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a plain light blue structured formal shirt buttoned to the top button. Her hair is tied back neatly into a low bun, close to the head and off the face, with both ears visible and no earrings. She wears a quiet nude lipstick close to her natural lip colour. She has a calm, relaxed expression with the mouth closed. Plain, neutral, even light with no colour cast.

#### V13: `woman/V13-festival.jpg`

Reference: `anchors/woman.jpg`. Preamble B. Calls: 2 ($0.16).

> Change: she wears a festive saffron-orange silk top with a gold zari border and a mandarin collar, traditional gold jhumka earrings (bell-shaped dangling gold earrings), a thin gold chain necklace on her neck, and a bold, clearly visible bright red lipstick evenly covering both lips. Her hair is tied back neatly into a bun, off the eyes. She gives an open, happy smile. Warm, even indoor light like festival lamps, both eyes well lit.

#### V14: `woman/V14-festival.jpg`

Reference: `anchors/woman.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a plain pale grey office shirt with a buttoned collar. No earrings, no necklace, bare lips with no lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes. She gives a flat, blank stare. The room is dim and dark: her face is underlit, murky and hard to see.

#### V15: `man/V15-festival.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a festive deep green silk kurta top with a gold-thread border and a mandarin collar. His beard is full and thick with a clean, sharp, trimmed edge on the cheeks and neck. No jewellery. He gives an open, happy smile. Warm, even indoor light like festival lamps, both eyes well lit.

#### V16: `man/V16-festival.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a plain white office shirt with a buttoned collar. His facial hair is patchy stubble: a few days of uneven growth with clearly visible bare gaps on the cheeks and no clean edge. He has a present, attentive expression with a slight smile. The room is dim and dark: his face is underlit and murky.

#### V17: `man/V17-funeral.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 2 ($0.16).

> Change: he wears a plain black shirt with every button fastened, including the top collar button, so the collar is closed snugly around the neck. His beard is trimmed short and close to the skin with an even, neat shape. He has a still expression with the mouth closed. Plain, even, neutral light.

#### V18: `man/V18-funeral.jpg`

Reference: `anchors/man.jpg`. Preamble B. Calls: 2 ($0.16).

> Change: he wears a bright yellow and red patterned shirt (no logos, no text) with an open collar. His beard is grown out long and scraggly, uneven with patchy gaps. He gives a wide, open-mouthed smile showing his teeth. A single hard lamp from one side lights half of his face and leaves the other half in deep shadow, and a bright pink-purple party light falls on the wall behind him. His skin keeps its natural colour: the coloured light is on the wall, not on his face.

#### V19: `woman/V19-funeral.jpg`

Reference: `anchors/woman.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a plain black shirt buttoned to the top button. She wears one small plain silver stud in each ear and nothing else: no necklace. Her lips are bare with no lipstick. Her hair is tied back neatly into a low bun, off the face. She has a still expression with the mouth closed. Plain, even, neutral light.

#### V20: `woman/V20-funeral.jpg`

Reference: `anchors/woman.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a bright festive magenta silk top with an open neckline, traditional gold jhumka earrings (bell-shaped dangling gold earrings) visible below the hair, and a bright red lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes. She gives a wide, open-mouthed smile showing her teeth. Hard light from one side lights half of her face and leaves the other half in deep shadow.

### White woman and East Asian man

#### P01: `woman-white/P01-date.jpg`

Reference: `anchors/woman-white.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a relaxed plain soft blue cotton shirt with the collar open. Her hair is tied back neatly into a low bun, off the face, with both ears visible. She wears one even nude-pink lipstick, neatly applied. No jewellery. She gives a relaxed, engaged closed-mouth smile. Soft even indoor light shows both eyes clearly.

#### P02: `woman-white/P02-date.jpg`

Reference: `anchors/woman-white.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a crisp white shirt buttoned to the neck with a plain navy tie, under a stiff dark formal blazer. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She wears red lipstick that is smeared: the colour goes past the lip line at one corner and the edge is uneven and blotchy. She has a relaxed, warm, engaged look with a slight smile. Soft even indoor light. No jewellery.

#### P03: `woman-white/P03-family-function.jpg`

Reference: `anchors/woman-white.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a festive emerald green silk top with a gold woven border and a mandarin collar, traditional gold jhumka earrings (bell-shaped dangling gold earrings), and a clear, even red lipstick. Her hair is tied back neatly into a low bun, off the face, with both ears visible. No necklace. She has a calm, present expression with the mouth gently closed. Soft even indoor light on both eyes.

#### P04: `woman-white/P04-family-function.jpg`

Reference: `anchors/woman-white.jpg`. Preamble A. Calls: 2 ($0.16).

> Change: she wears a plain pale blue formal office shirt with a buttoned collar and one small plain silver stud in each ear. Her lips are bare with no lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She gives a wide, open-mouthed grin showing her teeth. The room is dark except for one bare bulb close to one side of her face, like a stage spotlight from the side: that side of her face is brightly lit and the other side falls into near-black shadow, the far eye barely visible, with a hard, sharp edge running down the middle of the nose. Strong, high-contrast, natural skin colour.

#### P05: `woman-white/P05-interview.jpg`

Reference: `anchors/woman-white.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a plain white structured formal shirt with every button fastened, including the top collar button, so the collar is closed snugly around the neck. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. Her lips are bare with no lipstick. No jewellery. She has a calm, relaxed expression with the mouth closed. Plain, neutral, even light with no colour cast.

#### P06: `woman-white/P06-festival.jpg`

Reference: `anchors/woman-white.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a festive deep red silk top with a gold zari border and a mandarin collar, gold hoop earrings visible below the hair, a thin gold chain necklace on her neck, and a clear berry-coloured lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She gives a flat, blank, absent stare. Warm, even indoor light like festival lamps.

#### P07: `woman-white/P07-funeral.jpg`

Reference: `anchors/woman-white.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: she wears a plain black shirt with every button fastened, including the top collar button, so the collar is closed snugly around the neck. She wears one small plain silver stud in each ear and nothing else. She wears a quiet nude lipstick close to her natural lip colour. Her hair is tied back neatly into a low bun, off the face, with both ears visible. She has a still expression with the mouth closed. Plain, even, neutral light.

#### P08: `woman-white/P08-funeral.jpg`

Reference: `anchors/woman-white.jpg`. Preamble A. Calls: 1 ($0.08).

> Change: she wears a plain black top with an open V neckline, large gold festive drop earrings visible below the hair, a big statement gold necklace, and a bright pink lipstick. Her long hair is loose and falls forward across her face, with strands of hair hanging across both eyes; the eyes are still visible between the strands. She has a still expression with the mouth closed. Plain, even, neutral light.

#### P09: `man-eastasian/P09-date.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a relaxed plain olive cotton shirt with the collar open and the top two buttons undone. Fill his beard in to a short, even, neat beard: uniform density on the cheeks, jaw and chin, joined to the moustache, with a clean trimmed outline. He gives a relaxed closed-mouth smile, engaged with the camera. Soft even indoor light shows both eyes clearly.

#### P10: `man-eastasian/P10-date.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 3 ($0.24).

> Change: he wears a relaxed plain soft grey cotton shirt with the collar open. Make his thin beard clearly patchy: the cheeks are mostly bare skin with only scattered isolated hairs, the chin and jaw have sparse disconnected patches that do not join up, and the moustache is thin with gaps. He gives a flat, blank stare with no expression. A single hard lamp from one side lights half of his face and leaves the other half, including that eye, in deep shadow, with a hard edge down the nose.

#### P11: `man-eastasian/P11-family-function.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a festive navy blue raw-silk kurta top with a mandarin collar and a subtle woven border. Fill his beard in to a short, even, neat beard: uniform density on the cheeks, jaw and chin, joined to the moustache, with a clean trimmed outline. No jewellery. He has a calm, present expression with the mouth gently closed. Soft even indoor light on both eyes.

#### P12: `man-eastasian/P12-family-function.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a plain pale blue formal office shirt with a buttoned collar. Make his thin beard clearly patchy: the cheeks are mostly bare skin with only scattered isolated hairs, the chin and jaw have sparse disconnected patches that do not join up, and the moustache is thin with gaps. He gives a flat, blank, absent stare. Soft even indoor light on both eyes.

#### P13: `man-eastasian/P13-interview.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a plain heather grey crew-neck t-shirt, no collar. Make his thin beard clearly patchy: the cheeks are mostly bare skin with only scattered isolated hairs, the chin and jaw have sparse disconnected patches that do not join up, and the moustache is thin with gaps. He gives a wide, open-mouthed grin showing his teeth. A strong orange colour cast from a warm lamp covers the whole photo.

#### P14: `man-eastasian/P14-festival.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a festive saffron silk kurta top with a gold-thread border and a mandarin collar. Make his thin beard clearly patchy: the cheeks are mostly bare skin with only scattered isolated hairs, the chin and jaw have sparse disconnected patches that do not join up, and the moustache is thin with gaps. No jewellery. He gives an open, happy smile. Warm, even indoor light like festival lamps, both eyes well lit.

#### P15: `man-eastasian/P15-funeral.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 1 ($0.08).

> Change: he wears a plain black shirt buttoned all the way to the neck, worn with a plain black tie knotted at the closed collar. Fill his beard in to a short, even, neat beard: uniform density on the cheeks, jaw and chin, joined to the moustache, with a clean trimmed outline. He has a still expression with the mouth closed. Plain, even, neutral light.

#### P16: `man-eastasian/P16-funeral.jpg`

Reference: `anchors/man-eastasian.jpg`. Preamble B. Calls: 2 ($0.16).

> Change: he wears a plain black shirt buttoned all the way to the neck, worn with a plain black tie knotted at the closed collar. Make his thin beard clearly patchy: the cheeks are mostly bare skin with only scattered isolated hairs, the chin and jaw have sparse disconnected patches that do not join up, and the moustache is thin with gaps. He gives a wide, open-mouthed smile showing his teeth. A bright pink-purple party light falls on the wall behind him, so the background is a strong coloured wash. His skin keeps its natural colour: the coloured light is on the wall, not on his face.

## Cost

At FAL's listed prices:

- 12 Seedream anchor candidates at $0.135: $1.62.
- 51 edit calls at $0.08: $4.08. 37 were kept and 14 discarded or replaced, including the redone V02 and V18.
- Total: $5.70.

## Not in this folder

The catalog product shots (docs/occasions.md) and the known bad renders for the render check (T0.5 and hand edits) are follow-up work.
