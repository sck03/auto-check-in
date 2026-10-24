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

test('慢输入队列有界，超过限制后停止并允许核对后恢复', async () => {
  let release = () => {}
  const gate = new Promise<void>((resolve) => { release = resolve })
  let sent = 0
  const queue = inputQueue(async () => { sent++; await gate })
  const pending = Array.from({ length: 66 }, () => queue.push({ type: 'key', key: 'Tab' }).catch((e) => e))
  release()
  await Promise.all(pending)
  await expect(queue.drain()).rejects.toThrow('积压')
  expect(sent).toBeLessThanOrEqual(1)
  await queue.reset()
  await queue.push({ type: 'text', text: 'reviewed' })
  await queue.drain()
  await expect(queue.push({ type: 'text', text: '界'.repeat(2000) })).rejects.toThrow('限制')
})
