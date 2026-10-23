// The tree the chat Markdown parser produces. Renderers walk this tree and
// escape every string on the way out, so nothing the user typed can become
// markup: only the node kinds below exist.

export type FormatKind = 'strong' | 'em' | 'u' | 'del' | 'spoiler'

export type Inline =
  | { t: 'text'; v: string }
  | { t: 'code'; v: string }
  | { t: 'fmt'; k: FormatKind; c: Inline[] }
  /** bare: a URL typed in the text; auto: <https://…> (no preview card); masked: [text](https://…). */
  | { t: 'link'; kind: 'bare' | 'auto' | 'masked'; href: string; confirm: boolean; c: Inline[] }
  /** A generated element (mention pill, custom emoji). Tag and attributes are validated when rendered. */
  | { t: 'el'; tag: ElementTag; attrs: Readonly<Record<string, string>>; c: Inline[] }

export type ElementTag = 'span' | 'img'

export interface ListItem {
  c: Inline[]
  /** Lists nested below this item. */
  sub: Block[]
}

export type Block =
  | { t: 'p'; c: Inline[] }
  | { t: 'h'; level: 1 | 2 | 3; c: Inline[] }
  | { t: 'sub'; c: Inline[] }
  | { t: 'quote'; c: Block[] }
  | { t: 'list'; ordered: boolean; start: number; items: ListItem[] }
  | { t: 'code'; lang: string; v: string }
