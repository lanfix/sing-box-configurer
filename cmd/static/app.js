let pendingCount = 0;
let urlPendingCount = 0;
let autoRefreshInterval = null;

function showMessage(text, type) {
    const msg = document.getElementById('message');
    msg.textContent = text;
    msg.className = 'message ' + type + ' show';
    setTimeout(() => {
        msg.classList.remove('show');
    }, 5000);
}

function switchTab(tabName) {
    // Hide all tabs
    document.querySelectorAll('.tab-content').forEach(tab => {
        tab.classList.remove('active');
    });
    document.querySelectorAll('.tab-btn').forEach(btn => {
        btn.classList.remove('active');
    });
    
    // Show selected tab
    document.getElementById(tabName + '-tab').classList.add('active');
    event.target.classList.add('active');
    
    // Save active tab to localStorage
    localStorage.setItem('activeTab', tabName);
    
    // Clear auto-refresh when switching tabs
    if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
    }
    
    // Load data for the tab
    if (tabName === 'rules') {
        loadRules();
    } else if (tabName === 'url-sources') {
        loadURLSources();
        // Start auto-refresh for URL sources (every 5 seconds)
        autoRefreshInterval = setInterval(loadURLSources, 5000);
    } else if (tabName === 'config') {
        loadConfig();
    } else if (tabName === 'control') {
        // No auto-refresh needed for control tab
    }
}

function restoreActiveTab() {
    const savedTab = localStorage.getItem('activeTab') || 'rules';
    const tabBtn = document.querySelector(`.tab-btn[onclick*="${savedTab}"]`);
    if (tabBtn) {
        // Manually trigger tab switch
        document.querySelectorAll('.tab-content').forEach(tab => {
            tab.classList.remove('active');
        });
        document.querySelectorAll('.tab-btn').forEach(btn => {
            btn.classList.remove('active');
        });
        
        document.getElementById(savedTab + '-tab').classList.add('active');
        tabBtn.classList.add('active');
        
        // Load appropriate data
        if (savedTab === 'rules') {
            loadRules();
        } else if (savedTab === 'url-sources') {
            loadURLSources();
            // Start auto-refresh
            autoRefreshInterval = setInterval(loadURLSources, 5000);
        } else if (savedTab === 'config') {
            loadConfig();
        } else if (savedTab === 'control') {
            // No auto-refresh needed for control tab
        }
    }
}

function updateApplyButton() {
    const btn = document.getElementById('applyBtn');
    const oldBadge = btn.querySelector('.pending-badge');
    if (oldBadge) {
        oldBadge.remove();
    }
    
    if (pendingCount > 0) {
        btn.disabled = false;
        const badge = document.createElement('span');
        badge.className = 'pending-badge';
        badge.textContent = pendingCount;
        btn.appendChild(badge);
    } else {
        btn.disabled = true;
    }
}

function updateApplyURLButton() {
    const btn = document.getElementById('applyUrlBtn');
    if (!btn) return;
    
    const oldBadge = btn.querySelector('.pending-badge');
    if (oldBadge) {
        oldBadge.remove();
    }
    
    if (urlPendingCount > 0) {
        btn.disabled = false;
        const badge = document.createElement('span');
        badge.className = 'pending-badge';
        badge.textContent = urlPendingCount;
        btn.appendChild(badge);
    } else {
        btn.disabled = true;
    }
}

async function loadRules() {
    try {
        const response = await fetch('/api/rules');
        const data = await response.json();
        pendingCount = data.pending_count || 0;
        displayRules(data.rules || []);
        updateApplyButton();
    } catch (error) {
        showMessage('Ошибка загрузки правил: ' + error.message, 'error');
    }
}

