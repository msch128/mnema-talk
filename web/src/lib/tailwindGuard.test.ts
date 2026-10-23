import { describe, it, expect } from 'vitest'
import { matchTailwind3Class, scanFile } from '../../scripts/check-tailwind.ts'

describe('matchTailwind3Class', () => {
  it('flags bare outline-none and variants with outline-hidden replacement', () => {
    expect(matchTailwind3Class('outline-none')).toBe('outline-hidden')
    expect(matchTailwind3Class('focus:outline-none')).toBe('focus:outline-hidden')
    expect(matchTailwind3Class('focus-visible:outline-none')).toBe('focus-visible:outline-hidden')
  })

  it('flags flex-shrink utilities and suggests shrink replacements', () => {
    expect(matchTailwind3Class('flex-shrink-0')).toBe('shrink-0')
    expect(matchTailwind3Class('flex-shrink')).toBe('shrink')
    expect(matchTailwind3Class('sm:flex-shrink-0')).toBe('sm:shrink-0')
  })

  it('flags flex-grow utilities and suggests grow replacements', () => {
    expect(matchTailwind3Class('flex-grow')).toBe('grow')
    expect(matchTailwind3Class('flex-grow-0')).toBe('grow-0')
    expect(matchTailwind3Class('hover:flex-grow')).toBe('hover:grow')
  })

  it('flags shadow-sm and suggests shadow-xs replacement', () => {
    expect(matchTailwind3Class('shadow-sm')).toBe('shadow-xs')
    expect(matchTailwind3Class('max-md:shadow-sm')).toBe('max-md:shadow-xs')
  })

  it('skips rounded-sm because its old meaning is ambiguous in v4', () => {
    expect(matchTailwind3Class('rounded-sm')).toBeNull()
    expect(matchTailwind3Class('md:rounded-sm')).toBeNull()
  })

  it('flags overflow-ellipsis and box-decoration utilities', () => {
    expect(matchTailwind3Class('overflow-ellipsis')).toBe('text-ellipsis')
    expect(matchTailwind3Class('decoration-slice')).toBe('box-decoration-slice')
    expect(matchTailwind3Class('decoration-clone')).toBe('box-decoration-clone')
    expect(matchTailwind3Class('hover:decoration-slice')).toBe('hover:box-decoration-slice')
  })

  it('flags opacity utilities and suggests color opacity modifier replacement', () => {
    expect(matchTailwind3Class('bg-opacity-50')).toBe('bg-<color>/50')
    expect(matchTailwind3Class('text-opacity-75')).toBe('text-<color>/75')
    expect(matchTailwind3Class('border-opacity-10')).toBe('border-<color>/10')
    expect(matchTailwind3Class('hover:bg-opacity-80')).toBe('hover:bg-<color>/80')
  })

  it('flags prefix important form and converts to v4 suffix', () => {
    expect(matchTailwind3Class('!flex')).toBe('flex!')
    expect(matchTailwind3Class('!block')).toBe('block!')
    expect(matchTailwind3Class('hover:!bg-red-500')).toBe('hover:bg-red-500!')
    expect(matchTailwind3Class('!hover:bg-red-500')).toBe('hover:bg-red-500!')
    expect(matchTailwind3Class('!shadow-sm')).toBe('shadow-xs!')
    expect(matchTailwind3Class('!flex-shrink-0')).toBe('shrink-0!')
  })

  it('allows valid Tailwind 4 classes and non-class strings', () => {
    expect(matchTailwind3Class('shrink-0')).toBeNull()
    expect(matchTailwind3Class('grow')).toBeNull()
    expect(matchTailwind3Class('shadow-xs')).toBeNull()
    expect(matchTailwind3Class('outline-hidden')).toBeNull()
    expect(matchTailwind3Class('flex!')).toBeNull()
    expect(matchTailwind3Class('hover:bg-red-500!')).toBeNull()
    expect(matchTailwind3Class('')).toBeNull()
    expect(matchTailwind3Class('!')).toBeNull()
    expect(matchTailwind3Class('!!!')).toBeNull()
    expect(matchTailwind3Class('!../path')).toBeNull()
  })
})

describe('scanFile', () => {
  it('detects violations in Vue templates and scripts', () => {
    const vueCode = `<template>
  <div class="outline-none">
    <span :class="['flex', '!block', { 'shadow-sm': isSmall }]"></span>
  </div>
</template>
<script setup lang="ts">
const item = 'flex-grow'
</script>`

    const violations = scanFile('test.vue', vueCode)
    expect(violations).toEqual([
      { file: 'test.vue', line: 7, className: 'flex-grow', replacement: 'grow' },
      { file: 'test.vue', line: 2, className: 'outline-none', replacement: 'outline-hidden' },
      { file: 'test.vue', line: 3, className: '!block', replacement: 'block!' },
      { file: 'test.vue', line: 3, className: 'shadow-sm', replacement: 'shadow-xs' }
    ])
  })

  it('detects violations in TypeScript files', () => {
    const tsCode = `export const btn = 'flex-shrink-0 bg-opacity-50'`
    const violations = scanFile('test.ts', tsCode)
    expect(violations).toEqual([
      { file: 'test.ts', line: 1, className: 'flex-shrink-0', replacement: 'shrink-0' },
      { file: 'test.ts', line: 1, className: 'bg-opacity-50', replacement: 'bg-<color>/50' }
    ])
  })
})
