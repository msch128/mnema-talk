// The Discord server import (doc/import.md): previewing a template, applying
// the confirmed plan, and listing, inspecting and undoing earlier imports.
import { api } from './api'
import { arrayDecoder } from '../types/validation'
import {
  decodeDiscordimportDetail, decodeDiscordimportPlan, decodeDiscordimportRecord, decodeDiscordimportUndoResult,
  isDiscordimportRecord
} from '../types/rest'
import type { DiscordimportDetail, DiscordimportPlan, DiscordimportRecord, DiscordimportUndoResult } from '../types/rest'

const BASE = '/api/admin/import/discord'

/** The word that confirms an import when the community has no name. */
export const CONFIRM_WORD = 'IMPORT'

/** Downloads the template and returns what importing it would create. Writes nothing. */
export function previewImport(template: string): Promise<DiscordimportPlan> {
  return api(`${BASE}/preview`, { method: 'POST', json: { template }, decode: decodeDiscordimportPlan })
}

/** Runs the import the preview described; the three confirmations travel with it. */
export function applyImport(template: string, plan: DiscordimportPlan, confirmText: string): Promise<DiscordimportRecord> {
  return api(`${BASE}/apply`, {
    method: 'POST',
    json: { template, plan_hash: plan.hash, confirm_additive: true, confirm_roles: true, confirm_text: confirmText },
    decode: decodeDiscordimportRecord
  })
}

export function listImports(): Promise<DiscordimportRecord[]> {
  return api(`${BASE}/imports`, { decode: arrayDecoder('discordimport.Record', isDiscordimportRecord) })
}

/** An import and what undoing it would do right now. */
export function importDetail(id: string): Promise<DiscordimportDetail> {
  return api(`${BASE}/imports/${id}`, { decode: decodeDiscordimportDetail })
}

export function undoImport(id: string, acceptMessageLoss: boolean): Promise<DiscordimportUndoResult> {
  return api(`${BASE}/imports/${id}/undo`, { method: 'POST', json: { accept_message_loss: acceptMessageLoss }, decode: decodeDiscordimportUndoResult })
}
