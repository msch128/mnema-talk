// A reload (every spec starts with one) cancels the requests still on their
// way, and WebKit reports such a cancelled API request as a page error of its
// own, "Fetch API cannot load <url> due to access control checks", although the
// app handles the failed request. Playwright splits an error's text at the
// first colon into name and message, so that text arrives as the name
// "Fetch API cannot load http" and the message "/127.0.0.1:<port>/api/...":
// the signature has to be read from both halves together, never from the
// message alone.
const TAIL = String.raw`127\.0\.0\.1:\d+\/api\/\S+ due to access control checks\.`
const WHOLE = new RegExp(String.raw`^Fetch API cannot load https?:\/\/${TAIL}$`)
// Playwright drops two characters after the colon, ":/" of "://".
const SPLIT = { name: /^Fetch API cannot load https?$/, message: new RegExp(String.raw`^\/?\/?${TAIL}$`) }

/** True for WebKit's own report of a cancelled request to the app's API; every other error is the app's. */
export function isCancelledApiRequest(err: { name: string; message: string }): boolean {
  return WHOLE.test(err.message) || (SPLIT.name.test(err.name) && SPLIT.message.test(err.message))
}
