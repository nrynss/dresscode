# The video: shot list and script (T6.3)

A public video, 1–3 minutes, shot on a phone, of the real hosted app at
`https://dresscode.nryn.dev`. The second visit is the spine. This document is
written to be followed cold: every shot names the screen, the exact taps, the
caption text, and the voiceover line.

Ground rules the recording must keep:

- One evaluation number, said early: **269 of 273 criterion answers correct
  (98.5%)** on the recorded evaluation set — 37 sample photos across the five
  occasions (docs/evaluation.md). It is said once, in shot 1, and not repeated.
- Every YouCam API is named in a caption at the moment it works on screen.
- The person in the photos is an **AI-generated sample, not a real human** —
  said in shot 4, when the photo first fills the screen, and repeated on the
  end card.
- No brand or app names other than Dresscode and the providers it names
  (YouCam, Cloudflare Workers AI). No logos, no third-party footage, no
  scores or probabilities on screen. No music — if music is licensed later,
  the two render waits (shots 7 and 11) are where it goes; nothing here
  depends on it.
- Total: 15 shots, **168 seconds** (~2:48), leaving cut margin under the
  3:00 ceiling. Record each shot as its own clip.

## The spine in five lines

1. First visit, family function: two criteria missed, one fix picked.
2. The jhumka render lands on the photo; the shop list is labelled; the look
   is kept.
3. The red lip is tried and dismissed — one tap, and it is remembered.
4. Second visit: the memory line names both verdicts before anything runs.
5. The jhumkas come back first, the red lip never comes back, and the render
   lands on the same photo.

## The shot list

Times are the target length of the cut, not the recording. "Caption" is
on-screen text; "VO" is the voiceover line, word for word.

### Shot 1 — The front door (10s)

