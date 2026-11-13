// Bot manifests and the add link (doc/bots.md, "Adding a bot from a manifest").
import type { IntentName } from './constants.ts'
import type { CommandInput } from './types/commands.ts'
import { validateCommands } from './builders/commands.ts'
import { checkLength, fail } from './builders/validate.ts'
import { trimSlash } from './util.ts'

export type ManifestPermission =
  | 'view_channel'
  | 'send_messages'
  | 'attach_files'
  | 'add_reactions'
  | 'manage_messages'
  | 'mention_everyone'

export interface Manifest {
  manifest_version: 1
  /** 1 to 24 characters. */
  name: string
  /** Up to 200 characters. */
  description?: string
  /** An https URL of up to 512 characters. */
  homepage?: string
  /** A data: URL of a PNG, JPEG or WebP image of at most 256 KB. */
  avatar?: string
  intents?: IntentName[]
  permissions?: ManifestPermission[]
  /** At most 50, as for PUT /api/bot/v1/commands. */
  commands?: CommandInput[]
}

function validateManifest(m: Manifest): void {
  if (m?.manifest_version !== 1) fail('manifest_version must be 1')
  checkLength('manifest name', m.name, 1, 24)
  if (m.description !== undefined) checkLength('manifest description', m.description, 0, 200)
  if (m.homepage !== undefined) {
    checkLength('manifest homepage', m.homepage, 1, 512)
    if (!m.homepage.startsWith('https://')) fail('manifest homepage must be an https URL')
  }
  if (m.avatar !== undefined && !/^data:image\/(png|jpeg|webp);base64,/.test(m.avatar)) {
    fail('manifest avatar must be a data: URL of a PNG, JPEG or WebP image')
  }
  if (m.commands !== undefined) validateCommands(m.commands)
}

/** The manifest as base64url of its JSON (UTF-8), the fragment of the add link. */
export function encodeManifest(manifest: Manifest): string {
  validateManifest(manifest)
  return Buffer.from(JSON.stringify(manifest), 'utf8').toString('base64url')
}

/** The JSON of an encoded manifest (the inverse of encodeManifest). */
export function decodeManifest(encoded: string): Manifest {
  return JSON.parse(Buffer.from(encoded, 'base64url').toString('utf8')) as Manifest
}

/**
 * The link that opens the server's confirm screen for this manifest:
 * `${serverUrl}/admin/bots/add#manifest=<base64url JSON>`. The manifest is in
 * the fragment, which the browser never sends to the server.
 */
export function addBotLink(serverUrl: string, manifest: Manifest): string {
  return `${trimSlash(serverUrl)}/admin/bots/add#manifest=${encodeManifest(manifest)}`
}
