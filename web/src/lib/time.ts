// One place for how a time of day is written. A 12-hour clock gets
// `hour: 'numeric'`: no leading zero on the hour ("1:13 PM", like most chat
// apps; '2-digit' would force "01:13 PM"). A 24-hour locale keeps two digits
// ("09:05", "13:13"), as before.
export function clockTimeOptions(locale?: string): Intl.DateTimeFormatOptions {
  let twelveHour = false
  try {
    twelveHour = new Intl.DateTimeFormat(locale || undefined, { hour: 'numeric' }).resolvedOptions().hour12 === true
  } catch {
    // Unknown locale tag: fall back to the 24-hour form.
  }
  return { hour: twelveHour ? 'numeric' : '2-digit', minute: '2-digit' }
}
