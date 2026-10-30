# Occasions, fix menus, and the look vocabulary

Draft for sign-off. The data is in [`testdata/occasions/`](../testdata/occasions/). Nothing here is final until the user says so.

The product checks one selfie against one occasion. A criterion is a yes/no question. Yes means the dress code is met. The app shows a miss as the suggestion, never as a score. Every fix is opt-in. The menu does not infer who should see beard, lip colour, jewellery, or anything else.

## Files

| File | What it is |
|---|---|
| `vocabulary.json` | The only look vocabulary. `kind` is `vocabulary`. |
| `catalog.json` | Products T0.4 will photograph or license. `kind` is `catalog`. |
| `date.json`, `family-function.json`, `interview.json`, `festival.json`, `funeral.json` | One occasion each. `kind` is `occasion`. |

An occasion has `criteria`, `biggest_issue`, `fixes`, and `basket`. A criterion has `id`, `kind` (`clothing`, `grooming`, `lighting`, or `expression`), `clef` (the question), and `suggestion` (what the user reads). The biggest-issue question is one choice. Its options are that occasion's criteria, then `none`, then `can't tell`.

A fix has what it changes, a YouCam task (and, for hair and beard, a template), the analysis it needs (or none), the criteria it addresses, and the vocabulary values it sets. `youcam.path_verified` is true only where a Perfect Corp page prints that task path. `youcam.set` is the qualifying set: Skin, Beauty, Fashion, Jewelry & Watch, or Hair & Beard.

A Clef question is reused only when two occasions share the rule, and then the id, the question, and the suggestion are the same. Interview and funeral share `lip-quiet`: bare or a quiet nude, not a bright colour. Every other question is its own rule.

## Look vocabulary

One choice question per attribute. Every list includes `none` and `can't tell`. The render check answers these, and the app joins the answers into the line Lambo stores. The same look is always described the same way.

| Attribute | Question | Options |
|---|---|---|
| `jewellery` | What earring is visible? | none, jhumkas, studs, hoops, drops, can't tell |
| `jewellery_colour` | What colour is that earring? | none, gold, silver, pearl, red, multicolour, can't tell |
| `necklace` | What necklace is visible? | none, chain, pendant, choker, can't tell |
| `necklace_colour` | What colour is that necklace? | none, gold, silver, pearl, black, can't tell |
| `lip_colour` | What lip colour is visible? | none, red, nude, berry, brown, pink, can't tell |
| `collar` | How does the collar read? | none, open, buttoned, mandarin, crew, covered, can't tell |
| `hair` | How is the hair arranged? | none, loose, tied-back, short, bun, fringe, covered, can't tell |
| `hair_colour` | What hair colour is visible? | none, black, brown, blonde, grey, red, can't tell |
| `beard` | What facial hair is visible? | none, short, full, mustache, goatee, patchy, can't tell |
| `expression` | What expression is visible? | none, flat, closed-mouth-smile, open-smile, tense, can't tell |
| `garment` | What garment is visible at the neck and shoulders? | none, relaxed-shirt, plain-shirt, festive-top, dark-plain, tee, suit, can't tell |

`hair_colour` is described and never rendered. A hair-colour fix would have to pick one target colour, which would be a guess. The public AI Hair Color page does not print a task path, so none was invented. Hairstyle v2.1 can match a preset colour on some templates; this menu does not use that.

The family-function story is two vocabulary changes, not one. Kept: `jewellery` jhumkas and `jewellery_colour` gold (`family-gold-jhumkas`). Dismissed: `lip_colour` red (`family-red-lip`). "Last family function you kept the gold jhumkas and dismissed the red lip." A festival red lip is a different fix (`festival-red-lip`), so a dismissal there is not the family one.

## What a fix may render

Only tasks from the qualifying sets, and only ones whose path is on a Perfect Corp page.