- On screen: the Occasion tab. The five occasion cards ("Date", "Family
  function", "Interview", "Festival", "Funeral") and, below them, the folded
  "Sample looks" drawer.
- Taps: none. Hold on the cards.
- Caption: **Dresscode — the dress-code mirror.**
- VO: "Every occasion has a dress code. Dresscode lets you see it before
  they do — and remembers what you kept. On our recorded evaluation set of
  thirty-seven sample photos, its dress-code readings match the annotated
  truth on 269 of 273 checks — ninety-eight point five percent."

### Shot 2 — Turn style history on (10s)

- On screen: the History tab. The "Style history" heading with its switch,
  and the notice sentence under it.
- Taps: History tab → tap the switch on.
- Caption: **Style history is opt-in. What it keeps is text: the occasion,
  what you kept, what you dismissed.**
- VO: "Memory is a switch, and it is off until you turn it on. What it
  remembers is text — no scores, no analysis."

### Shot 3 — Pick the occasion and the open-to fixes (8s)

- On screen: back on the Occasion tab. Tap "Family function"; the menu opens
  under the fieldset legend "Fixes to try" with every tick empty. **No memory
  line above the menu — this is the first visit, and the empty space is the
  point.**
- Taps: Occasion tab → tap the "Family function" card → tick "Gold jhumkas"
  → tick "Red lip".
- Caption: **You choose the fixes you're open to. Nothing is inferred from
  your face.**
- VO: "First visit. I pick family function, and I say which fixes I'm open
  to — jewellery, makeup. Nothing is assumed."

### Shot 4 — The photo (10s)

- On screen: the Mirror tab. Tap "Choose photo"; the gallery opens; pick the
  sample photo. The arched preview fills with it.
- Taps: Mirror tab → "Choose photo" → pick the saved sample photo.
- Caption: **The person in every photo in this video is an AI-generated
  sample. Not a real human.**
- VO: "One selfie — and say it plainly: this person is AI-generated, a sample
  the whole demo runs on. No real face is in this video."

### Shot 5 — The judge (12s)

- On screen: tap "Check look". The verdict screen opens and the stage list
  runs ("Checking" lights, then "Choosing a fix"). When it finishes, open
  "Dress code notes": two lines — "The ear reads plain; try gold jhumkas."
  and "The lip reads bare; try a red lip."
- Taps: "Check look" → wait → tap "Dress code notes" open.
- Caption: **The judge reads the photo and the occasion's written criteria —
  nothing else. It runs on Cloudflare Workers AI. It answers in reasons,
  never a score.**
- VO: "The check reads the photo against the dress code and answers in
  reasons, worded as suggestions. No score, no percentage — ever."

### Shot 6 — The pick (8s)

- On screen: the verdict card: "A fix — Adds gold jhumkas." with the link
  "See the render".
- Taps: none. Hold on the card.
- Caption: **Choosing a fix is text only — the photo is never sent.**
- VO: "The pick is text only: the photo never leaves. A second fix waits if
  this one fails its check."

### Shot 7 — The render (14s)

- On screen: tap "See the render". The render screen opens; the divider of
  the before/after swipe sits mid-frame. Drag it both ways: original, render,
  original.
- Taps: "See the render" → drag the swipe divider left, right, left.
- Caption: **YouCam AI Earring Virtual Try-On — Jewelry & Watch set —
  renders the jhumkas on this photo. The original is one swipe away.**
- VO: "The fix is rendered on my own photo by YouCam's earring try-on. The
  original never goes away."

### Shot 8 — The render is checked (8s)

- On screen: still the render screen, the swipe at the "after" side.
- Taps: none.
- Caption: **Before anything is shown, the render is checked: was the change
  made, is it still the same person, is the criterion met now. A render that
  fails is discarded, never shown.**
- VO: "Nothing reaches you unchecked. A failed render is discarded and the
  next fix gets tried."

### Shot 9 — Shop the look (12s)

- On screen: tap "Shop the look". The sheet opens over "Every product is
  labelled for what it is." Tap a row's label; the popover opens ("This is
  the exact piece…"); close it.
- Taps: "Shop the look" → tap one product's label → dismiss the popover.
- Caption: **Every product is labelled: exact product, approximate match, or
  visual simulation.**
- VO: "The shop list tells you what each thing is. Nothing pretends to be
  more than it is."

### Shot 10 — Keep (10s)

- On screen: the render screen's verdict bar. Tap "Keep"; the button fills as
  the stored one.
- Taps: "Keep".
- Caption: **Kept. Style memory writes it down: family function, the gold
  jhumkas.**
- VO: "I'm keeping this. With history on, that's one sentence Dresscode will
  remember."

### Shot 11 — The red lip, dismissed (16s)

- On screen: Mirror tab again. The menu: untick "Gold jhumkas", leave "Red
  lip" ticked. Same photo, "Check look". The pick: "Paints the lip red." The
  render shows it (caption). Then the verdict bar: tap "Dismiss".
- Taps: Mirror tab → untick "Gold jhumkas" → "Check look" → wait → "Dismiss".
- Caption: **YouCam AI Makeup Virtual Try-On — Beauty set — paints the lip.
  Not for me. Dismissing is one tap, and it is remembered.**
- VO: "The lip they suggested? I try it, and it's not for me. One tap, and
  that answer is remembered too."

### Shot 12 — The second visit: the line (14s)

- On screen: Occasion tab. Tap "Family function". Above "Fixes to try", the
  banner is already there. Hold on it.
- Taps: Occasion tab → tap "Family function" → hold.
- Caption: **No photos, no scores — your verdicts, kept as text.**
- Banner on screen (the app's own words): **Last family function you kept
  the gold jhumkas and dismissed the red lip.**
- VO: "Second visit. Before anything runs, Dresscode remembers: last time I
  kept the gold jhumkas and dismissed the red lip."

### Shot 13 — The second look (14s)

- On screen: tick "Gold jhumkas" and "Red lip" as before; "Choose photo" —
  the same sample photo; "Check look". The pick card: "Adds gold jhumkas."
  The render lands on the same photo; drag the swipe once.
- Taps: tick both fixes → "Choose photo" → same photo → "Check look" → wait →
  "See the render" → drag the divider.
- Caption: **The dismissed fix was removed before the model was asked. It
  cannot come back — not first, not as runner-up.**
- VO: "Same photo, same occasion. The jhumkas are the fix. The red lip isn't
  offered at all — it was dismissed, so it's out of the choice entirely."

### Shot 14 — The shop list, kept first (10s)

- On screen: "Shop the look" on the second look. The jhumka rows lead.
- Taps: "Shop the look" → slow scroll down the list.
- Caption: **Memory orders the list: what you kept, first.**
- VO: "And the list leads with what I kept. Small thing. It's the whole
  point."

### Shot 15 — Free for judges, and the close (12s)

- On screen: Occasion tab. Open the "Sample looks" drawer; tap "Family
  function". The door opens straight onto the finished render (the recorded
  story, stages already done). Hold, then the end card.
- Taps: open "Sample looks" → tap "Family function".
- End card text: **Dresscode. The person here is an AI-generated sample.
  No scores. Every product labelled.**
- VO: "Judges open sample looks free — same story, nothing spent. Dresscode:
  see the dress code before they do."

## Recording checklist

Preconditions — do these once, before shot 1:

1. **Phone:** portrait, charged, Do Not Disturb on, brightness up, screen
   recorder set to its highest quality. Wipe other apps off the screen.
2. **The app:** `https://dresscode.nryn.dev` open in the browser, signed in
   or as the guest you will keep for the whole recording. Do not clear site
   data between shots — the guest session is what makes the second visit the
   same account as the first.
3. **The photo:** save `testdata/samples/woman/V04-family-function.jpg` from
   the repository to the phone's gallery once. It is the AI-generated sample
   person, and both visits must use this exact same file (the look is keyed
   to the photo).
4. **Style history on** (shot 2) and left on for the rest of the recording.
   The second visit's line is only served while the switch is on.
5. **Spend allowed:** the recording runs three paid looks — earring, lip,
   earring — one YouCam render each, 1 unit per render: **3 units total**
   (docs/youcam.md). The judge and render checks are Cloudflare Workers AI
   calls, not units. If a look stops with a spend or budget message, the
   spend guard refused it — resolve that before recording, because a refused
   render breaks the spine.

Order is load-bearing — record shots in this order:

- Shot 10's "Keep" and shot 11's "Dismiss" are what the memory line in shot
  12 is made of. The line only appears once both verdicts have landed, and
  only for looks that ended with one.
- After shot 11, open the Occasion tab once and confirm the banner shows
  before you shoot shot 12. If the line is missing or half there, a memory
  write did not land — re-run that look and tap the verdict again. (A stray
  tap can be out-voted: re-run the same look and tap the right button; the
  strongest verdict wins — dismissed beats bought, bought beats kept — but
  the clean fix is simply re-tapping the intended one.)
- Retakes of shots 1–11: record the shot again and tap the same verdicts. If
  the first visit must be re-recorded after the dismissal already happened
  (the banner would show too early), start clean: clear site data for the
  site (fresh guest), turn history on again, redo keep and dismiss, then
  continue. Nothing is deleted automatically, so an old take's looks never
  poison a fresh guest.

Clean screen state before every shot: navigate to the named tab, wait for
its content to finish loading, no dialog or popover open, no error banner,
and the ticks in the state the shot names. Record each shot as its own clip;
start the clip a breath before the first tap and hold a breath after the
last state settles.

If a stage runs long (the live render can take tens of seconds), keep
rolling — the stage word on screen is the honest wait — and cut the dull
middle in edit, landing on the moment the stage completes.

Why the steps are what they are (so a retake can be reasoned out):

- The second visit's line is real memory, not playback: with history on, a
  kept or dismissed verdict writes one sentence ("Family function kept the
  gold jhumkas.") to the account's style memory, and the next real look at
  that occasion reads it back — the banner on the occasion screen, the
  dismissed fix excluded from the pick before any model is asked, and the
  shop list reordered kept-first.
- The "Sample looks" drawer is different by design: it replays a recorded,
  cached story (judge, pick, render, check, shop — the same beats), it costs
  nothing, and it carries no memory — opening the same sample twice returns
  the same finished look, never a second visit. That is why the spine runs
  on real looks, and why the sample beat is only the closing "free for
  judges" shot.
