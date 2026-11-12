import { describe, expect, it } from 'vitest'
import { newestOf } from './laterLoading'

function deferred<T>() {
  let resolve: (v: T) => void = () => undefined
  let reject: (e: unknown) => void = () => undefined
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('a loader that keeps the newest answer', () => {
  it('drops an older answer that lands after a newer one', async () => {
    const shown: string[] = []
    const answers = [deferred<string>(), deferred<string>()]
    let call = 0
    const load = newestOf(() => answers[call++]!.promise, list => { shown.push(list) })
    const older = load()
    const newer = load()
    answers[1]!.resolve('new')
    await newer
    answers[0]!.resolve('old')
    await older
    expect(shown).toEqual(['new'])
  })

  it('applies an older answer at once, without waiting for a newer request that fails or never answers', async () => {
    const shown: string[] = []
    const answers = [deferred<string>(), deferred<string>()]
    let call = 0
    const load = newestOf(() => answers[call++]!.promise, list => { shown.push(list) })
    const older = load()
    const newer = load()
    answers[0]!.resolve('old')
    await older
    expect(shown).toEqual(['old'])
    answers[1]!.reject(new Error('502'))
    await expect(newer).rejects.toThrow('502')
    expect(shown).toEqual(['old'])
  })

  it('never applies an answer to a request made for another account', async () => {
    const shown: string[] = []
    let account = 0
    const answer = deferred<string>()
    const load = newestOf(() => answer.promise, list => { shown.push(list) }, () => account)
    const pending = load()
    account++
    answer.resolve('theirs')
    await pending
    expect(shown).toEqual([])
  })
})
