import type { Component } from 'vue'

export type MenuItemId = string | number

interface MenuItemBase {
  id?: MenuItemId
  label?: string
  icon?: Component
  shortcut?: string
  subtitle?: string
  disabled?: boolean
  danger?: boolean
  keepOpen?: boolean
  action?: () => void
}

export interface MenuSubmenu extends MenuItemBase {
  type: 'submenu'
  id: MenuItemId
  label: string
  value?: string | number | null
  items: ContextMenuItem[]
  minWidth?: number
  onOpen?: () => void
  onClose?: () => void
}

export type ContextMenuItem =
  | (MenuItemBase & { type?: 'action' })
  | (MenuItemBase & { type: 'separator' })
  | (MenuItemBase & { type: 'label'; label: string })
  | (MenuItemBase & { type: 'slider'; label: string; value: number; min?: number; max?: number; step?: number; onInput?: (value: number) => void })
  | (MenuItemBase & { type: 'radio' | 'checkbox'; label: string; checked?: boolean })
  | (MenuItemBase & { type: 'info'; label: string; value?: string | number | null })
  | MenuSubmenu

export type MenuAnchor = Element | { left?: number; top?: number; right?: number; bottom?: number; x?: number; y?: number; height?: number }
