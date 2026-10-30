import type { ShownError } from "./types";

/** Sentences for the recorded codes. The screen branches on the code and shows these. */
const sentences: Record<string, string> = {
  budget_refused: "Dresscode cannot take on a new look right now.",
  chunk_mismatch: "A piece of the upload did not match. Dresscode will need it again.",
  conflict: "That upload was already saved.",
  fetch_bad_port: "That link uses a port Dresscode does not open.",
  fetch_bad_scheme: "That link has to be a secure web address.",
  fetch_bad_status: "That link did not answer.",
  fetch_bad_type: "That link was not a page or a photo Dresscode can use.",
  fetch_bad_url: "That link could not be read.",
  fetch_blocked_address: "That link was refused.",
  fetch_timeout: "That link took too long.",
  fetch_too_large: "That page was too large to use.",
  fetch_too_many_redirects: "That link redirected too many times.",
  fetch_unreachable: "That link could not be reached.",
  guest_data_conflict: "Signing in would leave this device's looks on the guest. Switch to the account, or stay a guest.",
  hash_mismatch: "The finished upload did not match. Start it again.",
  heic: "That photo is HEIC. Save it as a JPEG and try again.",
  incomplete: "The upload is still missing a piece.",
  internal_error: "Something went wrong on the way. Try again.",
  invalid_code: "That code did not work. Request a new one.",
  invalid_request: "That request could not be used.",
  job_failed: "This look could not be finished.",
  lease_refused: "This session cannot start another fresh look.",
  limit_exceeded: "That is too large to store.",
  no_face_in_photo: "No face is visible in that photo. Try another.",
  not_found: "That is not available.",
  output_cannot_fit: "That photo could not be stored small enough. Try another.",
  rate_limited: "Too many looks were started. Wait a moment, then try again.",
  renders_paused: "Renders are paused. The suggestion still stands.",
  samples_only: "Only the sample looks are open.",
  send_limited: "Too many codes were sent. Wait a moment, then try again.",
  storage_error: "The upload could not be saved. Try again.",
  too_many_input_bytes: "That photo file is too large to send. Try a smaller one.",
  too_many_pixels: "That photo is too detailed to read. Try a smaller one.",
  too_small: "That photo is too small to judge. Try a closer one.",
  unsupported_format: "That photo could not be read. Try a JPEG or a PNG.",
  unsupported_type: "That file type cannot be stored.",
  upload_a_photo: "That page had nothing Dresscode can use. Add a photo of the product instead.",
  uploads_paused: "New selfies are paused. A sample look is still open.",
  http_error: "The request was refused.",
  network: "Dresscode could not reach the server.",
  timeout: "The request took too long.",
};

function detailString(detail: unknown, key: string): string | undefined {
  if (typeof detail !== "object" || detail === null) return undefined;
  const value = (detail as Record<string, unknown>)[key];
  return typeof value === "string" ? value : undefined;
}

function specific(error: ShownError): string | undefined {
  if (error.code === "lease_refused") {
    const cap = detailString(error.detail, "cap");
    if (cap === "fresh_selfies") return "This session cannot start another selfie. A sample look is still open.";
    if (cap === "renders") return "This session cannot start another render. The suggestion still stands.";
  }
  if (error.code === "limit_exceeded") {
    const limit = detailString(error.detail, "limit");
    if (limit === "upload") return "That upload is too large. Try a smaller photo.";
    if (limit === "owner") return "There is no room left for another photo.";
  }
  if (error.code === "invalid_request") {
    const field = detailString(error.detail, "field");
    if (field === "occasion") return "That occasion is not one Dresscode knows.";
    if (field === "choice") return "That sign-in choice is not one Dresscode honours.";
  }
  if (error.code === "http_error" && error.status === 404) return "That is not available.";
  return undefined;
}

/** The recorded codes this screen knows how to say. Transport codes are not in the set. */
export function recordedSentences(): Record<string, string> {
  const copy: Record<string, string> = {};
  for (const [code, sentence] of Object.entries(sentences)) {
    if (code === "http_error" || code === "network" || code === "timeout") continue;
    copy[code] = sentence;
  }
  return copy;
}

/** A sentence for a typed wire error. Known codes never fall through to the wire phrase. */
export function humanMessage(error: ShownError): string {
  return specific(error) ?? sentences[error.code] ?? fallback(error.message);
}

function fallback(message: string | undefined): string {
  const text = message?.trim() ?? "";
  if (text === "") return "Something went wrong.";
  return text;
}
