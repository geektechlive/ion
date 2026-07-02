// Plan-mode event handlers extracted from engine-control-plane-events.ts (split
// by event domain to keep every file under the 600-line cap). These are the
// `engine_plan_mode_changed`, `engine_plan_file_written`,
// `engine_plan_mode_auto_exit`, and `engine_plan_proposal` arms of the
// EngineEvent→NormalizedEvent translation switch, lifted out verbatim. No logic
// change. The main file delegates to handlePlanEvent from its switch.
import type { EngineEvent, NormalizedEvent } from '../shared/types'
import { log as _log } from './logger'
import type { EventEmitterContext, TabEntry } from './engine-control-plane-events-types'

const TAG = 'SessionPlane'
function log(msg: string): void { _log(TAG, msg) }

/**
 * Handle the plan-mode event arms. Returns true when the event type was one of
 * these arms, false otherwise. Behavior is identical to the former inline
 * cases.
 */
export function handlePlanEvent(
  ctx: EventEmitterContext,
  tabId: string,
  tab: TabEntry,
  event: EngineEvent,
): boolean {
  switch (event.type) {
    case 'engine_plan_mode_changed':
      log(`plan_mode_changed: tabId=${tabId} enabled=${event.planModeEnabled}`)
      // Only Enabled:true is authoritative — model-initiated EnterPlanMode
      // confirms the session has entered plan mode and the snapshot must
      // reflect that. Enabled:false is intentionally NOT synced here: the
      // engine no longer emits it for ExitPlanMode (model proposal only),
      // and the user-approval gate in the renderer's onImplement handler
      // is the single chokepoint for the mode flip back to 'auto'. If a
      // false event ever arrives (e.g. from a future engine path) we still
      // forward it to the renderer but do not mutate permissionMode here.
      if (event.planModeEnabled) {
        if (tab.permissionMode !== 'plan') {
          tab.permissionMode = 'plan'
          log(`plan_mode_changed: tabId=${tabId} engine flipped to plan, syncing tab.permissionMode`)
        }
      } else {
        log(`plan_mode_changed: tabId=${tabId} enabled=false ignored (mode flip deferred to user-approval chokepoint)`)
      }
      ctx.emit('event', tabId, event as any)
      return true

    case 'engine_plan_file_written':
      // A Write/Edit landed on the canonical plan file. This is the accurate
      // trigger for the "plan created / updated" conversation marker — the
      // file now exists with content, so the marker is correctly positioned
      // and any link resolves. Forward to the renderer reducer, which inserts
      // the divider (event-slice-plan-mode.ts). Distinct from
      // engine_plan_mode_changed, which only flips plan-mode state.
      log(
        `plan_file_written: tabId=${tabId} op=${event.planWriteOperation} planFilePath=${event.planFilePath ?? ''} planSlug=${event.planSlug ?? ''}`,
      )
      ctx.emit('event', tabId, event as any)
      return true

    case 'engine_plan_mode_auto_exit':
      // Engine synthesized an ExitPlanMode at end-of-turn. Emit as a
      // NormalizedEvent so the single reducer (event-slice.ts) can clear
      // the active instance's permissionMode. The parent tab.permissionMode
      // is NOT written here — the sticky-parent invariant requires that only
      // the active instance carries plan mode for extension-hosted tabs.
      log(`plan_mode_auto_exit: tabId=${tabId} stopReason=${event.stopReason}`)
      ctx.emit('event', tabId, {
        type: 'plan_mode_auto_exit',
        stopReason: event.stopReason || '',
        planFilePath: event.planFilePath,
        planSlug: event.planSlug,
        reason: event.reason,
        sessionId: event.sessionId,
        runId: event.runId,
      } as NormalizedEvent)
      return true

    case 'engine_plan_proposal':
      // The model has proposed a plan-mode transition (currently only
      // kind="exit" — the model called ExitPlanMode). This is a workflow
      // event, NOT a state transition: the actual mode change is deferred
      // to the user-approval chokepoint in runHandleImplement (renderer)
      // and handleImplementPlan (iOS path, main process).
      // The desktop forwards the event to the renderer as the authoritative
      // signal that an approval card should render; the permission_denial
      // path on engine_status remains the fallback card-render trigger so
      // existing logic keeps working during the migration. See
      // docs/architecture/adr/003-state-events-vs-workflow-events.md.
      log(
        `plan_proposal: tabId=${tabId} kind=${event.planProposalKind} planFilePath=${event.planFilePath ?? ''} planSlug=${event.planSlug ?? ''}`,
      )
      ctx.emit('event', tabId, event as any)
      return true
  }
  return false
}
