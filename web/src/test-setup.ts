// Global plugins every component test needs: $t, v-tooltip and v-max-chars.
import { config } from '@vue/test-utils'
import { beforeEach } from 'vitest'
import { clearAllDrafts } from './lib/drafts'
import { clearAllTrays } from './composables/attachmentTrayState'
import { i18nPlugin, setLocale } from './i18n'
import { maxChars } from './directives/maxChars'
import { tooltip } from './directives/tooltip'
import { installMediaCall } from './lib/media/api'
import { clock } from './lib/media/clock'

// The media tests step the clock with fake timers; the real clock is monotonic and has its own tests.
clock.now = () => Date.now()

// The real call loads the group stack. Tests that need it install their own.
installMediaCall({
  start() {},
  roster() {},
  receive() {},
  resume() {},
  stop() {},
  onKey() {},
  onWire() {},
  onNotice() {},
  onVerification() {},
  protectSender: () => true,
  prepareIncoming: () => true,
  protectIncoming: () => true,
})

config.global.plugins = [i18nPlugin]
config.global.directives = { tooltip, maxChars }
setLocale('de')

// Unsent drafts live in the page's storage: no test inherits another's.
beforeEach(() => clearAllDrafts())
// Nor do staged attachments carry over.
beforeEach(() => clearAllTrays())

// jsdom has no object URLs; the attachment tray previews files with them.
if (!URL.createObjectURL) Object.assign(URL, { createObjectURL: () => 'blob:test', revokeObjectURL: () => undefined })
