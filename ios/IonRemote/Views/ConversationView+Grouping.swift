import SwiftUI

// MARK: - ConversationView message grouping + scroll
//
// Extracted from the merged ConversationView (formerly EngineView) to keep the
// main view file under the Swift 600-line cap after the #256 view merge. Holds
// the GroupedItem model, the groupedMessages grouping algorithm, the chatItems
// adapter, and the conversationScroll subview. Members of ConversationView via
// this extension so `mainContent` calls `conversationScroll` unchanged.

extension ConversationView {

    enum GroupedItem: Identifiable {
        case single(Message)
        case toolGroup([Message])
        case compaction(Message)
        case thinking(Message)
        case agentTurn(tools: [Message], assistantMessages: [Message], isActive: Bool)
        var id: String {
            switch self {
            case .single(let msg): return msg.id
            case .toolGroup(let msgs): return "tg-\(msgs.first?.id ?? "")"
            case .compaction(let msg): return "cp-\(msg.id)"
            case .thinking(let msg): return "th-\(msg.id)"
            case .agentTurn(let tools, let assistants, _):
                let anchor = tools.first?.id ?? assistants.first?.id ?? ""
                return "at-\(anchor)"
            }
        }
    }

    var groupedMessages: [GroupedItem] {
        DiagnosticLog.log("ENGINE-BOOTSTRAP: groupedMessages entry total=\(engineMsgs.count)")
        var result: [GroupedItem] = []
        var toolBuf: [Message] = []
        var bootstrapBuf: [Message] = []
        var totalRunsFlushed = 0
        var totalSuppressed = 0

        // When unified turn view is active, use the shared turn-grouping
        // algorithm then map ConversationItems back into GroupedItems.
        if unifiedTurnView {
            // First, collapse bootstrap messages, then feed into unified grouping.
            var preprocessed: [Message] = []
            for msg in engineMsgs {
                if msg.role == .harness && msg.content.hasPrefix(Self.bootstrapPrefix) {
                    bootstrapBuf.append(msg)
                } else {
                    if !bootstrapBuf.isEmpty {
                        var representative = bootstrapBuf.last!
                        let suppressed = bootstrapBuf.count - 1
                        if suppressed > 0 {
                            representative.bootstrapCollapsedCount = suppressed
                        }
                        preprocessed.append(representative)
                        totalRunsFlushed += 1
                        totalSuppressed += suppressed
                        bootstrapBuf = []
                    }
                    preprocessed.append(msg)
                }
            }
            if !bootstrapBuf.isEmpty {
                var representative = bootstrapBuf.last!
                let suppressed = bootstrapBuf.count - 1
                if suppressed > 0 {
                    representative.bootstrapCollapsedCount = suppressed
                }
                preprocessed.append(representative)
                totalRunsFlushed += 1
                totalSuppressed += suppressed
                bootstrapBuf = []
            }

            let items = groupConversationItems(preprocessed, unifiedTurnView: true)
            for item in items {
                switch item {
                case .user(let m), .assistant(let m), .system(let m):
                    result.append(.single(m))
                case .thinking(let m):
                    result.append(.thinking(m))
                case .toolGroup(let tools):
                    result.append(.toolGroup(tools))
                case .compaction(let m):
                    result.append(.compaction(m))
                case .agentTurn(let tools, let assistants, let isActive):
                    result.append(.agentTurn(tools: tools, assistantMessages: assistants, isActive: isActive))
                }
            }
            DiagnosticLog.log(
                "ENGINE-BOOTSTRAP: groupedMessages done runs=\(totalRunsFlushed) suppressed=\(totalSuppressed) output=\(result.count)"
            )
            return result
        }

        let flushBootstrap = {
            guard !bootstrapBuf.isEmpty else { return }
            var representative = bootstrapBuf.last!
            let suppressed = bootstrapBuf.count - 1
            if suppressed > 0 {
                representative.bootstrapCollapsedCount = suppressed
            }
            DiagnosticLog.log(
                "ENGINE-BOOTSTRAP: flush run count=\(bootstrapBuf.count) kept=\(representative.id) suppressed=\(suppressed)"
            )
            result.append(.single(representative))
            totalRunsFlushed += 1
            totalSuppressed += suppressed
            bootstrapBuf = []
        }

        for msg in engineMsgs {
            if msg.role == .tool {
                flushBootstrap()
                toolBuf.append(msg)
            } else {
                if !toolBuf.isEmpty {
                    result.append(.toolGroup(toolBuf))
                    toolBuf = []
                }
                if msg.role == .harness && msg.content.hasPrefix(Self.bootstrapPrefix) {
                    DiagnosticLog.log("ENGINE-BOOTSTRAP: enqueue id=\(msg.id) buf=\(bootstrapBuf.count + 1)")
                    bootstrapBuf.append(msg)
                } else if msg.content.hasPrefix("[Compaction]") {
                    flushBootstrap()
                    result.append(.compaction(msg))
                } else if msg.role == .thinking {
                    // Extended-thinking reasoning block (issue #158) —
                    // standalone collapsed row in turn order.
                    flushBootstrap()
                    result.append(.thinking(msg))
                } else {
                    flushBootstrap()
                    result.append(.single(msg))
                }
            }
        }
        flushBootstrap()
        if !toolBuf.isEmpty {
            result.append(.toolGroup(toolBuf))
        }
        DiagnosticLog.log(
            "ENGINE-BOOTSTRAP: groupedMessages done runs=\(totalRunsFlushed) suppressed=\(totalSuppressed) output=\(result.count)"
        )
        return result
    }