| Task | Set | Path | Used for |
|---|---|---|---|
| AI Clothes Virtual Try-On | Fashion | `POST /s2s/v2.0/task/cloth-v4` | Collar and garment |
| AI Earring Virtual Try-On | Jewelry & Watch | `POST /s2s/v2.0/task/2d-vto/earring` | Jhumkas |
| AI Necklace Virtual Try-On | Jewelry & Watch | `POST /s2s/v2.0/task/2d-vto/necklace` | Gold chain |
| AI Makeup Virtual Try-On | Beauty | `POST /s2s/v2.0/task/makeup-vto` | Lip colour, category `lip_color` |
| AI Hairstyle Generator | Hair & Beard | `POST /s2s/v2.1/task/hair-transfer` | Tied-back or short hair, from a template |
| AI Beard Style Generator | Hair & Beard | `POST /s2s/v2.0/task/beard-style` | A short or full beard, from a template |
| AI Smile | Skin | `POST /s2s/v2.0/task/ai-smile` | A closed-mouth smile |

AI Smile is grouped with face and skin tools on the YouCam MCP page, not with Creators. If the hackathon catalog files it under Beauty instead, it is still a qualifying set.

A hair-transfer or beard-style fix carries `template`, the YouCam template id it renders with. Every id is in T0.5's recorded listings (`testdata/youcam/http-templates-*`). The loader refuses a hair or beard fix without one, and a template on any other task. The vocabulary change is what Clef reads on the render, which is not always the words of the criterion: a bun is tied-back hair, and a goatee is a short beard.

| Template | Title | Fixes | Reads as | Who chose |
|---|---|---|---|---|
| `female_casual_updo` | Casual Updo | every `*-tie-hair` | hair bun | The user. No title says tied back; of the two T0.5 rendered, the updo keeps all the hair off the eyes and High Pony Tail does not. |
| `all_circle` | Circle | every `*-short-beard` | beard goatee | The user. No beard is short and even across the cheeks; Circle (moustache joined to a chin beard, cheeks clear) is the shortest even shape. |
| `all_garibaldi` | Garibaldi | `festival-full-beard` | beard full | Picked by the agent, approved by the user. Of the full beards (Garibaldi, Bandholz, Long Bandholz, Ducktail), Garibaldi is wide and rounded with an even edge; the Bandholz pair are long and Ducktail is pointed. Not rendered yet. |
| `female_crewcut` | Crewcut | `interview-short-hair` | hair short | Picked by the agent, approved by the user. Crewcut is the title closest to short and neat. Of the two Crewcut templates, this one keeps the user's hair colour (`keep_users_color`); `all_crewcut` does not. Not rendered yet. |

The `female_` and `all_` prefixes are YouCam's listing categories. They are not a gender choice, and nothing infers one.

Makeup must not set lip `morphology` fullness, must not pick a lip shape that redraws the mouth, and must send `skin_smooth` at 0 (left out, it defaults to 50 and smooths the skin; T0.5). A render that smooths skin or reshapes the face is discarded.

The only analysis on the menu is face shape, and only for a hair fix. The task name on the analyzer page is `face-attr-analysis` (`POST 'task/face-attr-analysis'`). The `/s2s/v2.0` prefix is the v2 pattern used by the other tasks, not a second path printed on that page. The picker may receive `faceShape` only. That response also carries age and gender. Those fields are never read, stored, or sent on. Ratios are never sent.

No fix runs a shade match, so Facial Color Tones (`skin-tone-analysis`) is not on the menu. Hair type (`hair-type-detection`) needs a front, a left, and a right. The product is one selfie, so it is not on the menu either.

## Date

An easy collar, a relaxed shirt, hair off the eyes, facial hair absent or short and even, a lip that is bare or one even colour, an engaged expression, and light that shows both eyes.

| Fix | Changes | Task | Template | Analysis | Criteria | Vocabulary |
|---|---|---|---|---|---|---|
| `date-open-collar` | Open-collar shirt | `cloth-v4` | none | none | collar, garment | collar open, garment relaxed-shirt |
| `date-tie-hair` | Hair tied back | `hair-transfer` | `female_casual_updo` | face shape | hair | hair bun |
| `date-short-beard` | Short even beard | `beard-style` | `all_circle` | none | facial hair | beard goatee |
| `date-nude-lip` | One even nude | `makeup-vto` | none | none | lip | lip colour nude |
| `date-closed-smile` | Closed-mouth smile | `ai-smile` | none | none | expression | expression closed-mouth-smile |

