// Global plugins every component test needs: $t and v-tooltip.
import { config } from '@vue/test-utils'
import { i18nPlugin, setLocale } from './i18n'
import { tooltip } from './directives/tooltip'
import { installMediaCall } from './lib/media/api'

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
config.global.directives = { tooltip }
setLocale('de')
