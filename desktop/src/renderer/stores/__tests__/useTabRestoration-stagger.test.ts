/**
 * Staggered eager session-start ordering (daemon-model compatibility, 869fa0c6).
 *
 * The engine is a shared launchd daemon. On restore the desktop must not fire
 * all ensureEngineSession calls simultaneously — the burst overwhelms the
 * daemon's dispatch goroutine and event queue (result drops, 30s timeouts).
 * The restore loop therefore: (1) orders the active tab first, (2) starts
 * sessions one at a time, awaiting each before the next.
 *
 * These tests pin both guarantees at the extracted pure/structural seam
 * (orderSessionCandidates + startSessionsSequentially), independent of the
 * heavy useTabRestoration React effect.
 *
 * Revert-test contract:
 *   - Reverting the ordering to plain input order makes the active-first
 *     assertions go red.
 *   - Reverting the sequential loop to Promise.all / fire-and-forget makes the
 *     "at most one in flight" assertion go red (all calls would fire before any
 *     resolves).
 */

import { describe, it, expect, vi } from 'vitest'
import { orderSessionCandidates, startSessionsSequentially } from '../../hooks/useTabRestoration-helpers'

describe('orderSessionCandidates — active tab first', () => {
  it('moves the active candidate to the front, preserving the rest order', () => {
    const candidates = [
      { tabId: 't0', index: 0 },
      { tabId: 't1', index: 1 },
      { tabId: 't2', index: 2 },
    ]
    const ordered = orderSessionCandidates(candidates, 1)
    expect(ordered.map((c) => c.tabId)).toEqual(['t1', 't0', 't2'])
  })

  it('preserves input order when the active index matches no candidate', () => {
    const candidates = [
      { tabId: 't0', index: 0 },
      { tabId: 't2', index: 2 },
    ]
    // activeIdx 1 is not a candidate (e.g. it was an engine/terminal tab).
    const ordered = orderSessionCandidates(candidates, 1)
    expect(ordered.map((c) => c.tabId)).toEqual(['t0', 't2'])
  })

  it('preserves input order when activeIdx is -1 (no active tab)', () => {
    const candidates = [
      { tabId: 't0', index: 0 },
      { tabId: 't1', index: 1 },
    ]
    const ordered = orderSessionCandidates(candidates, -1)
    expect(ordered.map((c) => c.tabId)).toEqual(['t0', 't1'])
  })

  it('returns empty for no candidates', () => {
    expect(orderSessionCandidates([], 0)).toEqual([])
  })
})

describe('startSessionsSequentially — one start in flight at a time', () => {
  it('starts each item only after the previous resolves (no burst)', async () => {
    const items = ['a', 'b', 'c']
    const started: string[] = []
    const resolvers: Array<() => void> = []
    let inFlight = 0
    let maxInFlight = 0

    const start = (item: string): Promise<void> => {
      started.push(item)
      inFlight++
      maxInFlight = Math.max(maxInFlight, inFlight)
      return new Promise<void>((resolve) => {
        resolvers.push(() => {
          inFlight--
          resolve()
        })
      })
    }

    const done = startSessionsSequentially(items, start)

    // Only the first item has started; the loop is awaiting it.
    await Promise.resolve()
    expect(started).toEqual(['a'])

    // Resolve 'a' → 'b' may now start.
    resolvers[0]()
    await Promise.resolve()
    await Promise.resolve()
    expect(started).toEqual(['a', 'b'])

    // Resolve 'b' → 'c' starts.
    resolvers[1]()
    await Promise.resolve()
    await Promise.resolve()
    expect(started).toEqual(['a', 'b', 'c'])

    resolvers[2]()
    await done

    // The core no-burst guarantee: never more than one start in flight.
    expect(maxInFlight).toBe(1)
  })

  it('starts items in the given order', async () => {
    const order: string[] = []
    await startSessionsSequentially(['x', 'y', 'z'], async (item) => {
      order.push(item)
    })
    expect(order).toEqual(['x', 'y', 'z'])
  })

  it('continues past an individual start failure', async () => {
    const order: string[] = []
    const start = vi.fn(async (item: string) => {
      order.push(item)
      if (item === 'b') throw new Error('boom')
    })
    await startSessionsSequentially(['a', 'b', 'c'], start)
    // 'b' threw but 'c' still ran.
    expect(order).toEqual(['a', 'b', 'c'])
  })

  it('is a no-op for an empty list', async () => {
    const start = vi.fn(async () => {})
    await startSessionsSequentially([], start)
    expect(start).not.toHaveBeenCalled()
  })
})
