# Dresscode

*The dress-code mirror. Every occasion has a dress code. Dresscode lets you see it before they do.*

Entry for the YouCam API Skin AI & eCommerce VTO Hackathon (Devpost). Submit by 2 Nov, 11:45 EST (22:15 IST). The full brief, with the reasoning behind each decision, is kept outside this repository; this file is the product as it will be built.

## The product

People are judged against dress codes all the time: first dates, family functions, interviews, festivals. The judgement comes after the photo is public or the door is open. Dresscode gives a private first look, and the shopping list that fixes it.

The user picks an **occasion** and brings **one selfie**, plus any products they are considering. Then:

1. **Check against the dress code.** Clef reads the photo and the occasion's written criteria, nothing else, and flags the criteria the look misses: "the collar reads formal", "the face is lit flat". The app shows them as reasons, worded as a suggestion. **No score is ever shown.**
2. **Pick a fix.** Clef picks one fix from the occasion's menu, limited to the fixes the user ticked as open to. The call is text only. Jev, on OpenRouter, takes the same call and is kept so the two can be compared. That comparison is still required. Clef remains the picker until it is done.
3. **Measure, only for that fix.** YouCam analysis runs only when the fix needs it: colour tones for a shade match, face shape and hair type for a hairstyle. Each result is cached for that photo.
4. **Render.** YouCam renders the fix on the user's own photo. The original is always one tap away.
5. **Check the render.** Clef sees the original and the render together and answers: was the change made, is it still the same person, is the criterion met now, and what does the look show (a fixed vocabulary, for memory). A render that fails either of the first two is discarded, never shown; the runner-up fix is tried, and after that the suggestion stands without a render.
6. **Shop the look.** Every product is labelled **exact product**, **approximate match** or **visual simulation**.
7. **Remember (opt-in).** With style history on, Lambo keeps the occasion, the flagged criteria, the fixes kept and dismissed, the products, and the look in words: "Last family function you kept the gold jhumkas and dismissed the red lip."

**The moment the demo is built around:** a second visit. The user picks "family function" again; Dresscode remembers the jhumkas they kept, skips the red lip they dismissed, and the render lands on their own photo with the products labelled honestly.

## Occasions

Date, family function, interview, festival, funeral. Each occasion is a dress code (5–8 yes/no criteria about clothing, grooming, lighting and expression only), a fix menu and a retail basket. The occasion is state at every step, not a label.

**Fix menus are opt-in.** The user ticks the fixes they're open to: beard, makeup, hair colour, jewellery and so on. The pipeline never infers gender or anything else from the face to decide which fixes apply.

## Who sees what

| Step | Model | Receives | Never receives |
|---|---|---|---|
| Judge | Clef | the photo, the occasion, its criteria | YouCam analysis |
| Pick a fix | Clef | flagged criteria, occasion, the opt-in menu, allowlisted attributes (face shape, hair type and length, colour tones), recall if history is on | the photo |
| Check the render | Clef | original, render, the requested change, the look vocabulary | YouCam analysis |

Never sent to any model: face ratios, Skin Analysis scores, any age or attractiveness estimate. Probabilities steer the pipeline and are never shown.

## Accounts (decided 30 Oct)

- **Every visitor is a guest user.** A real user row, a server-side session, a signed cookie carrying only the session id. A guest can do everything, style history included.
- **Sign in by emailed code** (Resend) to keep history across devices. Signing in keeps the guest's looks.
- **No Google sign-in** for the event.
- **Deletion finishes.** A photo, a look, the style history, or the whole account (after a fresh code). Account deletion fans out across stored files, rows and the Lambo session, survives a restart, and reports when every part has confirmed.
- **Nothing is deleted automatically.** Photos, renders and history stay until the user deletes them.

## Style memory (decided 30 Oct)

**Text only.** Everything Lambo keeps is text, embedded with `@cf/baai/bge-m3` on Workers AI, one Lambo session per account. Clef's render check describes the look through choice questions over a fixed vocabulary (jewellery and its colour, lip colour, collar, hair, …); the app joins the answers into a line Lambo remembers. The fixed vocabulary means the same look is always described the same way.

Not built: recall by visual similarity. It needs image embeddings no host serves, and a local GPU service to keep up through judging.

## Sample people (user decision)

- The demo, the video and the samples judges open use **four AI-generated people**: an Indian man and woman, a white woman and an East Asian man. No photo of a real person is used anywhere, the author included.
- Each is an original design, never generated from a real photo or to look like anyone.
- The video and the README say the people are generated.
- The second-visit story is the Indian woman's. The two men cover the beard criteria, the two women the earring and lip criteria.

## Shape

| Layer | Built on |
|---|---|
| Server | One Go binary on **Keel v0.9.0**: gate, lease, cost (budgets in YouCam units and dollars), job, stream, providertask, mediastore, upload, photo, fetch, identity, erase, sqlite, flag, config, id, wire |
| App | **Svelte 5 + Chaaya 0.5.0**, a mobile-first installable web app built as a static single-page app (Capacitor can wrap it later). Chaaya: capture, upload, job stream, priced actions, share intake, theme, accessibility gates |
| Decisions | **Cloudflare Workers AI** over REST: Clef, Clef-flash |
| Try-on and analysis | **YouCam API v2**, India region |
| Memory | **Lambo v0.8.0**, beside the server, multi-session, embeddings on Workers AI |
| Mail | **Resend**, for sign-in codes only |
| Hosting | The author's own machine behind a Cloudflare Tunnel (HTTPS for camera and install) |

Architecture in [`docs/architecture.md`](docs/architecture.md).

## Hackathon rules that shape the build

- **At least one YouCam API from the Skin, Beauty, Fashion, Jewelry & Watch or Hair & Beard sets, used meaningfully.** Image editing or video alone don't qualify.
- **Judges get free, unrestricted access through judging** (rules: 3–20 Nov; Devpost schedule: 9–20 Nov). A public URL with sample selfies whose analysis is cached, behind spend guards that refuse before they spend.
- **A public 1–3 minute video** showing the app on its intended device (a phone) and explaining the YouCam APIs used.
- **The repository** is public with a licence, or private and shared with the organiser's address.
- **The unit budget is real:** 1,000 YouCam units for development, the demo and judging together.

## Not this

- Not a rating. No score, no percentage, no attractiveness anything.
- Not a beauty filter. A render that smooths skin or reshapes the face is discarded.
- Not a store. Products are labelled for what they are; nothing is sold here.

## Open decisions

- Compare Clef and Jev on the same fix-selection requests. Clef remains the picker until that comparison is done.
- The occasions' criteria, fix menus and the look vocabulary (T0.3).