The light criterion has no fix. Basket: garments (`shirt-open-collar`), lip colour (`lip-nude`). Hair, beard, and the smile are templates, labelled visual simulation, with nothing to photograph.

## Family function

This is the occasion the demo tells. The first visit can miss the earring and the lip. The jhumkas render is the one to keep. The red lip is the one to dismiss. Later, the same occasion offers the jhumkas and does not offer that red lip.

The dress code: gold jhumkas rather than a bare ear or a plain stud, a lip with a clear colour rather than bare, a festive top rather than an office shirt, hair tied and off the eyes, facial hair absent or one neat shape, a present calm expression, and even light on both eyes.

| Fix | Changes | Task | Template | Analysis | Criteria | Vocabulary |
|---|---|---|---|---|---|---|
| `family-gold-jhumkas` | Gold jhumkas | `2d-vto/earring` | none | none | earring | jewellery jhumkas, colour gold |
| `family-red-lip` | Red lip | `makeup-vto` | none | none | lip | lip colour red |
| `family-festive-top` | Festive top | `cloth-v4` | none | none | garment | garment festive-top, collar mandarin |
| `family-tie-hair` | Hair tied back | `hair-transfer` | `female_casual_updo` | face shape | hair | hair bun |
| `family-short-beard` | Short even beard | `beard-style` | `all_circle` | none | facial hair | beard goatee |
| `family-closed-smile` | Closed-mouth smile | `ai-smile` | none | none | expression | expression closed-mouth-smile |

The lip question asks for a clear colour. The only fix that meets it is the red lip, so a dismissal is specifically that red. Light has no fix. Basket: earrings (`gold-jhumkas`), lip colour (`lip-red`), garments (`top-festive`).

The earring reference is one jhumka, front view, no logo and no ear. A front selfie is supported; the page says a side view renders better. The jhumkas are an exact product because the task composites that photograph. The red lip is an approximate match: the task paints a colour, and the catalog photo is the colour we mean, not an image it pastes on.

## Interview

A buttoned collar, a plain shirt, hair off the face and close, facial hair absent or short and off the collar, a lip that is bare or a quiet nude, a calm expression, and plain even light.

| Fix | Changes | Task | Template | Analysis | Criteria | Vocabulary |
|---|---|---|---|---|---|---|
| `interview-buttoned-shirt` | Plain buttoned shirt | `cloth-v4` | none | none | collar, garment | collar buttoned, garment plain-shirt |
| `interview-tie-hair` | Hair tied back | `hair-transfer` | `female_casual_updo` | face shape | hair | hair bun |
| `interview-short-hair` | Hair short | `hair-transfer` | `female_crewcut` | face shape | hair | hair short |
| `interview-short-beard` | Short even beard | `beard-style` | `all_circle` | none | facial hair | beard goatee |
| `interview-nude-lip` | Quiet nude | `makeup-vto` | none | none | lip | lip colour nude |
| `interview-closed-smile` | Closed-mouth smile | `ai-smile` | none | none | expression | expression closed-mouth-smile |

Tie-back and short are separate fixes so memory can tell them apart. Light has no fix. Basket: garments (`shirt-buttoned`), lip colour (`lip-nude`).

## Festival

A festive garment, a festive earring, a necklace, a red or berry lip, finished hair off the eyes, facial hair absent or a full shape with a clean edge, an open expression, and warm even light.

| Fix | Changes | Task | Template | Analysis | Criteria | Vocabulary |
|---|---|---|---|---|---|---|
| `festival-festive-top` | Festive top | `cloth-v4` | none | none | garment | garment festive-top, collar mandarin |
| `festival-gold-jhumkas` | Gold jhumkas | `2d-vto/earring` | none | none | earring | jewellery jhumkas, colour gold |
| `festival-gold-chain` | Gold chain | `2d-vto/necklace` | none | none | necklace | necklace chain, colour gold |
| `festival-red-lip` | Red lip | `makeup-vto` | none | none | lip | lip colour red |
| `festival-tie-hair` | Hair tied back | `hair-transfer` | `female_casual_updo` | face shape | hair | hair bun |
| `festival-full-beard` | Full even beard | `beard-style` | `all_garibaldi` | none | facial hair | beard full |
| `festival-closed-smile` | Closed-mouth smile | `ai-smile` | none | none | expression | expression closed-mouth-smile |

