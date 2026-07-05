import Foundation

// MARK: - Permission / message event handlers
//
// Extracted from SessionViewModel+EventHandlers.swift to keep that file
// under the 600-line Swift cap. The handlers continue to be members of
// the same `extension SessionViewModel` and are dispatched from
// handleEvent in the original file.

extension SessionViewModel {

    @MainActor
    func handlePermissionRequest(tabId: String, instanceId: String? = nil, questionId: String, toolName: String, toolInput: [String: AnyCodable]?, options: [PermissionOption]) {
        let inputKeys = toolInput?.keys.sorted() ?? []
        let inputSummary = toolInput?.map { "\($0.key): \(type(of: $0.value.value))" }.joined(separator: ", ") ?? "nil"
        let hasEngineExtension = tabs.first(where: { $0.id == tabId })?.hasEngineExtension == true
        DiagnosticLog.log("PERM: handlePermissionRequest: tabId=\(tabId.prefix(8)) instanceId=\(instanceId?.prefix(8) ?? "nil") questionId=\(questionId.prefix(16)) toolName=\(toolName) inputKeys=\(inputKeys) inputTypes=[\(inputSummary)] options=\(options.map(\.label)) isEngine=\(hasEngineExtension)")

        if let idx = tabs.firstIndex(where: { $0.id == tabId }) {
            // Normalize AnyCodable toolInput to Foundation types so the
            // card views can parse with simple `as?` casts. The Codable
            // decoder wraps nested values as [AnyCodable]/[String: AnyCodable],
            // but the card views expect Foundation types (NSArray/NSDictionary)
            // which is what JSONSerialization produces.
            var normalizedInput = toolInput
            if let input = toolInput,
               let data = try? JSONEncoder().encode(input),
               let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                normalizedInput = dict.mapValues { AnyCodable($0) }
                let normalizedSummary = normalizedInput?.map { "\($0.key): \(type(of: $0.value.value))" }.joined(separator: ", ") ?? "nil"
                DiagnosticLog.log("PERM: handlePermissionRequest: normalized toolInput types=[\(normalizedSummary)]")
            } else {
                DiagnosticLog.log("PERM: handlePermissionRequest: normalization failed or skipped, using raw toolInput")
            }
            let request = PermissionRequest(
                questionId: questionId,
                toolName: toolName,
                toolInput: normalizedInput,
                options: options,
                instanceId: instanceId
            )
            DiagnosticLog.log("PERM: handlePermissionRequest: queued request for tabId=\(tabId.prefix(8)) queueSize=\(self.tabs[idx].permissionQueue.count + 1)")
            tabs[idx].permissionQueue.append(request)
        } else {
            DiagnosticLog.log("PERM: handlePermissionRequest: tab \(tabId.prefix(8)) not found, dropping permission request")
        }
    }

    @MainActor
    func handleConversationHistory(tabId: String, newMessages: [Message], hasMore: Bool, cursor: String?) {
        cancelLoadTimer(tabId: tabId)
        conversationLoadFailed.remove(tabId)
        loadingConversation.remove(tabId)
        conversationLoaded.insert(tabId)
        clearLiveText(tabId: tabId)
        conversationHasMore[tabId] = hasMore
        conversationCursor[tabId] = cursor

        // Deduplicate by message ID, keeping last occurrence (most recent version).
        let deduped = deduplicateMessages(newMessages)

        if cursor != nil {
            suppressScrollToBottom = true
            setConversationMessages(tabId: tabId, deduped + conversationMessages(tabId))
        } else {
            // Preserve any pending optimistic user messages that arrived between
            // submit and this history response. An optimistic message is one in
            // the current list whose id is NOT in the incoming history — it was
            // written locally by `submit` and has not yet been confirmed by a
            // desktop echo. If we did a bare replace it would be silently dropped,
            // leaving no user bubble until the echo arrived over the relay
            // round-trip (the MISSING symptom for the case where history lands
            // before the echo). Prepend them so they sit above the history tail;
            // `deduplicateMessages` then collapses any overlap when the echo
            // arrives later and carries the same clientMsgId.
            let incomingIds = Set(deduped.map { $0.id })
            let pending = conversationMessages(tabId).filter {
                !incomingIds.contains($0.id) && $0.role == .user && $0.source == .remote
            }
            let merged = pending.isEmpty ? deduped : deduplicateMessages(pending + deduped)
            setConversationMessages(tabId: tabId, merged)
        }

        // Log the last 3 messages for diagnostics (permission card restoration depends on message content).
        let allMsgs = conversationMessages(tabId)
        let tail = allMsgs.suffix(3)
        let tailSummary = tail.map { "role=\($0.role.rawValue) toolName=\($0.toolName ?? "nil") isTool=\($0.isTool) toolInput=\($0.toolInput?.prefix(60) ?? "nil")" }.joined(separator: " | ")
        DiagnosticLog.log("CONV-HIST: tabId=\(tabId.prefix(8)) total=\(allMsgs.count) hasMore=\(hasMore) cursor=\(cursor?.prefix(8) ?? "nil") tail=[\(tailSummary)]")
    }

    @MainActor
    func handleMessageAdded(tabId: String, message: Message) {
        // Always update tab preview for user/assistant messages (even if conversation isn't loaded)
        if message.role == .user || message.role == .assistant {
            if let idx = tabs.firstIndex(where: { $0.id == tabId }) {
                tabs[idx].lastMessage = String(message.content.prefix(64))
                    .replacingOccurrences(of: "\n", with: " ")
            }
        }
        // Defect A fix: render the live user/assistant echo even on a fresh
        // conversation that has not been loaded yet. The desktop forwards a
        // user echo as a desktop_message_added (role == .user) from its own
        // remote-prompt path (remote/handlers/tabs-prompt.ts); on an iOS-started
        // slash command for a fresh extension-hosted conversation, no history
        // had loaded yet, so the `conversationLoaded` guard dropped that echo
        // and NO user bubble rendered. For user/assistant roles we now mark the
        // conversation loaded and fall through to the insert/reconcile-by-id
        // block below. A later full history reload (handleConversationHistory)
        // replaces the list and reconciles by id, so this early insert never
        // produces a duplicate. Other roles (tool/system) keep the original
        // guard — they are only meaningful against an already-loaded
        // conversation.
        if message.role == .user || message.role == .assistant {
            if !conversationLoaded.contains(tabId) {
                DiagnosticLog.log("MSG-ADD: tabId=\(tabId.prefix(8)) inserting \(message.role.rawValue) echo on not-yet-loaded conversation (marking loaded)")
                conversationLoaded.insert(tabId)
            }
        } else {
            guard conversationLoaded.contains(tabId) else { return }
        }
        mutateConversationMessages(tabId: tabId) { msgs in
            // ID-based reconciliation: if a message with this ID already exists
            // (optimistic insert), replace it with the canonical version from desktop.
            if let existingIdx = msgs.firstIndex(where: { $0.id == message.id }) {
                DiagnosticLog.log("MSG-RECONCILE: tabId=\(tabId.prefix(8)) id=\(message.id.prefix(8)) role=\(message.role.rawValue) replaced-at-idx=\(existingIdx) totalMsgs=\(msgs.count)")
                msgs[existingIdx] = message
            } else {
                DiagnosticLog.log("MSG-APPEND: tabId=\(tabId.prefix(8)) id=\(message.id.prefix(8)) role=\(message.role.rawValue) totalMsgs=\(msgs.count + 1)")
                msgs.append(message)
            }
        }
    }

    @MainActor
    func handleMessageUpdated(tabId: String, messageId: String, content: String?, toolStatus: ToolStatus?, toolInput: String?) {
        guard conversationLoaded.contains(tabId) else { return }
        mutateConversationMessages(tabId: tabId) { msgs in
            guard let idx = msgs.firstIndex(where: { $0.id == messageId }) else { return }
            if let content {
                msgs[idx].content = content
            }
            if let toolStatus {
                // Meta-tools report as errors but should show as completed (not error, not stuck running)
                let toolName = msgs[idx].toolName
                if toolName == "ExitPlanMode" || toolName == "AskUserQuestion" {
                    msgs[idx].toolStatus = .completed
                } else {
                    msgs[idx].toolStatus = toolStatus
                }
            }
            if let toolInput {
                msgs[idx].toolInput = toolInput
            }
        }
    }

    @MainActor
    func handleInputPrefill(tabId: String, text: String, switchTo: Bool, instanceId: String?) {
        // Engine-instance prefill (engine_rewind): seed the engine instance's
        // draft, not the CLI input. The desktop broadcasts a fresh
        // desktop_conversation_history immediately after the rewind restart
        // (broadcastEngineHistory), which the conversationHistory handler
        // applies as a full replace — so the truncated message list refreshes
        // on its own. Here we only place the rewound user message back in the
        // engine input box.
        if let instanceId {
            DiagnosticLog.log("EVENT: inputPrefill -> engine draft tabId=\(tabId.prefix(8)) instance=\(instanceId.prefix(8)) len=\(text.count)")
            setEngineDraft(tabId: tabId, instanceId: instanceId, text)
            if switchTo {
                pendingNavigationTabId = tabId
            }
            return
        }

        // CLI-tab prefill: write the tab-level pending input and (for a
        // rewind, switchTo == false) reload the CLI conversation so the
        // truncated history is reflected.
        pendingInputByTab[tabId] = text
        if switchTo {
            pendingNavigationTabId = tabId
        } else {
            // Rewind: reload the conversation for this tab
            conversationLoaded.remove(tabId)
            setConversationMessages(tabId: tabId, [])
            conversationLoadFailed.remove(tabId)
            loadConversation(tabId: tabId)
        }
    }
}
