import Foundation

// MARK: - Tab Event Handlers

extension SessionViewModel {

    @MainActor
    func handleTabClosed(tabId: String) {
        pendingCloseTabIds.remove(tabId)
        tabIdleSince.removeValue(forKey: tabId)
        tabs.removeAll { $0.id == tabId }
        tabIds.remove(tabId)
        // Clean up all conversation/engine state for this tab. The single
        // instance carries messages + liveText + workingMessage +
        // thinkingMessageId, so removing it drops all of them.
        conversationInstances.removeValue(forKey: tabId)
        activeEngineInstance.removeValue(forKey: tabId)
        for key in engineDialogs.keys where key == tabId || key.hasPrefix("\(tabId):") {
            engineDialogs.removeValue(forKey: key)
        }
        for key in enginePinnedPrompt.keys where key == tabId || key.hasPrefix("\(tabId):") {
            enginePinnedPrompt.removeValue(forKey: key)
        }
        for key in activeTools.keys where key.hasPrefix("\(tabId):") || key == tabId {
            activeTools.removeValue(forKey: key)
        }
        engineConversationLoaded = engineConversationLoaded.filter { $0 != tabId && !$0.hasPrefix("\(tabId):") }
        for key in lastSpokenEngineMessageCount.keys where key == tabId || key.hasPrefix("\(tabId):") {
            lastSpokenEngineMessageCount.removeValue(forKey: key)
        }
        conversationLoaded.remove(tabId)
        loadingConversation.remove(tabId)
        // Drafts are local-only state — clean them up when the tab is closed
        // (don't survive tab close; do survive disconnect / restart). One
        // unified bare-tabId draft store covers plain and engine tabs.
        clearTabDraft(tabId)
    }

