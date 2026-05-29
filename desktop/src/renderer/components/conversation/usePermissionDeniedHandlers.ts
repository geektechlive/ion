import { useSessionStore } from '../../stores/sessionStore'
import { usePreferencesStore } from '../../preferences'
import type { TabState, Attachment } from '../../../shared/types'

interface Handlers {
  onDismiss: () => void
  onAnswer: (answer: string) => void
  onApprove: (toolNames: string[]) => void
  onImplement: () => Promise<void>
  onImplementAndUnpin: () => Promise<void>
}

/**
 * Build the four PermissionDeniedCard callbacks for the active tab.
 *
 * onImplement starts a fresh engine session (to exit plan mode cleanly),
 * reads the plan file, and sends "Implement the following plan:\n\n<content>"
 * as the first message of the new session. The planning conversation history
 * is not re-injected — the plan file is the complete artifact of the planning
 * session and is all the model needs to implement it.
 */
export function buildPermissionDeniedHandlers(
  tab: TabState,
  sendMessage: (
    content: string,
    workingDir?: string,
    attachments?: Attachment[],
    appendSystemPrompt?: string,
    implementationPhase?: boolean,
  ) => void,
): Handlers {
  const dismissPermissionDenied = () => {
    useSessionStore.setState((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tab.id ? { ...t, permissionDenied: null } : t
      ),
    }))
  }

  const onDismiss = dismissPermissionDenied

  const onAnswer = (answer: string) => {
    dismissPermissionDenied()
    sendMessage(answer)
  }

  const onApprove = (toolNames: string[]) => {
    window.ion.approveDeniedTools(tab.id, toolNames)
    dismissPermissionDenied()
    sendMessage('The denied tools have been approved. Please retry the operation.')
  }

  const onImplement = async () => {
    dismissPermissionDenied()
    // Switch to auto mode for implementation
    useSessionStore.getState().setPermissionMode('auto', 'plan_approved')

    // Auto-switch to the implementation model if the split feature is enabled
    const { planModelSplitEnabled, implementModeModel } = usePreferencesStore.getState()
    if (planModelSplitEnabled && implementModeModel) {
      useSessionStore.getState().setTabModel(tab.id, implementModeModel)
    }

    // Auto-move tab to in-progress group if designated
    const { inProgressGroupId, tabGroupMode, autoGroupMovement } = usePreferencesStore.getState()
    if (autoGroupMovement && inProgressGroupId && tabGroupMode === 'manual' && tab.groupId !== inProgressGroupId) {
      if (tab.groupPinned) {
        console.log(`[auto-move] suppressed: tab=${tab.id.slice(0, 8)} pinned=true currentGroup=${tab.groupId ?? 'none'} wouldMoveTo=${inProgressGroupId}`)
      } else {
        useSessionStore.getState().moveTabToGroup(tab.id, inProgressGroupId)
      }
    }

    // Extract plan file path: tab state (engine event) > denial toolInput
    let planFilePath: string | null = tab.planFilePath || null
    if (!planFilePath && tab.permissionDenied?.tools) {
      const exitDenial = tab.permissionDenied.tools.find(
        (t) => t.toolName === 'ExitPlanMode' && t.toolInput
      )
      if (exitDenial?.toolInput?.planFilePath) {
        planFilePath = exitDenial.toolInput.planFilePath as string
      }
    }

    // Read plan content
    let planContent: string | null = null
    if (planFilePath) {
      try {
        const result = await window.ion.readPlan(planFilePath)
        planContent = result.content
      } catch (err) {
        console.warn('Failed to read plan file:', err)
      }
    }

    // Start a fresh session to break out of plan mode. This destroys the
    // engine session (clearing planMode, injected plan-mode system prompts,
    // and the restricted tool list) so the implementation run starts clean.
    window.ion.resetTabSession(tab.id)

    // Clear UI messages and reset conversation state so the implementation
    // conversation starts visually fresh.
    useSessionStore.setState((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === tab.id
          ? {
              ...t,
              messages: [],
              historicalSessionIds: [
                ...t.historicalSessionIds,
                ...(t.conversationId && !t.historicalSessionIds.includes(t.conversationId)
                  ? [t.conversationId] : []),
              ],
              conversationId: null,
              lastResult: null,
              currentActivity: '',
              permissionQueue: [],
              permissionDenied: null,
              queuedPrompts: [],
            }
          : t
      ),
    }))

    // Structured signal to the engine that this run is the implement
    // half of a plan-then-implement flow. The engine maps this onto
    // RunOptions.ImplementationPhase, which suppresses EnterPlanMode
    // sentinel-tool injection so the model can't re-propose plan-mode
    // entry against the user's already-approved intent.
    //
    // Replaces the prior mechanism that prepended a "You are implementing
    // a user-approved plan. Do not re-enter plan mode..." preamble to
    // the user prompt and relied on the EnterPlanMode tool docstring
    // telling the model to recognize those phrases. Substring matching
    // was brittle (translation-sensitive, easy to bypass with
    // paraphrasing) and bled UI/harness policy into engine-visible
    // prompt text. The boolean is the mechanical equivalent and lives
    // on the structured wire contract.
    const implementPrompt = planContent
      ? `Implement the following plan:\n\n${planContent}`
      : 'Implement the plan.'

    const planAttachment = planFilePath ? [{
      id: crypto.randomUUID(),
      type: 'plan' as const,
      name: planFilePath.split('/').pop() || 'plan.md',
      path: planFilePath,
    }] : undefined

    sendMessage(implementPrompt, tab.workingDirectory, planAttachment, undefined, true)
  }

  const onImplementAndUnpin = async (): Promise<void> => {
    // Unpin first so the auto-move guard fires when onImplement switches
    // the tab to auto mode — tab will then move to in-progress as expected.
    useSessionStore.getState().toggleTabGroupPin(tab.id)
    console.log(`[tab-pin] implement-and-unpin: tab=${tab.id.slice(0, 8)} — pin cleared, handing off to onImplement`)
    await onImplement()
  }

  return { onDismiss, onAnswer, onApprove, onImplement, onImplementAndUnpin }
}
