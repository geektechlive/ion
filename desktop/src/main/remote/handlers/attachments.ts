import { log as _log } from '../../logger'
import { state } from '../../state'
import type { RemoteCommand } from '../protocol'

function log(msg: string): void {
  _log('main', msg)
}

/**
 * Handle `load_attachments` command from iOS.
 * Extracts all unique attachments from a tab's full message history
 * via executeJavaScript in the renderer, then sends the result back
 * to the requesting device.
 */
export async function handleLoadAttachments(
  cmd: Extract<RemoteCommand, { type: 'load_attachments' }>,
  deviceId: string,
): Promise<void> {
  const tabId = cmd.tabId
  log(`load_attachments: tab=${tabId}`)

  if (!state.mainWindow) {
    log('load_attachments: mainWindow not available')
    state.remoteTransport?.sendToDevice(deviceId, {
      type: 'tab_attachments', tabId, attachments: [],
    })
    return
  }

  const escapedTabId = tabId.replace(/\\/g, '\\\\').replace(/'/g, "\\'")

  try {
    // If the tab's messages haven't been loaded yet (skeleton tab), trigger
    // loadSkeletonMessages before scanning. Skeleton tabs have messages===null
    // after a desktop restart. Without this, source 3 (system planFilePath)
    // and source 4 (tool-call plan detection) both miss plans because they
    // scan tab.messages, which is empty. Engine tabs are exempt (their
    // messages are managed separately via enginePanes).
    await state.mainWindow.webContents.executeJavaScript(`
      (function() {
        try {
          var store = window.__Ion_SESSION_STORE__;
          if (!store) return null;
          var s = store.getState();
          var tab = s.tabs.find(function(t) { return t.id === '${escapedTabId}'; });
          if (!tab || tab.isEngine || tab.messages !== null) return null;
          // Skeleton tab: load messages now and return the Promise so
          // Electron awaits hydration before the attachment scan runs.
          return s.loadSkeletonMessages('${escapedTabId}');
        } catch(e) { return null; }
      })()
    `)

    const attachments = await state.mainWindow.webContents.executeJavaScript(`
      (function() {
        try {
          var store = window.__Ion_SESSION_STORE__;
          if (!store) return [];
          var s = store.getState();
          var tab = s.tabs.find(function(t) { return t.id === '${escapedTabId}'; });
          if (!tab) return [];
          // Source messages from the right place per tab type:
          //  - Conversation tabs: tab.messages
          //  - Engine tabs: active instance messages in enginePanes
          var msgs = tab.messages || [];
          if (tab.isEngine) {
            var pane = s.enginePanes ? s.enginePanes.get('${escapedTabId}') : null;
            if (pane) {
              var inst = pane.activeInstanceId ? pane.instances.find(function(i) { return i.id === pane.activeInstanceId; }) : null;
              if (inst && inst.messages) msgs = inst.messages;
            }
          }
          var seen = {};
          var result = [];
          var re = /^\\[Attached (image|file|plan): ([^\\]]+)\\]$/;
          var planTools = { 'Write': 1, 'Edit': 1, 'NotebookEdit': 1 };
          var planPathRe = /[\\\/]plans[\\\/][^\\\/]+\\.md$/;
          for (var i = 0; i < msgs.length; i++) {
            var msg = msgs[i];
            // 1. Structured attachments on user messages
            if (msg.role === 'user') {
              var ma = msg.attachments;
              if (ma) {
                for (var j = 0; j < ma.length; j++) {
                  var p = ma[j].path;
                  if (p && !seen[p]) {
                    seen[p] = true;
                    result.push({ type: ma[j].type, name: ma[j].name || '', path: p });
                  }
                }
              }
              // 2. Content markers
              var lines = (msg.content || '').split('\\n');
              for (var k = 0; k < lines.length; k++) {
                var m = re.exec(lines[k]);
                if (!m) break;
                if (!seen[m[2]]) {
                  seen[m[2]] = true;
                  var parts = m[2].split('/');
                  result.push({ type: m[1], name: parts[parts.length - 1] || m[2], path: m[2] });
                }
              }
            }
            // 3. System divider messages with planFilePath
            if (msg.role === 'system' && msg.planFilePath && !seen[msg.planFilePath]) {
              seen[msg.planFilePath] = true;
              var sp = msg.planFilePath.split('/');
              result.push({ type: 'plan', name: sp[sp.length - 1] || 'plan.md', path: msg.planFilePath });
            }
            // 4. Tool-call plan detection (Write/Edit on **/plans/*.md)
            if (msg.role === 'tool' && msg.toolName && planTools[msg.toolName]) {
              var ti = msg.toolInput || msg.input || '';
              if (typeof ti === 'string') {
                try { ti = JSON.parse(ti); } catch(e) {}
              }
              var fp = (ti && typeof ti === 'object') ? (ti.file_path || ti.path || ti.filePath || '') : '';
              if (fp && planPathRe.test(fp) && !seen[fp]) {
                seen[fp] = true;
                var tp = fp.split('/');
                result.push({ type: 'plan', name: tp[tp.length - 1] || 'plan.md', path: fp });
              }
            }
          }
          // Plan file: check tab-level first, then engine instance level
          var planPath = tab.planFilePath || null;
          if (!planPath && tab.isEngine && pane) {
            var inst = pane.activeInstanceId ? pane.instances.find(function(i) { return i.id === pane.activeInstanceId; }) : null;
            if (inst && inst.planFilePath) planPath = inst.planFilePath;
          }
          if (planPath && !seen[planPath]) {
            var pp = planPath.split('/');
            result.push({ type: 'plan', name: pp[pp.length - 1] || 'plan.md', path: planPath });
          }
          // Include conversation-scoped resources (briefings) for this tab.
          // These are keyed by conversationId in s.resources and are not
          // attached to messages — they arrive through the resource broker.
          // Encode as type='briefing' with path='resource:<id>' so iOS can
          // look them up in its ResourceStore without a file read roundtrip.
          var convId = tab.conversationId || null;
          if (convId) {
            var resources = s.resources || {};
            Object.keys(resources).forEach(function(kind) {
              var items = resources[kind] || [];
              for (var ri = 0; ri < items.length; ri++) {
                var item = items[ri];
                if (item.conversationId === convId) {
                  var resourcePath = 'resource:' + item.id;
                  if (!seen[resourcePath]) {
                    seen[resourcePath] = true;
                    result.push({
                      type: 'briefing',
                      name: item.title || item.kind || 'Briefing',
                      path: resourcePath,
                    });
                  }
                }
              }
            });
          }
          return result;
        } catch(e) { return []; }
      })()
    `) || []

    log(`load_attachments: tab=${tabId} found=${attachments.length}`)
    state.remoteTransport?.sendToDevice(deviceId, {
      type: 'tab_attachments', tabId, attachments,
    })
  } catch (err) {
    log(`load_attachments error: ${(err as Error).message}`)
    state.remoteTransport?.sendToDevice(deviceId, {
      type: 'tab_attachments', tabId, attachments: [],
    })
  }
}