The documented toothy smile is not used. Light has no fix. Basket: garments (`top-festive`), earrings (`gold-jhumkas`), necklaces (`gold-chain`), lip colour (`lip-red`).

The necklace page asks for a worn necklace as the reference, neck unobstructed in the selfie. If a flat product shot is rejected, the reference is a sample person's own neck (T0.4). Never a real person.

## Funeral

A dark plain garment, a closed plain collar, jewellery absent or one small stud, a lip that is bare or a quiet nude, hair off the face, facial hair absent or short and close, a still closed mouth, and plain even light.

| Fix | Changes | Task | Template | Analysis | Criteria | Vocabulary |
|---|---|---|---|---|---|---|
| `funeral-dark-shirt` | Dark plain shirt, collar buttoned | `cloth-v4` | none | none | garment, collar | garment dark-plain, collar buttoned |
| `funeral-nude-lip` | Quiet nude | `makeup-vto` | none | none | lip | lip colour nude |
| `funeral-tie-hair` | Hair tied back | `hair-transfer` | `female_casual_updo` | face shape | hair | hair bun |
| `funeral-short-beard` | Short close beard | `beard-style` | `all_circle` | none | facial hair | beard goatee |

Three criteria have no fix, and the suggestion says so. Festive jewellery cannot be removed. A wide smile cannot be removed. Light cannot be relit. The nude covers a bright lip; it does not restore a bare one. Basket: garments (`shirt-dark-plain`), lip colour (`lip-nude`).

## Catalog

T0.4a takes real product photos from web image search for the exact products (user decision: fair use) and makes colour swatches for the lip colours. No generated image stands for an exact product. No brand logos, watermarks or text, no third-party pattern, no real person.

| Id | Category | Fidelity | What to shoot |
|---|---|---|---|
| `gold-jhumkas` | earrings | exact | One jhumka, clear front, plain background, no ear |
| `gold-chain` | necklaces | exact | Plain gold chain; worn only if the task rejects a product shot, and then only a sample person's neck |
| `lip-red` | lip-colour | approximate | Unbranded red, bullet or smear on a card, no mouth |
| `lip-nude` | lip-colour | approximate | Unbranded nude, same rules |
| `shirt-open-collar` | garments | exact | One plain shirt, collar open, front product shot |
| `shirt-buttoned` | garments | exact | One plain shirt, collar buttoned |
| `top-festive` | garments | exact | One festive top, mandarin collar, plain colour or a simple weave |
| `shirt-dark-plain` | garments | exact | One dark plain shirt, collar buttoned |

Clothes references are product shots, not a second person. The clothes file spec wants the user's photo to show the face and the shoulders, from the chest upward. The integration guide on the same page also says a full-body photo. Those two disagree. The shot list should keep the collar and the shoulders in frame.

`garment_category` is not filled in. The only value in the public request example is `full_body`. The file spec also describes upper-body and lower-body regions, and `invalid_parameter` covers a bad category, but the allowed list is not on that page. T0.5 reads the enum before the first call. Do not guess one.

## Left off the menu

- **Light.** Every occasion judges it. The public lighting task sits with Creators (image editing), which is outside the qualifying sets, so there is no render.
- **Watch, ring, bracelet.** They need a wrist or a hand, not the selfie.
- **Hat, scarf, shoes, bag.** The fashion pages require an explicit gender value. This product does not ask for one and does not infer one.
- **Removing jewellery or a smile.** No qualifying task undoes either.
- **Hair colour.** Described in the vocabulary, not rendered. See above.
- **Skin smoothing, face reshape, face lift, body reshape, aging, eye-colour lenses.** A dress code does not ask for these, and a render that smooths or reshapes is discarded.
- **Skin scores, Fitzpatrick type, age, gender, ratios, attractiveness.** Never sent, never shown.

Beard styles are marketed as men's styles. The menu still offers the fix only when the user ticks it.

## For the sign-off

The questions to mark up are the Clef lines and the suggestions, the choice of red as the only family-function lip fix, and the festive-top mandarin collar. Strike a criterion rather than softening it into a score.
