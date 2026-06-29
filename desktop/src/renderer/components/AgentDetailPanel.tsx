import React, { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useColors } from '../theme'
import { useSessionStore } from '../stores/sessionStore'
import { FloatingPanel } from './FloatingPanel'
import { AgentExpandedView } from './AgentExpandedView'
import { meta } from './agent-panel-helpers'
import type { DispatchInfo } from './agent-panel-helpers'
import type { AgentStateUpdate } from '../../shared/types'
import type { Message } from '../../shared/types'

interface AgentDetailPanelProps {
  agent: AgentStateUpdate
  loadedMessages: Message[] | undefined
  loading: boolean
  dispatches: DispatchInfo[]
  selectedDispatch: number
  onSelectDispatch: (idx: number) => void
  onClose: () => void
}

export function AgentDetailPanel({
  agent,
  loadedMessages,
  loading,
  dispatches,
  selectedDispatch,
  onSelectDispatch,
  onClose,
}: AgentDetailPanelProps) {
  const colors = useColors()
  const title = meta(agent, 'displayName', agent.name)
  const geometry = useSessionStore((s) => s.agentDetailGeometry)
  const setGeometry = useSessionStore((s) => s.setAgentDetailGeometry)
  const handleGeometryChange = useCallback(
    (geo: { x: number; y: number; w: number; h: number }) => setGeometry(geo),
    [setGeometry],
  )

  // headerHost: the DOM node that owns the pinned header trio.
  // A ref-callback drives a state update so React re-renders once the node
  // mounts and the portal target is available. On subsequent renders the
  // ref-callback is stable (same element) so no extra renders occur.
  const [headerHost, setHeaderHost] = useState<HTMLDivElement | null>(null)
  const headerHostCallback = useCallback((node: HTMLDivElement | null) => {
    setHeaderHost(node)
  }, [])

  // Scroll management — mirrors ConversationView.tsx pattern exactly.
  // The panel opens at the bottom (newest content) and sticks there while
  // messages stream in. The user can scroll up to read history; new content
  // no longer yanks them back to the top on each push update.
  const scrollRef = useRef<HTMLDivElement>(null)
  const isNearBottomRef = useRef(true)

  const handleScroll = useCallback(() => {
    if (!scrollRef.current) return
    const el = scrollRef.current
    const threshold = 80
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
    isNearBottomRef.current = nearBottom
  }, [])

  // Auto-tail: scroll to bottom whenever messages change and the user is
  // already near the bottom. isNearBottomRef starts true so the very first
  // populate (and every subsequent append while pinned) scrolls automatically.
  // The dependency array tracks both count and the last message id so a
  // streaming text update to the final message (content change, same count)
  // still triggers the tail when the user is pinned.
  const msgCount = loadedMessages?.length ?? 0
  const lastMsgId = loadedMessages?.[loadedMessages.length - 1]?.id ?? ''
  useEffect(() => {
    if (isNearBottomRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [msgCount, lastMsgId])

  return (
    <FloatingPanel
      title={title}
      onClose={onClose}
      defaultWidth={600}
      defaultHeight={500}
      initialPos={{ x: geometry.x, y: geometry.y }}
      initialSize={{ w: geometry.w, h: geometry.h }}
      onGeometryChange={handleGeometryChange}
    >
      {/* Flex column: pinned header zone above scrolling body zone. */}
      <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* Pinned header host — AgentExpandedView portals its header trio
            (infoBar + pager + taskBubble) here via the headerSlot prop.
            Lives outside scrollRef so the header never scrolls away. */}
        <div ref={headerHostCallback} style={{ flexShrink: 0 }} />

        {/* Scrolling transcript body. */}
        <div
          ref={scrollRef}
          onScroll={handleScroll}
          style={{ flex: 1, overflow: 'auto', minHeight: 0 }}
        >
          <AgentExpandedView
            agent={agent}
            colors={colors}
            loadedMessages={loadedMessages}
            loading={loading}
            isFullscreen={true}
            dispatches={dispatches}
            selectedDispatch={selectedDispatch}
            onSelectDispatch={onSelectDispatch}
            headerSlot={(header) =>
              headerHost ? createPortal(header, headerHost) : null
            }
          />
        </div>
      </div>
    </FloatingPanel>
  )
}
