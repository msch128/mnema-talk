// Global plugins every component test needs: $t and v-tooltip.
import { config } from '@vue/test-utils'
import { i18nPlugin, setLocale } from './i18n'
import { tooltip } from './directives/tooltip'

config.global.plugins = [i18nPlugin]
config.global.directives = { tooltip }
setLocale('de')
