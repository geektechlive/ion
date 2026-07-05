// @vitest-environment jsdom
/**
 * Regression test for the right-aligned user bubble overflowing past the
 * conversation pane's LEFT edge.
 *
 * Root cause (the bug this pins): `MessageBubble` renders the bubble column as
 * a flex item (`inline-flex … max-w-[85%]`) inside a `flex justify-end` row.
 * A flex item defaults to `min-width: auto`, so a long unbreakable token makes
 * the item's intrinsic min-width exceed the 85% cap; `justify-end` then anchors
 * the right edge and pushes the overflow off the LEFT of the pane. The fix is
 * flex-shrink containment:
 *
 *   1. `min-w-0` on the bubble-column wrapper so `max-w-[85%]` is honored.
 *   2. `min-w-0 overflow-hidden` on the inner `.prose-cloud` div so the markdown
 *      wraps inside the bubble (the `.prose-cloud` CSS already sets
 *      `overflow-wrap: break-word; word-break: break-word`, which only takes
 *      effect once the container is allowed to shrink).
 *
 * This test renders a user message with a long unbreakable token and asserts
 * BOTH containment seams are present. Reverting either class change drops the
 * asserted class and turns this test red — that is the regression guard.
 *
 * The assertions read the produced className strings (the stable layout
 * contract), not computed geometry: jsdom does not lay out flexbox, so a
 * pixel-position assertion would be meaningless here. The class contract is the
 * right seam — it is exactly what the fix changes.
 */

import { describe, it, expect, afterEach } from 'vitest'
import React, { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MessageBubble } from '../MessageBubble'
import type { Message } from '../../../../shared/types'

// React requires this flag set before any act() call so it knows the test
// environment is an act-aware one. Without it React logs a warning on every
// render even though the render itself succeeds.
;(globalThis as unknown as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const LONG_UNBREAKABLE =
  'https://example.com/' + 'a'.repeat(400) + '/path/that/never/wraps?q=' + 'z'.repeat(200)

function userMessage(content: string): Message {
  return { id: 'm1', role: 'user', content, timestamp: 0 }
}

let container: HTMLDivElement | null = null
let root: Root | null = null

function renderBubble(message: Message): HTMLElement {
  container = document.createElement('div')
  document.body.appendChild(container)
  root = createRoot(container)
  act(() => {
    root!.render(React.createElement(MessageBubble, { message, skipMotion: true }))
  })
  return container
}

afterEach(() => {
  act(() => {
    root?.unmount()
  })
  root = null
  container?.remove()
  container = null
})

describe('MessageBubble — left-edge overflow containment', () => {
  it('caps the bubble column with max-w-[85%] AND min-w-0 so it cannot grow past the cap', () => {
    const el = renderBubble(userMessage(LONG_UNBREAKABLE))

    // The bubble column is the `inline-flex flex-col items-end` wrapper.
    const column = el.querySelector('.inline-flex.flex-col.items-end') as HTMLElement | null
    expect(column).not.toBeNull()

    const cls = column!.className
    // The width cap that bounds the bubble inside the conversation pane.
    expect(cls).toContain('max-w-[85%]')
    // The shrink-enable that makes the cap actually hold for wide content.
    // Without this the flex item's `min-width: auto` overrides the cap and the
    // bubble spills off the left edge — the exact regression.
    expect(cls).toContain('min-w-0')
  })

  it('contains the prose body with min-w-0 + overflow-hidden so markdown wraps inside the bubble', () => {
    const el = renderBubble(userMessage(LONG_UNBREAKABLE))

    const prose = el.querySelector('.prose-cloud.prose-cloud-user') as HTMLElement | null
    expect(prose).not.toBeNull()

    const cls = prose!.className
    expect(cls).toContain('min-w-0')
    expect(cls).toContain('overflow-hidden')
  })
})
