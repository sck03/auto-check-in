import { test, expect } from '@playwright/test'
import { inputQueue } from '../src/inputQueue'

test('输入失败停止后续操作，保存等待失败，核对后可继续且不重放', async () => {
  const sent: string[] = []
  const queue = inputQueue(async (event) => {
    sent.push(String(event.type))
    if (sent.length === 1) throw new Error('fixture send failure')
  })
  const first = queue.push({ type: 'text', text: 'fixture' })
  const later = queue.push({ type: 'key', key: 'Enter' })
  await expect(first).rejects.toThrow('fixture send failure')
  await expect(later).rejects.toThrow('fixture send failure')
  await expect(queue.drain()).rejects.toThrow('fixture send failure')
  expect(sent).toEqual(['text'])
  await queue.reset()
  await queue.push({ type: 'text', text: 'reviewed' })
  await queue.drain()
  expect(sent).toEqual(['text', 'text'])
})