    var chatItems: [ChatItem<GroupedItem>] {
        groupedMessages.map { ChatItem(id: $0.id, payload: $0) }
    }

    var conversationScroll: some View {
        ZStack(alignment: .bottom) {
            ChatCollectionView(
                items: chatItems,
                isNearBottom: $isNearBottom,
                forceScrollCounter: forceScrollCounter,
                spacing: 8,
                horizontalInset: 12
            ) { item in
                Group {
                    switch item {
                    case .single(let msg):
                        // Rewind is offered on user messages only, and only
                        // while the instance is idle — mirrors the desktop
                        // MessageActions gate (variant === 'user' && isIdle).
                        // Fork is intentionally not offered for engine
                        // instances (desktop doesn't either). The command is
                        // per-instance: it targets the active engine instance.
                        if msg.role == .user && !isRunning {
                            EngineMessageRow(message: msg, onRewind: { messageId in
                                viewModel.engineRewindInstance(
                                    tabId: tabId,
                                    instanceId: activeInstanceId,
                                    messageId: messageId
                                )
                            })
                        } else {
                            // Non-user rows include plan-lifecycle dividers;
                            // pass onTapPlan so a "Plan created"/"Plan updated"
                            // divider with a planFilePath opens the plan
                            // preview when its slug link is tapped.
                            EngineMessageRow(message: msg, onTapPlan: { path in
                                selectedPlanPath = IdentifiablePath(path: path)
                            })
                        }
                    case .toolGroup(let tools):
                        EngineToolGroupRow(tools: tools)
                    case .compaction(let msg):
                        CompactionRowView(message: msg)
                    case .thinking(let msg):
                        ThinkingRowView(message: msg)
                    case .agentTurn(let tools, let assistants, let isActive):
                        AgentTurnRow(tools: tools, assistantMessages: assistants, isActive: isActive)
                    }
                }
            }

            if !isNearBottom {
                Button {
                    isNearBottom = true
                    forceScrollCounter += 1
                } label: {
                    Image(systemName: "chevron.down")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(.secondary)
                        .frame(width: 40, height: 40)
                        .background(.regularMaterial)
                        .clipShape(Circle())
                        .shadow(color: .black.opacity(0.15), radius: 4, y: 2)
                }
                .padding(.bottom, 12)
                .transition(.opacity.combined(with: .scale))
            }
        }
        .animation(IonTheme.snappySpring, value: isNearBottom)
    }

}