function displayRules(rules) {
    const tbody = document.getElementById('rulesBody');
    
    if (rules.length === 0) {
        tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Нет правил. Добавьте первое правило выше.</td></tr>';
        return;
    }
    
    tbody.innerHTML = rules.map(rule => `
        <tr class="${rule.deleted ? 'deleted' : (rule.applied ? '' : 'pending')}">
            <td>
                <span class="status-badge ${rule.deleted ? 'status-deleted' : (rule.applied ? 'status-applied' : 'status-pending')}">
                    ${rule.deleted ? 'К удалению' : (rule.applied ? 'Применено' : 'Ожидает')}
                </span>
            </td>
            <td><span class="badge badge-${rule.type}">${getTypeLabel(rule.type)}</span></td>
            <td><code>${rule.value}</code></td>
            <td class="description-cell">${rule.description || ''}</td>
            <td style="text-align: center;">
                <button class="btn btn-danger" onclick="deleteRule('${rule.id}')" ${rule.deleted ? 'disabled' : ''}>
                    ${rule.deleted ? 'Удалено' : 'Удалить'}
                </button>
            </td>
        </tr>
    `).join('');
}

function getTypeLabel(type) {
    const labels = {
        'domain': 'Домен',
        'domain_suffix': 'Суффикс',
        'ip': 'IP',
        'cidr': 'CIDR'
    };
    return labels[type] || type;
}

