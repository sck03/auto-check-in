export type BrowserInput = Record<string, unknown> & { type: string; text?: string }

// Merge only adjacent, unsent text. Clicks and special keys remain ordering
// boundaries, so typing into two fields cannot be combined across a Tab key.
export function inputQueue(send: (event: BrowserInput) => Promise<void>) {
  let chain = Promise.resolve()
  let failure: Error | undefined
  let tail: { event: BrowserInput; bytes: number; promise: Promise<void> } | undefined
  const encoder = new TextEncoder()
  return {
    push(event: BrowserInput): Promise<void> {
      if (failure) return Promise.reject(failure)
      const bytes = event.type === 'text' ? encoder.encode(event.text).length : 0
      if (tail?.event.type === 'text' && event.type === 'text' && tail.bytes + bytes <= 4096) {
        tail.event.text = (tail.event.text || '') + (event.text || '')
        tail.bytes += bytes
        return tail.promise
      }
      const entry = { event: { ...event }, bytes, promise: Promise.resolve() }
      entry.promise = chain.then(async () => {
        if (tail === entry) tail = undefined
        if (failure) throw failure
        try {
          await send(entry.event)
        } catch (e) {
          failure = e instanceof Error ? e : new Error('输入未送达，请核对网页后继续')
          throw failure
        }
      })
      chain = entry.promise.catch(() => undefined)
      tail = entry
      return entry.promise
    },
    async drain() {
      await chain
      if (failure) throw failure
    },
    // Never replay an uncertain keystroke. The user reviews the page first.
    async reset() {
      await chain
      failure = undefined
    },
  }
}
