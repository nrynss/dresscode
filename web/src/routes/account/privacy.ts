import source from "../../../../product.md?raw";

/** The product file, with markdown bold markers removed. */
function plain(markdown: string): string {
  return markdown.replaceAll("**", "");
}

/** The product's own sentence, from `opening` through its full stop. */
export function productSentence(opening: string, markdown = source): string {
  const text = plain(markdown);
  const at = text.indexOf(opening);
  if (at < 0) throw new Error(`product.md has no sentence for ${opening}`);
  const rest = text.slice(at);
  const stop = rest.indexOf(".");
  if (stop < 0) throw new Error(`product.md sentence has no stop for ${opening}`);
  return rest.slice(0, stop + 1);
}

export interface PrivacyCopy {
  heading: string;
  cloudflare: string;
  clef: string[];
  youcam: string[];
  withheld: string[];
}

/** What YouCam and Cloudflare receive, in the product's sentences. */
export function privacyCopy(markdown = source): PrivacyCopy {
  const text = plain(markdown);
  const heading = "Who sees what";
  const cloudflare = "Cloudflare Workers AI runs Clef to read the dress code and check the render";
  if (!text.includes(heading)) throw new Error("product.md has no who-sees heading");
  if (!text.includes(cloudflare)) throw new Error("product.md has no Cloudflare line");
  return {
    heading,
    cloudflare,
    clef: [
      productSentence("Clef reads the photo and the occasion's written criteria", markdown),
      productSentence("Clef picks one fix from the occasion's menu", markdown),
      productSentence("That step is text only, with no photo sent.", markdown),
      productSentence("Clef sees the original and the render together", markdown),
    ],
    youcam: [
      productSentence("YouCam analysis runs only when the fix needs it", markdown),
      productSentence("YouCam renders the fix on the user's own photo.", markdown),
    ],
    withheld: [
      productSentence("Never sent to any model:", markdown),
      productSentence("Probabilities steer the pipeline and are never shown.", markdown),
    ],
  };
}

/** Product order: judge, pick, measure, render, check, then what is never sent. */
export function privacySentences(markdown = source): string[] {
  const copy = privacyCopy(markdown);
  const check = copy.clef[3];
  if (check === undefined) throw new Error("product.md is missing the render check");
  return [...copy.clef.slice(0, 3), ...copy.youcam, check, ...copy.withheld];
}

/** Account sentences from product.md. The screen shows these beside the controls. */
export function accountSentences(markdown = source): {
  keepsLooks: string;
  noGoogle: string;
  deletion: string;
  notAutomatic: string;
  stays: string;
} {
  return {
    keepsLooks: productSentence("Signing in keeps the guest's looks.", markdown),
    noGoogle: productSentence("No Google sign-in", markdown),
    deletion: productSentence("Account deletion removes your stored photos", markdown),
    notAutomatic: productSentence("Nothing is deleted automatically.", markdown),
    stays: productSentence("Photos, renders and history stay until the user deletes them.", markdown),
  };
}
