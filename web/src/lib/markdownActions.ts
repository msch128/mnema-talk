// What clicking inside rendered Markdown does beyond plain browsing: copy a
// code block, and open a link whose text does not name its destination only
// after the reader confirmed where it leads.
import { confirm } from './confirm'
import { t } from '../i18n'
import { useToastStore } from '../stores/toast'

/** Copies the code of the block the "copy" button belongs to. */
export async function copyCodeBlock(button: Element): Promise<void> {
  const code = button.closest('.md-codeblock-wrap')?.querySelector('pre code')?.textContent
  if (code === null || code === undefined) return
  const toasts = useToastStore()
  try {
    await navigator.clipboard.writeText(code)
    toasts.success(t('chat.codeCopied'))
  } catch {
    toasts.error(t('chat.copyCodeFailed'))
  }
}

function openInNewTab(href: string): void {
  const a = document.createElement('a')
  a.href = href
  a.target = '_blank'
  a.rel = 'noopener noreferrer nofollow'
  a.click()
}

/** Asks first, then opens in a new tab exactly like an ordinary link would. */
export async function openConfirmedLink(anchor: HTMLAnchorElement): Promise<void> {
  const href = anchor.getAttribute('href') ?? ''
  const host = anchor.dataset.host || href
  const ok = await confirm({
    title: t('chat.linkConfirmTitle'),
    body: t('chat.linkConfirmBody', { host }),
    excerpt: href,
    confirmLabel: t('chat.linkOpen')
  })
  if (ok) openInNewTab(href)
}