async function addRule(event) {
    event.preventDefault();
    
    const type = document.getElementById('ruleType').value;
    const value = document.getElementById('ruleValue').value;
    const description = document.getElementById('ruleDescription').value;
    
    try {
        const response = await fetch('/api/rules/add', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ type, value, description })
        });
        
        if (!response.ok) {
            throw new Error('Ошибка при добавлении правила');
        }
        
        showMessage('Правило успешно добавлено', 'success');
        document.getElementById('ruleValue').value = '';
        document.getElementById('ruleDescription').value = '';
        await loadRules();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function deleteRule(id) {
    if (!confirm('Удалить это правило?')) {
        return;
    }
    
    try {
        const response = await fetch('/api/rules/delete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ id })
        });
        
        if (!response.ok) {
            throw new Error('Ошибка при удалении правила');
        }
        
        showMessage('Правило успешно удалено', 'success');
        await loadRules();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function applyRules() {
    if (pendingCount === 0) {
        return;
    }
    
    try {
        const response = await fetch('/api/apply', {
            method: 'POST'
        });
        
        if (!response.ok) {
            const data = await response.json().catch(() => ({}));
            throw new Error(data.error || 'Ошибка при применении правил');
        }
        
        showMessage('Правила успешно применены', 'success');
        await loadRules();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

// URL Sources functions

async function loadURLSources() {
    try {
        const response = await fetch('/api/url-sources');
        const data = await response.json();
        displayURLSources(data.url_sources || []);
        
        // Count pending
        urlPendingCount = (data.url_sources || []).filter(s => !s.applied || s.deleted).length;
        updateApplyURLButton();
    } catch (error) {
        showMessage('Ошибка загрузки URL источников: ' + error.message, 'error');
    }
}

function displayURLSources(sources) {
    const tbody = document.getElementById('urlSourcesBody');
    
    if (sources.length === 0) {
        tbody.innerHTML = '<tr><td colspan="7" class="empty-state">Нет URL источников. Добавьте первый источник выше.</td></tr>';
        return;
    }
    
    tbody.innerHTML = sources.map(source => {
        const lastUpdate = source.last_update ? new Date(source.last_update).toLocaleString('ru-RU') : 'Никогда';
        const statusClass = source.last_status === 'success' ? 'status-success' : (source.last_status === 'error' ? 'status-error' : 'status-pending');
        const statusIcon = source.last_status === 'success' ? '🟢' : (source.last_status === 'error' ? '🔴' : '🟡');
        const statusText = source.last_status === 'success' ? 'Успех' : (source.last_status === 'error' ? 'Ошибка' : 'Ожидает');
        
        return `
            <tr class="${source.deleted ? 'deleted' : (source.applied ? '' : 'pending')}">
                <td>
                    <span class="status-badge ${source.deleted ? 'status-deleted' : (source.applied ? 'status-applied' : 'status-pending')}">
                        ${source.deleted ? 'К удалению' : (source.applied ? 'Применено' : 'Ожидает')}
                    </span>
                    <br>
                    <div class="status-indicator ${statusClass}" style="margin-top: 8px;">
                        <span class="status-icon">${statusIcon}</span>
                        <span class="status-text">${statusText}</span>
                    </div>
                    ${source.last_error ? `<div style="color: #C62828; font-size: 11px; margin-top: 4px;">${source.last_error}</div>` : ''}
                </td>
                <td class="url-cell" title="${source.url}">${source.url}</td>
                <td class="description-cell">${source.description || ''}</td>
                <td>${source.interval} мин</td>
                <td style="font-size: 12px;">${lastUpdate}</td>
                <td>${source.items_count || 0}</td>
                <td style="text-align: center;">
                    <button class="btn btn-info" onclick="viewURLSourceRules('${source.id}')" ${!source.applied ? 'disabled' : ''}>
                        Посмотреть
                    </button>
                    <button class="btn btn-danger" onclick="deleteURLSource('${source.id}')" ${source.deleted ? 'disabled' : ''}>
                        ${source.deleted ? 'Удалено' : 'Удалить'}
                    </button>
                </td>
            </tr>
        `;
    }).join('');
}

async function validateURL() {
    const url = document.getElementById('urlSourceURL').value;
    
    if (!url) {
        showMessage('Введите URL', 'error');
        return;
    }
    
    try {
        const response = await fetch('/api/url-sources/validate', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ url })
        });
        
        const data = await response.json();
        
        if (data.valid) {
            showMessage(`URL валидный! Найдено ${data.count} правил`, 'success');
        } else {
            showMessage(`Ошибка валидации: ${data.error}`, 'error');
        }
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function addURLSource(event) {
    event.preventDefault();
    
    const url = document.getElementById('urlSourceURL').value;
    const interval = parseInt(document.getElementById('urlSourceInterval').value);
    const description = document.getElementById('urlSourceDescription').value;
    
    try {
        const response = await fetch('/api/url-sources/add', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ url, interval, description })
        });
        
        if (!response.ok) {
            throw new Error('Ошибка при добавлении URL источника');
        }
        
        showMessage('URL источник успешно добавлен', 'success');
        document.getElementById('urlSourceURL').value = '';
        document.getElementById('urlSourceInterval').value = '60';
        document.getElementById('urlSourceDescription').value = '';
        await loadURLSources();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function deleteURLSource(id) {
    if (!confirm('Удалить этот URL источник?')) {
        return;
    }
    
    try {
        const response = await fetch('/api/url-sources/delete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ id })
        });
        
        if (!response.ok) {
            throw new Error('Ошибка при удалении URL источника');
        }
        
        showMessage('URL источник успешно удален', 'success');
        await loadURLSources();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function applyURLSources() {
    if (urlPendingCount === 0) {
        return;
    }
    
    try {
        const response = await fetch('/api/url-sources/apply', {
            method: 'POST'
        });
        
        if (!response.ok) {
            const data = await response.json().catch(() => ({}));
            throw new Error(data.error || 'Ошибка при применении URL источников');
        }
        
        showMessage('URL источники успешно применены', 'success');
        await loadURLSources();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function viewURLSourceRules(id) {
    try {
        const response = await fetch(`/api/url-sources/rules?id=${id}`);
        const data = await response.json();

        const cidrList = data.cidrList || [];
        const domains = data.domains || [];
        const domainSuffixes = data.domainSuffixes || []

        if (cidrList.length === 0 && domains.length === 0 && domainSuffixes.length === 0) {
            showMessage('Нет загруженных правил для этого источника', 'error');
            return;
        }

        const cidrListText = cidrList.join('\n');
        const domainsText = domains.join('\n');
        const domainSuffixesText = domainSuffixes.join('\n');

        const modal = `
            <div style="position: fixed; top: 0; left: 0; width: 100%; height: 100%; background: rgba(0,0,0,0.5); z-index: 1000; display: flex; align-items: center; justify-content: center;" onclick="this.remove()">
                <div style="background: white; padding: 20px; border-radius: 8px; max-width: 600px; max-height: 80vh; overflow: auto;" onclick="event.stopPropagation()">
                    <h3>Загруженные правила</h3>
                    <div style="display: ${cidrList.length===0?"none":"block"}">
                        <h5 style="margin-top: 10px;">Префиксы CIDR (${cidrList.length})</h5>
                        <pre style="margin-top: 5px; background: #f5f5f5; padding: 15px; border-radius: 4px; overflow: auto; max-height: 400px;">${cidrListText}</pre>
                    </div>
                    <div style="display: ${domains.length===0?"none":"block"}">
                        <h5 style="margin-top: 10px;">Конкретные домены (${domains.length})</h5>
                        <pre style="margin-top: 5px; background: #f5f5f5; padding: 15px; border-radius: 4px; overflow: auto; max-height: 400px;">${domainsText}</pre>
                    </div>
                    <div style="display: ${domainSuffixes.length===0?"none":"block"}">
                        <h5 style="margin-top: 10px;">Суффиксы доменов (${domainSuffixes.length})</h5>
                        <pre style="margin-top: 5px; background: #f5f5f5; padding: 15px; border-radius: 4px; overflow: auto; max-height: 400px;">${domainSuffixesText}</pre>
                    </div>
                    <button style="margin-top: 10px;" class="btn btn-primary" onclick="this.closest('div[style*=fixed]').remove()">Закрыть</button>
                </div>
            </div>
        `;
        document.body.insertAdjacentHTML('beforeend', modal);
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

// Control functions

async function reloadSingBox() {
    const btn = document.getElementById('reloadBtn');
    const statusDiv = document.getElementById('reloadStatus');
    
    btn.disabled = true;
    btn.textContent = '⏳ Перезагрузка...';
    statusDiv.textContent = '';
    statusDiv.className = 'reload-status';
    
    try {
        const response = await fetch('/api/control/reload', {
            method: 'POST'
        });
        
        const data = await response.json();
        
        if (response.ok && data.success) {
            showMessage('Sing-Box успешно перезагружен', 'success');
            statusDiv.textContent = '✅ ' + data.message;
            statusDiv.className = 'reload-status success';
        } else {
            throw new Error(data.error || 'Ошибка перезагрузки');
        }
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
        statusDiv.textContent = '❌ Ошибка: ' + error.message;
        statusDiv.className = 'reload-status error';
    } finally {
        btn.disabled = false;
        btn.textContent = '🔄 Перезагрузить Sing-Box';
    }
}

// Load rules on page load
restoreActiveTab();

// Config Editor functionality
let originalConfig = '';
let currentConfig = '';
let hasPendingChanges = false;

// Undo/Redo history
let undoStack = [];
let redoStack = [];
let isUndoRedoAction = false;

function saveToUndoStack(editor) {
    if (isUndoRedoAction) return;
    
    undoStack.push({
        value: editor.value,
        selectionStart: editor.selectionStart,
        selectionEnd: editor.selectionEnd
    });
    
    // Limit stack size to 100
    if (undoStack.length > 100) {
        undoStack.shift();
    }
    
    // Clear redo stack on new action
    redoStack = [];
}

function undo(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'z' && !e.shiftKey) {
        e.preventDefault();
        
        const editor = document.getElementById('configEditor');
        if (!editor || undoStack.length === 0) return;
        
        // Save current state to redo stack
        redoStack.push({
            value: editor.value,
            selectionStart: editor.selectionStart,
            selectionEnd: editor.selectionEnd
        });
        
        // Restore previous state
        const state = undoStack.pop();
        isUndoRedoAction = true;
        editor.value = state.value;
        editor.selectionStart = state.selectionStart;
        editor.selectionEnd = state.selectionEnd;
        isUndoRedoAction = false;
        
        // Update display
        updateLineNumbers();
        resizeTextarea();
        updateConfigButtons();
        scrollToCursor(editor);
    }
}

function redo(e) {
    if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || (e.key === 'z' && e.shiftKey))) {
        e.preventDefault();
        
        const editor = document.getElementById('configEditor');
        if (!editor || redoStack.length === 0) return;
        
        // Save current state to undo stack
        undoStack.push({
            value: editor.value,
            selectionStart: editor.selectionStart,
            selectionEnd: editor.selectionEnd
        });
        
        // Restore next state
        const state = redoStack.pop();
        isUndoRedoAction = true;
        editor.value = state.value;
        editor.selectionStart = state.selectionStart;
        editor.selectionEnd = state.selectionEnd;
        isUndoRedoAction = false;
        
        // Update display
        updateLineNumbers();
        resizeTextarea();
        updateConfigButtons();
        scrollToCursor(editor);
    }
}

function highlightJSON(text) {
    // Handle multi-line comments first
    const parts = [];
    let currentIndex = 0;
    let inMultiLineComment = false;
    
    // Find all /* */ blocks
    const multiLineCommentRegex = /\/\*[\s\S]*?\*\//g;
    const multiLineMatches = [];
    let match;
    
    while ((match = multiLineCommentRegex.exec(text)) !== null) {
        multiLineMatches.push({
            start: match.index,
            end: match.index + match[0].length,
            text: match[0]
        });
    }
    
    // Split text by lines and process
    const lines = text.split('\n');
    const result = [];
    let charIndex = 0;
    
    for (let lineNum = 0; lineNum < lines.length; lineNum++) {
        let line = lines[lineNum];
        const lineStart = charIndex;
        const lineEnd = charIndex + line.length;
        
        // Escape HTML
        line = line
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;');
        
        // Check if this line is inside a multi-line comment
        let insideMultiLine = false;
        let multiLineStart = -1;
        let multiLineEnd = -1;
        
        for (const mlc of multiLineMatches) {
            if (lineStart >= mlc.start && lineEnd <= mlc.end) {
                insideMultiLine = true;
                multiLineStart = Math.max(0, mlc.start - lineStart);
                multiLineEnd = Math.min(line.length, mlc.end - lineStart);
                break;
            } else if (lineStart < mlc.start && lineEnd > mlc.start && lineEnd <= mlc.end) {
                // Comment starts in this line
                multiLineStart = mlc.start - lineStart;
                multiLineEnd = line.length;
                insideMultiLine = true;
                break;
            } else if (lineStart >= mlc.start && lineStart < mlc.end && lineEnd > mlc.end) {
                // Comment ends in this line
                multiLineStart = 0;
                multiLineEnd = mlc.end - lineStart;
                insideMultiLine = true;
                break;
            }
        }
        
        if (insideMultiLine) {
            // Split line into parts: before comment, comment, after comment
            const before = line.substring(0, multiLineStart);
            const comment = line.substring(multiLineStart, multiLineEnd);
            const after = line.substring(multiLineEnd);
            
            let resultLine = '';
            if (before) {
                resultLine += highlightJSONLine(before);
            }
            if (comment) {
                resultLine += '<span class="json-comment">' + comment + '</span>';
            }
            if (after) {
                resultLine += highlightJSONLine(after);
            }
            
            result.push(resultLine);
            charIndex = lineEnd + 1;
            continue;
        }
        
        // Check if line is a single-line comment (starts with //, or #)
        const trimmed = line.trim();
        if (trimmed.startsWith('//') || trimmed.startsWith('#')) {
            result.push('<span class="json-comment">' + line + '</span>');
            charIndex = lineEnd + 1;
            continue;
        }
        
        // Check if line contains inline comment
        const commentIndex = line.indexOf('//');
        const hashIndex = line.indexOf('#');
        let splitIndex = -1;
        
        if (commentIndex !== -1 && hashIndex !== -1) {
            splitIndex = Math.min(commentIndex, hashIndex);
        } else if (commentIndex !== -1) {
            splitIndex = commentIndex;
        } else if (hashIndex !== -1) {
            splitIndex = hashIndex;
        }
        
        if (splitIndex !== -1) {
            // Check if comment is not inside a string
            const beforeComment = line.substring(0, splitIndex);
            const quotes = (beforeComment.match(/"/g) || []).length;
            const escapedQuotes = (beforeComment.match(/\\"/g) || []).length;
            const actualQuotes = quotes - escapedQuotes;
            
            // If odd number of quotes, comment is inside string
            if (actualQuotes % 2 === 0) {
                let codePart = highlightJSONLine(beforeComment);
                let commentPart = '<span class="json-comment">' + line.substring(splitIndex) + '</span>';
                result.push(codePart + commentPart);
                charIndex = lineEnd + 1;
                continue;
            }
        }
        
        // Regular JSON highlighting
        result.push(highlightJSONLine(line));
        charIndex = lineEnd + 1;
    }
    
    return result.join('\n');
}

function highlightJSONLine(line) {
    return line
        .replace(/("(?:\\.|[^"\\])*")\s*:/g, '<span class="json-key">$1</span>:')
        .replace(/:(\s*)("(?:\\.|[^"\\])*")/g, ':$1<span class="json-string">$2</span>')
        .replace(/\b(-?\d+\.?\d*)\b/g, '<span class="json-number">$1</span>')
        .replace(/\b(true|false)\b/g, '<span class="json-boolean">$1</span>')
        .replace(/\bnull\b/g, '<span class="json-null">null</span>')
        .replace(/([{}[\],:])/g, '<span class="json-punctuation">$1</span>');
}

function scrollToCursor(editor) {
    const wrapper = document.getElementById('editorWrapper');
    if (!wrapper) return;
    
    // Get cursor position
    const cursorPos = editor.selectionStart;
    const textBeforeCursor = editor.value.substring(0, cursorPos);
    const lines = textBeforeCursor.split('\n');
    const currentLine = lines.length - 1;
    const currentColumn = lines[lines.length - 1].length;
    
    // Calculate pixel position
    const lineHeight = 1.6 * 14; // line-height * font-size
    const charWidth = 8.4; // approximate character width in Consolas 14px
    
    const cursorY = currentLine * lineHeight + 12; // +12 for padding
    const cursorX = currentColumn * charWidth + 12 + 58; // +12 padding, +58 line numbers width
    
    // Get wrapper dimensions
    const wrapperRect = wrapper.getBoundingClientRect();
    const scrollTop = wrapper.scrollTop;
    const scrollLeft = wrapper.scrollLeft;
    const viewportHeight = wrapper.clientHeight;
    const viewportWidth = wrapper.clientWidth;
    
    // Scroll vertically if cursor is outside viewport
    if (cursorY < scrollTop + 20) {
        wrapper.scrollTop = Math.max(0, cursorY - 20);
    } else if (cursorY > scrollTop + viewportHeight - 40) {
        wrapper.scrollTop = cursorY - viewportHeight + 40;
    }
    
    // Scroll horizontally if cursor is outside viewport
    if (cursorX < scrollLeft + 70) {
        wrapper.scrollLeft = Math.max(0, cursorX - 70);
    } else if (cursorX > scrollLeft + viewportWidth - 20) {
        wrapper.scrollLeft = cursorX - viewportWidth + 20;
    }
}

function updateSyntaxHighlight() {
    const editor = document.getElementById('configEditor');
    const highlight = document.getElementById('syntaxHighlight');
    
    if (!editor || !highlight) return;
    
    const highlighted = highlightJSON(editor.value);
    highlight.innerHTML = highlighted;
}

function updateLineNumbers() {
    const editor = document.getElementById('configEditor');
    const lineNumbers = document.getElementById('lineNumbers');
    if (!editor || !lineNumbers) return;
    
    const lines = editor.value.split('\n');
    const numbers = lines.map((_, i) => i + 1).join('\n');
    lineNumbers.textContent = numbers;
}

function resizeTextarea() {
    const editor = document.getElementById('configEditor');
    const highlight = document.getElementById('syntaxHighlight');
    if (!editor || !highlight) return;
    
    // Sync content
    highlight.textContent = editor.value;
    
    // Apply highlighting
    const highlighted = highlightJSON(editor.value);
    highlight.innerHTML = highlighted;
}

function handleTab(e) {
    if (e.key === 'Tab') {
        e.preventDefault();
        
        const editor = e.target;
        
        // Save state before change
        saveToUndoStack(editor);
        
        const start = editor.selectionStart;
        const end = editor.selectionEnd;
        const value = editor.value;
        
        // Check if we have multiline selection
        const selectedText = value.substring(start, end);
        const hasNewline = selectedText.includes('\n');
        
        if (hasNewline || (start !== end && value.substring(start, end).includes('\n'))) {
            // Multi-line indent
            const lines = value.split('\n');
            let currentPos = 0;
            let newStart = start;
            let newEnd = end;
            let startLine = -1;
            let endLine = -1;
            
            // Find which lines are selected
            for (let i = 0; i < lines.length; i++) {
                const lineStart = currentPos;
                const lineEnd = currentPos + lines[i].length;
                
                if (startLine === -1 && start >= lineStart && start <= lineEnd) {
                    startLine = i;
                }
                if (endLine === -1 && end >= lineStart && end <= lineEnd) {
                    endLine = i;
                }
                
                currentPos = lineEnd + 1; // +1 for newline
            }
            
            // Indent selected lines
            const tab = '  ';
            for (let i = startLine; i <= endLine; i++) {
                if (e.shiftKey) {
                    // Unindent
                    if (lines[i].startsWith(tab)) {
                        lines[i] = lines[i].substring(tab.length);
                        if (i === startLine) newStart -= tab.length;
                        newEnd -= tab.length;
                    } else if (lines[i].startsWith(' ')) {
                        lines[i] = lines[i].substring(1);
                        if (i === startLine) newStart -= 1;
                        newEnd -= 1;
                    }
                } else {
                    // Indent
                    lines[i] = tab + lines[i];
                    if (i === startLine) newStart += tab.length;
                    newEnd += tab.length;
                }
            }
            
            editor.value = lines.join('\n');
            editor.selectionStart = Math.max(0, newStart);
            editor.selectionEnd = Math.max(0, newEnd);
        } else {
            // Single position or single-line selection - insert tab
            const tab = '  ';
            editor.value = value.substring(0, start) + tab + value.substring(end);
            editor.selectionStart = editor.selectionEnd = start + tab.length;
        }
        
        // Update display
        updateLineNumbers();
        resizeTextarea();
        updateConfigButtons();
    }
}

function toggleComment(e) {
    if (e.key === '/' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        
        const editor = e.target;
        
        // Save state before change
        saveToUndoStack(editor);
        
        const start = editor.selectionStart;
        const end = editor.selectionEnd;
        const value = editor.value;
        const lines = value.split('\n');
        
        let currentPos = 0;
        let startLine = -1;
        let endLine = -1;
        
        // Find which lines are selected
        for (let i = 0; i < lines.length; i++) {
            const lineStart = currentPos;
            const lineEnd = currentPos + lines[i].length;
            
            if (startLine === -1 && start >= lineStart && start <= lineEnd) {
                startLine = i;
            }
            if (endLine === -1 && end >= lineStart && end <= lineEnd) {
                endLine = i;
            }
            
            currentPos = lineEnd + 1;
        }
        
        // Check if all selected lines are commented
        let allCommented = true;
        for (let i = startLine; i <= endLine; i++) {
            if (!lines[i].trim().startsWith('//')) {
                allCommented = false;
                break;
            }
        }
        
        let offsetStart = 0;
        let offsetEnd = 0;
        
        // Toggle comments
        for (let i = startLine; i <= endLine; i++) {
            if (allCommented) {
                // Uncomment
                const match = lines[i].match(/^(\s*)\/\/\s?/);
                if (match) {
                    lines[i] = lines[i].replace(/^(\s*)\/\/\s?/, '$1');
                    const removed = match[0].length - match[1].length;
                    if (i === startLine) offsetStart -= removed;
                    offsetEnd -= removed;
                }
            } else {
                // Comment
                const match = lines[i].match(/^(\s*)/);
                if (match) {
                    const indent = match[1];
                    lines[i] = indent + '// ' + lines[i].substring(indent.length);
                    if (i === startLine) offsetStart += 3;
                    offsetEnd += 3;
                }
            }
        }
        
        editor.value = lines.join('\n');
        editor.selectionStart = start + offsetStart;
        editor.selectionEnd = end + offsetEnd;
        
        // Update display
        updateLineNumbers();
        resizeTextarea();
        updateConfigButtons();
    }
}

function updateConfigButtons() {
    const saveBtn = document.getElementById('saveConfigBtn');
    const applyBtn = document.getElementById('applyConfigBtn');
    const discardBtn = document.getElementById('discardConfigBtn');
    const statusSpan = document.getElementById('configStatus');
    
    if (!saveBtn || !applyBtn || !discardBtn || !statusSpan) return;
    
    const editor = document.getElementById('configEditor');
    const isModified = editor && editor.value !== originalConfig;
    
    saveBtn.disabled = !isModified;
    discardBtn.disabled = !isModified && !hasPendingChanges;
    applyBtn.disabled = !hasPendingChanges;
    
    if (hasPendingChanges) {
        statusSpan.textContent = '⚠️ Есть несохраненные изменения';
        statusSpan.className = 'config-status saved';
    } else if (isModified) {
        statusSpan.textContent = '✏️ Редактируется';
        statusSpan.className = 'config-status modified';
    } else {
        statusSpan.textContent = '';
        statusSpan.className = 'config-status';
    }
}

async function loadConfig() {
    try {
        const response = await fetch('/api/config/get');
        const data = await response.json();
        
        const editor = document.getElementById('configEditor');
        if (!editor) return;
        
        // Pretty print JSON
        try {
            const parsed = JSON.parse(data.config);
            const formatted = JSON.stringify(parsed, null, 2);
            editor.value = formatted;
            originalConfig = formatted;
            currentConfig = formatted;
        } catch (e) {
            editor.value = data.config;
            originalConfig = data.config;
            currentConfig = data.config;
        }
        
        hasPendingChanges = data.hasPending || false;
        
        updateLineNumbers();
        resizeTextarea();
        updateConfigButtons();
        
        // Initialize undo stack with initial state
        undoStack = [{
            value: editor.value,
            selectionStart: 0,
            selectionEnd: 0
        }];
        redoStack = [];
        
        // Add undo/redo handlers
        editor.addEventListener('keydown', undo);
        editor.addEventListener('keydown', redo);
        
        // Add tab handler
        editor.addEventListener('keydown', handleTab);
        
        // Add comment toggle handler
        editor.addEventListener('keydown', toggleComment);
        
        // Add scrollIntoView on cursor movement
        editor.addEventListener('keyup', (e) => {
            if (['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown'].includes(e.key)) {
                scrollToCursor(editor);
            }
        });
        
        editor.addEventListener('click', () => {
            scrollToCursor(editor);
        });
        
        // Add input listener
        let inputTimeout;
        editor.addEventListener('input', () => {
            // Save to undo stack after a short delay (debounce)
            clearTimeout(inputTimeout);
            inputTimeout = setTimeout(() => {
                saveToUndoStack(editor);
            }, 500);
            
            updateLineNumbers();
            resizeTextarea();
            updateConfigButtons();
        });
        
    } catch (error) {
        showMessage('Ошибка загрузки конфига: ' + error.message, 'error');
    }
}

async function saveTempConfig() {
    const editor = document.getElementById('configEditor');
    if (!editor) return;
    
    const config = editor.value;
    
    // Не валидируем JSON на клиенте - сервер сделает это с поддержкой комментариев
    
    try {
        const response = await fetch('/api/config/save-temp', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ config })
        });
        
        if (!response.ok) {
            const data = await response.json().catch(() => ({}));
            throw new Error(data.error || 'Ошибка при сохранении');
        }
        
        showMessage('Конфиг сохранен временно. Нажмите "Применить" для активации.', 'success');
        originalConfig = config;
        hasPendingChanges = true;
        updateConfigButtons();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function applyConfig() {
    if (!confirm('Применить изменения и перезагрузить Sing-Box?\n\nТекущие соединения могут быть разорваны.')) {
        return;
    }
    
    try {
        const response = await fetch('/api/config/apply', {
            method: 'POST'
        });
        
        if (!response.ok) {
            const data = await response.json().catch(() => ({}));
            throw new Error(data.error || 'Ошибка при применении');
        }
        
        const data = await response.json();
        
        if (data.warning) {
            showMessage('⚠️ ' + data.message + ' Предупреждение: ' + data.warning, 'error');
        } else {
            showMessage('✅ ' + data.message, 'success');
        }
        
        hasPendingChanges = false;
        updateConfigButtons();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function discardConfig() {
    if (!confirm('Отменить все несохраненные изменения?')) {
        return;
    }
    
    try {
        const response = await fetch('/api/config/discard', {
            method: 'POST'
        });
        
        if (!response.ok) {
            throw new Error('Ошибка при отмене изменений');
        }
        
        showMessage('Изменения отменены', 'success');
        
        // Reload config from server
        await loadConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}
