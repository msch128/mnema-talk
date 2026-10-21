// Own sources the coverage gate must see, excluding type-only modules.
import { readFileSync, readdirSync, type Dirent } from 'node:fs'
import { join, relative } from 'node:path'
import ts from 'typescript'

const transpileOptions: ts.CompilerOptions = { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext }

export function isTestOrFixture(path: string): boolean {
  if (path.endsWith('.test.ts')) return true
  return path.endsWith('.fixture.ts')
}

function isTypeScriptOrVue(name: string): boolean {
  if (name.endsWith('.ts')) return true
  return name.endsWith('.vue')
}

function isExecutableName(name: string): boolean {
  if (!isTypeScriptOrVue(name)) return false
  if (name.endsWith('.d.ts')) return false
  if (name === 'test-setup.ts') return false
  return !isTestOrFixture(name)
}

function isTypeOnlyStatement(statement: ts.Statement): boolean {
  if (ts.isImportDeclaration(statement)) return true
  if (ts.isExportDeclaration(statement)) return true
  return ts.isEmptyStatement(statement)
}

function isTypeOnlyModule(path: string, source: string): boolean {
  const emitted = ts.transpileModule(source, { compilerOptions: transpileOptions }).outputText
  const ast = ts.createSourceFile(path, emitted, ts.ScriptTarget.ES2022, true)
  return ast.statements.every(isTypeOnlyStatement)
}

function skipTypeOnly(absolute: string): boolean {
  if (!absolute.endsWith('.ts')) return false
  return isTypeOnlyModule(absolute, readFileSync(absolute, 'utf8'))
}

function relativeSource(root: string, absolute: string): string[] {
  if (skipTypeOnly(absolute)) return []
  return [relative(root, absolute).replaceAll('\\', '/')]
}

function nestedSources(root: string, name: string, path: string): string[] {
  if (name === 'third_party') return []
  return listSources(root, path)
}

function listedPaths(root: string, directory: string, entry: Dirent): string[] {
  const path = join(directory, entry.name)
  if (entry.isDirectory()) return nestedSources(root, entry.name, path)
  if (!entry.isFile()) return []
  if (!isExecutableName(entry.name)) return []
  return [path]
}

function listSources(root: string, directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => listedPaths(root, directory, entry))
}

export function executableSources(root: string): string[] {
  return listSources(root, join(root, 'src')).flatMap(absolute => relativeSource(root, absolute))
}
