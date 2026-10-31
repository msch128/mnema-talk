// The server's specific error sentences (English, stable) mapped to the
// localized text the user should see. Anything not listed here falls back to
// the generic text of its error code plus the server message (see api.ts).
import { t } from '../i18n'
import { SENTENCES } from './errorSentences'
import { durationLabel } from './moderation'

const FIELDS = new Set(['content', 'bio', 'display_name', 'topic'])
// "name" is the one shared word for a name everywhere in the UI.
const fieldLabel = (field: string) => field === 'name' ? t('common.name') : FIELDS.has(field) ? t(`errors.field.${field}`) : field

/** Exact server message -> key under errors.detail. */
export const DETAILS: Readonly<Record<string, string>> = {
  'invalid invite code': 'invalidInvite',
  'invite code has expired': 'inviteExpired',
  'invite code usage limit reached': 'inviteUsedUp',
  'username is already taken': 'usernameTaken',
  'current password is incorrect': 'wrongPassword',
  'invalid or expired code': 'codeWrong',
  'the code is incorrect': 'codeWrong',
  'too many wrong codes today: use a recovery code': 'codeCapped',
  'two-factor sign-in is already on': 'twoFactorOn',
  'two-factor sign-in is not on': 'twoFactorOff',
  'this link is invalid or has expired': 'linkInvalid',
  'enter a valid e-mail address': 'emailInvalid',
  'this e-mail address is already confirmed for another account': 'emailInUse',
  'this server cannot send e-mail': 'mailOff',
  'password reset by e-mail is not available': 'mailOff',
  'there is no unconfirmed e-mail address': 'emailNoPending',
  'the confirmation e-mail could not be sent': 'emailSendFailed',
  'the poll has ended': 'pollEnded',
  'poll not found': 'pollGone',
  'this poll takes one answer': 'pollOneAnswer',
  'an option is given twice': 'pollOptionTwice',
  'you have already voted in this anonymous poll; a vote there cannot be changed': 'pollAlreadyVoted',
  'a vote in an anonymous poll cannot be withdrawn': 'pollNoWithdraw',
  'unknown option': 'pollUnknownOption',
  'a poll has 2 to 10 options': 'pollOptionCount',
  'an option is one line of text': 'pollOptionLine',
  'options must differ': 'pollOptionsDiffer',
  'a poll cannot be edited; delete it and ask again': 'pollNoEdit',
  'an emoji with this name already exists': 'emojiNameTaken',
  'name must be 2 to 32 characters: a-z, 0-9 and _': 'emojiName',
  'invalid emoji': 'emojiInvalid',
  'emoji not found': 'emojiGone',
  'emoji has no still image': 'emojiGone',
  'file is required': 'fileRequired',
  'invalid multipart form': 'fileUnreadable',
  'the file cannot be read': 'fileUnreadable',
  'animated WebP is not supported, use a GIF': 'emojiAnimatedWebp',
  'only PNG, GIF and WebP images are accepted': 'emojiImageType',
  'an emoji must be a PNG, GIF or WebP image': 'emojiImageType',
  'file storage is not configured': 'storageOff',
  'exports need object storage': 'storageOff',
  'there is no export to download': 'exportNone',
  'the download can only be started from this site': 'exportSameSite',
  'exports are being built for other members right now; try again in a few minutes': 'exportBusy',
  'push notifications are turned off on this server': 'pushOff',
  'device not found': 'deviceGone',
  'the Owner account cannot be deleted: ownership is not transferable, so the community would be left without an Owner': 'ownerDelete',
  'this action is not available for the Owner account': 'ownerProtected',
  'only the Owner can do this': 'ownerOnly',
  'you cannot moderate yourself': 'selfModeration',
  'the server is busy, try again shortly': 'serverBusy',
  'only the author can edit this message': 'editAuthorOnly',
  'only the author or a moderator can delete this message': 'deleteAuthorOnly',
  "the Owner's messages can only be deleted by the Owner": 'deleteOwnerMessage',
  'too many reactions on this message': 'reactionsLimit',
  'message to reply to not found': 'replyGone',
  'parent message not found': 'replyGone',
  'can only reply to a message in the same conversation': 'replyElsewhere',
  'invite code already exists': 'inviteExists',
  'this username is reserved': 'usernameReserved'
}

