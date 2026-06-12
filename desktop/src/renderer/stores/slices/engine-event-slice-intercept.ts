import type { StoreSet } from '../session-store-types'
import { nextMsgId } from '../session-store-helpers'

/**
 * Handler for `engine_intercept` events on engine-view tabs (compound
 * `${tabId}:${instanceId}` key). Extracted from engine-event-slice.ts
 * to keep that file under the 600-line TypeScript cap.
 *
 * Renders an inline banner in the conversation scrollback so the user
 * can see that an intercept fired — which extension triggered it,
 * what it said, and whether the run was redirected. Uses role:
 * 'harness' so the groupMessages pipeline routes it through the
 * InterceptBanner component (via tool-helpers kind: 'intercept').
 * The `interceptLevel` field on the message lets InterceptBanner
 * choose visual weight: 'redirect' gets the amber/urgent style;
 * 'banner' gets a lighter informational style.
 *
 * Content format: markdown so the title renders as bold and the
 * body gets normal prose treatment. The level prefix ("Conversation
 * redirected:") is prepended for 'redirect' level so the user sees
 * the action label even if the title is terse.
 */
export function handleEngineInterceptEvent(
  set: StoreSet,
  key: string,
  event: { interceptLevel: string; interceptTitle: string; interceptMessage: string },
): void {
  const levelPrefix = event.interceptLevel === 'redirect' ? 'Conversation redirected: ' : ''
  const content = `**${levelPrefix}${event.interceptTitle}**\n\n${event.interceptMessage}`
  set((state) => {
    const messages = new Map(state.engineMessages)
    const msgs = [...(messages.get(key) || [])]
    msgs.push({
      id: nextMsgId(),
      role: 'harness' as const,
      content,
      timestamp: Date.now(),
      interceptLevel: event.interceptLevel,
    })
    messages.set(key, msgs)
    return { engineMessages: messages }
  })
}