    @MainActor
    func handleTabStatus(tabId: String, status: TabStatus) {
        if let idx = tabs.firstIndex(where: { $0.id == tabId }) {
            let previousStatus = tabs[idx].status
            tabs[idx].status = status
            if status == .running {
                // A new task started — any previous ExitPlanMode/AskUserQuestion
                // entries are stale (plan was implemented or user moved on).
                tabs[idx].permissionQueue.removeAll {
                    $0.toolName == "ExitPlanMode" || $0.toolName == "AskUserQuestion"
                }
            }
            if status == .idle || status == .completed || status == .failed || status == .dead {
                // Capture preview from liveText before clearing — if tabStatus
                // arrives before taskComplete, this preserves the lastMessage.
                let text = liveText(tabId)
                if !text.isEmpty {
                    tabs[idx].lastMessage = String(text.suffix(64))
                        .replacingOccurrences(of: "\n", with: " ")
                }
                clearLiveText(tabId: tabId)
                // Preserve ExitPlanMode/AskUserQuestion entries -- desktop auto-allows
                // these but iOS needs them for plan card UI and status indicators
                tabs[idx].permissionQueue.removeAll {
                    $0.toolName != "ExitPlanMode" && $0.toolName != "AskUserQuestion"
                }
                // Clear active tools for this tab (both bare tabId and compound keys)
                activeTools.removeValue(forKey: tabId)
                for key in activeTools.keys where key.hasPrefix("\(tabId):") {
                    activeTools.removeValue(forKey: key)
                }

                // Engine tab TTS: speak the last assistant response once per turn.
                // Engine tabs emit tab_status:idle at turn end but do NOT emit
                // task_complete, so handleTaskComplete never runs for them — this
                // is the only voice-readback trigger for engine conversations.
                // tab_status:idle fires exactly once per turn; engine_message_end
                // fires once per sub-message (multiple times with tool calls), so
                // triggering there would cause repeated speech. De-duplicate with
                // lastSpokenEngineMessageCount so repeated idle events (reconnects,
                // upstream re-delivery) don't re-speak the same response.
                if status == .idle {
                    let key = tabId
                    let msgs = engineInstance(tabId: tabId, instanceId: activeEngineInstance[tabId])?.messages ?? []
                    let prevCount = lastSpokenEngineMessageCount[key] ?? 0
                    if let last = msgs.last(where: { $0.role == .assistant }),
                       msgs.count > prevCount,
                       !last.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        lastSpokenEngineMessageCount[key] = msgs.count
                        DiagnosticLog.log("VOICE-TTS: tabIdle speaking \(last.content.count) chars tabId=\(tabId.prefix(8))")
                        voiceService.speak(text: last.content, messageId: last.id, tabId: tabId)
                    }
                }
            }
            // One-shot post-run heal: when a tab transitions out of .running or
            // .connecting into a terminal/idle state, the local transcript may
            // have missed the final deltas (tool_end, last assistant text chunk).
            // Fire a reconcile now that streaming has stopped; the fingerprint
            // and debounce guards in maybeReconcileStaleConversation ensure this
            // only triggers a reload if there is a real divergence, and at most
            // once per reconcileDebounce window.
            if (previousStatus == .running || previousStatus == .connecting)
                && (status == .idle || status == .completed || status == .failed || status == .dead) {
                DiagnosticLog.log("SNAP: post-run heal check tabId=\(tabId.prefix(16)) \(previousStatus.rawValue)->\(status.rawValue)")
                maybeReconcileStaleConversation(tab: tabs[idx])
            }
        }
        // Track idle-since timestamp for sidebar display
        if status == .running || status == .connecting {
            tabIdleSince.removeValue(forKey: tabId)
        } else if tabIdleSince[tabId] == nil {
            tabIdleSince[tabId] = Date()
        }
    }

    @MainActor
    func handleTaskComplete(tabId: String) {
        // Capture liveText before it's cleared — the relay sends assistant
        // output as text_chunk (which populates liveText) rather than
        // engine_text_delta (which populates the instance messages), so
        // liveText is the only reliable source for voice readback in that
        // path.
        let capturedLiveText = liveText(tabId)

        if let idx = tabs.firstIndex(where: { $0.id == tabId }) {
            tabs[idx].status = .completed
            // Preserve ExitPlanMode/AskUserQuestion entries for plan card UI
            tabs[idx].permissionQueue.removeAll {
                $0.toolName != "ExitPlanMode" && $0.toolName != "AskUserQuestion"
            }
            // Capture final preview from accumulated live text before it's cleared
            if !capturedLiveText.isEmpty {
                tabs[idx].lastMessage = String(capturedLiveText.suffix(64))
                    .replacingOccurrences(of: "\n", with: " ")
            }
        }
        clearLiveText(tabId: tabId)
        activeTools.removeValue(forKey: tabId)
        for key in activeTools.keys where key.hasPrefix("\(tabId):") {
            activeTools.removeValue(forKey: key)
        }
        tabIdleSince[tabId] = Date()

        // TTS: try the unified conversation messages → liveText. Both the
        // engine_text_delta path and the message_added path now land in the
        // single instance, so one read covers both; liveText is the
        // text_chunk (relay) fallback.
        let convLoaded = conversationLoaded.contains(tabId)
        let msgs = conversationMessages(tabId)
        DiagnosticLog.log("VOICE-TTS: taskComplete tabId=\(tabId.prefix(8)) convLoaded=\(convLoaded) liveText=\(capturedLiveText.count) msgs=\(msgs.count)")
        let spokenInfo: (text: String, messageId: String?)? = {
            // 1. unified instance messages (engine_text_delta + message_added)
            if let last = msgs.last(where: { $0.role == .assistant }),
               !last.content.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return (last.content, last.id)
            }
            // 2. liveText (text_chunk path — captured before clear) — no ID
            if !capturedLiveText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                return (capturedLiveText, nil)
            }
            return nil
        }()

        if let info = spokenInfo,
           info.text.trimmingCharacters(in: .whitespacesAndNewlines).count > 20 {
            DiagnosticLog.log("VOICE-TTS: speaking \(info.text.count) chars")
            voiceService.speak(text: info.text, messageId: info.messageId, tabId: tabId)
        } else {
            DiagnosticLog.log("VOICE-TTS: not speaking — text=\(spokenInfo == nil ? "nil" : "\(spokenInfo!.text.count) chars")")
        }
    }
}