type ErrorPattern = readonly [RegExp, (...matches: string[]) => string]
const PATTERNS: readonly ErrorPattern[] = [
  [/^password must be at least (\d+) characters$/, n => t('errors.detail.passwordMin', { count: n })],
  [/^password must be at most/, () => t('errors.detail.passwordMax')],
  [/^username must be/, () => t('errors.detail.usernameFormat')],
  [/^(\w+) must be at most (\d+) characters$/, (field = '', n = '') => t('errors.detail.fieldMax', { field: fieldLabel(field), count: n })],
  [/^too many requests: retry in (\d+) s/, n => t('errors.detail.rateWait', { time: durationLabel(Number(n)) })],
  [/^slow mode: wait (\d+) s/, n => t('errors.detail.slowMode', { seconds: n })],
  [/^Your message was not sent: it breaks the (?:server|community) rule "(.*)" \(automod\)\./, rule => t('errors.detail.automodSend', { rule })],
  [/^Your edit was not saved: it breaks the (?:server|community) rule "(.*)" \(automod\)\./, rule => t('errors.detail.automodEdit', { rule })],
  [/^file exceeds the (\d+) MB limit$/, n => t('errors.detail.fileTooBig', { size: n })],
  [/^(missing permission|cannot (grant|allow|change) a permission you do not have)/, () => t('errors.detail.missingPermission')],
  [/^a channel can keep at most (\d+) pinned messages/, n => t('errors.detail.pinCap', { count: n })],
  [/^an account can use push on at most (\d+) devices/, n => t('errors.detail.pushDeviceLimit', { count: n })],
  [/^the limit of (\d+) custom emoji is reached$/, n => t('errors.detail.emojiLimit', { count: n })],
  [/^duration must be one of/, () => t('errors.detail.pollDuration')],
  [/^endpoint must be an https URL|^p256dh must be/, () => t('errors.detail.pushSubscription')],
  [/^the file must be between 1 byte and (\d+) KiB/, n => t('errors.detail.emojiSize', { size: n })],
  [/^the image is larger than (\d+) KiB/, n => t('errors.detail.emojiSize', { size: n })],
  [/^the image must be between 1 and (\d+) pixels/, n => t('errors.detail.emojiPixels', { size: n })]
]

// Values the server fills in as English words (an event status, "avatar", why an invite code is weak).
const WORDS: Readonly<Record<string, string>> = {
  'too repetitive': 'repetitive', 'needs letters and digits, or 16 or more characters': 'lettersDigits',
  'contains a common word or sequence': 'commonWord', avatar: 'avatar', banner: 'banner',
  scheduled: 'scheduled', active: 'active', completed: 'completed', cancelled: 'cancelled'
}
const SHORTER = /^shorter than (\d+) characters$/

function word(value: string): string {
  if (Object.prototype.hasOwnProperty.call(WORDS, value)) return t(`errors.word.${WORDS[value]}`)
  const shorter = SHORTER.exec(value)
  return shorter ? t('errors.word.shorter', { a: shorter[1] }) : value
}

const PLACEHOLDER_NAMES = ['a', 'b', 'c', 'd']

// A server format string such as "at most %d favourites" becomes a regex that
// reads the varying parts back out of the finished sentence.
function compile(format: string, key: string) {
  const source = format.split(/(%[dsq])/).map(part => {
    if (part === '%d') return '(\\d+)'
    if (part === '%q') return '"(.*)"'
    if (part === '%s') return '(.+?)'
    return part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  }).join('')
  return { key, pattern: new RegExp(`^${source}$`), literal: format.replace(/%[dsq]/g, '').length }
}

const SENTENCE_KEYS = new Map(SENTENCES.filter(([format]) => !format.includes('%')).map(([format, key]) => [format, key]))
// The most specific format (the most fixed text) wins over a generic "%s is required".
const SENTENCE_FORMATS = SENTENCES.filter(([format]) => format.includes('%')).map(([format, key]) => compile(format, key))
  .sort((a, b) => b.literal - a.literal)

function formatSentence(message: string): string | null {
  const exact = SENTENCE_KEYS.get(message)
  if (exact) return t(`errors.detail.${exact}`)
  for (const { key, pattern } of SENTENCE_FORMATS) {
    const match = message.match(pattern)
    if (match) return t(`errors.detail.${key}`, Object.fromEntries(match.slice(1).map((value, i) => [PLACEHOLDER_NAMES[i] ?? `p${i}`, word(value)])))
  }
  return null
}

/** The localized sentence for a known server message, or null. */
export function formatDetailMessage(message: string): string | null {
  if (Object.prototype.hasOwnProperty.call(DETAILS, message)) {
    return t(`errors.detail.${DETAILS[message]}`)
  }
  for (const [pattern, format] of PATTERNS) {
    const match = message.match(pattern)
    if (match) return format(...match.slice(1))
  }
  return formatSentence(message)
}
