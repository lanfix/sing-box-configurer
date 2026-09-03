let pendingCount = 0;
let urlPendingCount = 0;
let autoRefreshInterval = null;

function escapeHTML(text) {
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function showMessage(text, type) {
    const msg = document.getElementById('message');
    msg.textContent = text;
    msg.className = 'message ' + type + ' show';
    clearTimeout(showMessage.timer);
    showMessage.timer = setTimeout(() => {
        msg.classList.remove('show');
    }, 5000);
}

// Tabs

function activateTab(tabName) {
    const content = document.getElementById(tabName + '-tab');
    const btn = document.querySelector(`.tab-btn[data-tab="${tabName}"]`);
    if (!content || !btn) return false;

    document.querySelectorAll('.tab-content.active').forEach(tab => {
        tab.classList.remove('active');
    });
    document.querySelectorAll('.tab-btn.active').forEach(b => {
        b.classList.remove('active');
    });

    content.classList.add('active');
    btn.classList.add('active');
    localStorage.setItem('activeTab', tabName);

    if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
    }

    if (tabName === 'rules') {
        loadRules();
    } else if (tabName === 'url-sources') {
        loadURLSources();
        autoRefreshInterval = setInterval(loadURLSources, 5000);
    } else if (tabName === 'config') {
        openConfigTab();
    }

    return true;
}

function switchTab(tabName) {
    activateTab(tabName);
}

function restoreActiveTab() {
    const savedTab = localStorage.getItem('activeTab') || 'rules';
    if (!activateTab(savedTab)) {
        activateTab('rules');
    }
}

function updatePendingButton(btnId, count) {
    const btn = document.getElementById(btnId);
    if (!btn) return;

    let badge = btn.querySelector('.pending-badge');

    if (count > 0) {
        btn.disabled = false;
        if (!badge) {
            badge = document.createElement('span');
            badge.className = 'pending-badge';
            btn.appendChild(badge);
        }
        badge.textContent = count;
    } else {
        btn.disabled = true;
        if (badge) badge.remove();
    }
}

function updateApplyButton() {
    updatePendingButton('applyBtn', pendingCount);
}

function updateApplyURLButton() {
    updatePendingButton('applyUrlBtn', urlPendingCount);
}

// Rules

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
            <td><span class="badge badge-${escapeHTML(rule.type)}">${getTypeLabel(rule.type)}</span></td>
            <td><code>${escapeHTML(rule.value)}</code></td>
            <td class="description-cell">${escapeHTML(rule.description || '')}</td>
            <td class="actions-cell">
                <button class="btn btn-danger" onclick="deleteRule('${escapeHTML(rule.id)}')" ${rule.deleted ? 'disabled' : ''}>
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
    return labels[type] || escapeHTML(type);
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

// URL Sources

async function loadURLSources() {
    try {
        const response = await fetch('/api/url-sources');
        const data = await response.json();
        const sources = data.url_sources || [];
        displayURLSources(sources);

        urlPendingCount = sources.filter(s => !s.applied || s.deleted).length;
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
        const url = escapeHTML(source.url);
        const id = escapeHTML(source.id);

        return `
            <tr class="${source.deleted ? 'deleted' : (source.applied ? '' : 'pending')}">
                <td>
                    <span class="status-badge ${source.deleted ? 'status-deleted' : (source.applied ? 'status-applied' : 'status-pending')}">
                        ${source.deleted ? 'К удалению' : (source.applied ? 'Применено' : 'Ожидает')}
                    </span>
                    <div class="status-indicator ${statusClass}">
                        <span class="status-icon">${statusIcon}</span>
                        <span class="status-text">${statusText}</span>
                    </div>
                    ${source.last_error ? `<div class="source-error">${escapeHTML(source.last_error)}</div>` : ''}
                </td>
                <td class="url-cell" title="${url}">${url}</td>
                <td class="description-cell">${escapeHTML(source.description || '')}</td>
                <td>${escapeHTML(source.interval)} мин</td>
                <td class="date-cell">${lastUpdate}</td>
                <td>${source.items_count || 0}</td>
                <td class="actions-cell">
                    <button class="btn btn-info" onclick="viewURLSourceRules('${id}')" ${!source.applied ? 'disabled' : ''}>
                        Посмотреть
                    </button>
                    <button class="btn btn-danger" onclick="deleteURLSource('${id}')" ${source.deleted ? 'disabled' : ''}>
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
    const interval = parseInt(document.getElementById('urlSourceInterval').value, 10);
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
        const response = await fetch(`/api/url-sources/rules?id=${encodeURIComponent(id)}`);
        const data = await response.json();

        const cidrList = data.cidrList || [];
        const domains = data.domains || [];
        const domainSuffixes = data.domainSuffixes || [];

        if (cidrList.length === 0 && domains.length === 0 && domainSuffixes.length === 0) {
            showMessage('Нет загруженных правил для этого источника', 'error');
            return;
        }

        const section = (title, items) => items.length === 0 ? '' : `
            <div class="modal-section">
                <h5>${title} (${items.length})</h5>
                <pre>${escapeHTML(items.join('\n'))}</pre>
            </div>
        `;

        const modal = `
            <div class="modal-overlay" onclick="this.remove()">
                <div class="modal" onclick="event.stopPropagation()">
                    <h3>Загруженные правила</h3>
                    ${section('Префиксы CIDR', cidrList)}
                    ${section('Конкретные домены', domains)}
                    ${section('Суффиксы доменов', domainSuffixes)}
                    <button class="btn btn-primary modal-close" onclick="this.closest('.modal-overlay').remove()">Закрыть</button>
                </div>
            </div>
        `;
        document.body.insertAdjacentHTML('beforeend', modal);
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

// Control

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

// Config Editor
//
// Архитектура: textarea является единственным скролл-контейнером. Слой
// подсветки и номера строк лежат под ним и синхронизируются через
// transform (только композитинг, без layout). Подсветка строится построчно
// однопроходным токенизатором; при вводе перерисовываются только
// изменившиеся строки.

const INDENT = '  ';
const UNDO_LIMIT = 200;
const UNDO_GROUP_MS = 700;

const editorState = {
    initialized: false,
    editor: null,
    highlightInner: null,
    gutterInner: null,
    container: null,
    lineHeight: 22,
    lines: [],
    // enter[i] - находится ли начало строки i внутри блочного комментария.
    enter: [false],
    gutterDigits: 0,
    renderScheduled: false,
    scrollScheduled: false,
    originalConfig: '', // Последний применённый конфиг (для diff индикаторов)
    loadedConfig: '', // Конфиг, загруженный в редактор (для определения изменений)
    hasPendingChanges: false,
    undoStack: [],
    redoStack: [],
    lastState: null,
    lastInputType: null,
    lastInputTime: 0
};

const KEYWORD_CLASS = {
    'true': 'json-boolean',
    'false': 'json-boolean',
    'null': 'json-null'
};

const NUMBER_RE = /-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/y;
const WORD_RE = /[A-Za-z_$][\w$]*/y;
const PLAIN_RE = /[^"\/#\-\d{}\[\],:A-Za-z_$]+/y;

function span(cls, text) {
    return '<span class="' + cls + '">' + escapeHTML(text) + '</span>';
}

// Токенизирует одну строку. Возвращает HTML и состояние блочного
// комментария на конце строки.
function tokenizeLine(line, inBlock) {
    const n = line.length;
    let html = '';
    let i = 0;

    while (i < n) {
        if (inBlock) {
            const end = line.indexOf('*/', i);
            if (end === -1) {
                html += span('json-comment', line.slice(i));
                i = n;
            } else {
                html += span('json-comment', line.slice(i, end + 2));
                i = end + 2;
                inBlock = false;
            }

            continue;
        }

        const ch = line[i];

        if (ch === '"') {
            let j = i + 1;
            while (j < n) {
                if (line[j] === '\\') {
                    j += 2;
                    continue;
                }
                if (line[j] === '"') break;
                j++;
            }
            j = Math.min(j + 1, n);

            let k = j;
            while (k < n && (line[k] === ' ' || line[k] === '\t')) k++;

            html += span(line[k] === ':' ? 'json-key' : 'json-string', line.slice(i, j));
            i = j;
            continue;
        }

        if (ch === '#' || (ch === '/' && line[i + 1] === '/')) {
            html += span('json-comment', line.slice(i));
            i = n;
            continue;
        }

        if (ch === '/' && line[i + 1] === '*') {
            inBlock = true;
            continue;
        }

        if (ch === '{' || ch === '}' || ch === '[' || ch === ']' || ch === ',' || ch === ':') {
            html += '<span class="json-punctuation">' + ch + '</span>';
            i++;
            continue;
        }

        NUMBER_RE.lastIndex = i;
        let m = NUMBER_RE.exec(line);
        if (m) {
            html += span('json-number', m[0]);
            i += m[0].length;
            continue;
        }

        WORD_RE.lastIndex = i;
        m = WORD_RE.exec(line);
        if (m) {
            const cls = KEYWORD_CLASS[m[0]];
            html += cls ? span(cls, m[0]) : escapeHTML(m[0]);
            i += m[0].length;
            continue;
        }

        PLAIN_RE.lastIndex = i;
        m = PLAIN_RE.exec(line);
        if (m) {
            html += escapeHTML(m[0]);
            i += m[0].length;
            continue;
        }

        html += escapeHTML(ch);
        i++;
    }

    return { html, inBlock };
}

function createLineElement(html) {
    const el = document.createElement('div');
    el.className = 'hl-line';
    el.innerHTML = html;

    return el;
}

// Перестраивает подсветку, обновляя только изменившиеся строки.
function renderHighlight() {
    const st = editorState;
    const container = st.highlightInner;
    const newLines = st.editor.value.split('\n');
    const oldLines = st.lines;
    const oldEnter = st.enter;
    const oldLen = oldLines.length;
    const newLen = newLines.length;

    // Находим общий префикс неизменённых строк.
    let prefix = 0;
    const minLen = Math.min(oldLen, newLen);
    while (prefix < minLen && oldLines[prefix] === newLines[prefix]) prefix++;

    // Находим общий суффикс неизменённых строк.
    let suffix = 0;
    while (
        suffix < minLen - prefix &&
        oldLines[oldLen - 1 - suffix] === newLines[newLen - 1 - suffix]
    ) suffix++;

    const oldEnd = oldLen - suffix;
    const newEnd = newLen - suffix;
    const newEnter = new Array(newLen + 1);

    // Копируем состояния для неизменённого префикса.
    for (let i = 0; i <= prefix; i++) newEnter[i] = oldEnter[i];

    const children = container.children;
    let inBlock = newEnter[prefix] || false;

    // Сначала удаляем старые элементы из изменённого диапазона.
    for (let k = oldEnd - 1; k >= prefix; k--) {
        children[k].remove();
    }

    // Создаём новые элементы для изменённого диапазона.
    const fragment = document.createDocumentFragment();
    for (let i = prefix; i < newEnd; i++) {
        const res = tokenizeLine(newLines[i], inBlock);
        inBlock = res.inBlock;
        newEnter[i + 1] = inBlock;
        fragment.appendChild(createLineElement(res.html));
    }

    // Вставляем новые элементы перед суффиксом (или в конец).
    container.insertBefore(fragment, children[prefix] || null);

    // Обрабатываем суффикс: состояние блочного комментария могло измениться.
    let i = newEnd;
    for (; i < newLen; i++) {
        const oldIdx = i - newLen + oldLen;
        if (inBlock === oldEnter[oldIdx]) {
            // Состояние совпало, копируем оставшиеся.
            for (let k = i; k <= newLen; k++) {
                newEnter[k] = oldEnter[k - newLen + oldLen];
            }
            break;
        }

        // Перерендериваем строку с новым состоянием.
        const res = tokenizeLine(newLines[i], inBlock);
        inBlock = res.inBlock;
        newEnter[i + 1] = inBlock;
        children[i].innerHTML = res.html;
    }

    st.lines = newLines;
    st.enter = newEnter;

    renderGutter(newLen);
}

// Вычисляет LCS (longest common subsequence) diff между двумя массивами строк.
// Возвращает массив операций: { type: 'equal'|'insert'|'delete'|'modify', oldIndex, newIndex }
function computeLCS(oldLines, newLines) {
    const m = oldLines.length;
    const n = newLines.length;
    
    // DP таблица для LCS длин
    const dp = Array(m + 1).fill(0).map(() => Array(n + 1).fill(0));
    
    for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
            if (oldLines[i - 1] === newLines[j - 1]) {
                dp[i][j] = dp[i - 1][j - 1] + 1;
            } else {
                dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1]);
            }
        }
    }
    
    // Восстанавливаем diff
    const diff = [];
    let i = m;
    let j = n;
    
    while (i > 0 || j > 0) {
        if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
            diff.push({ type: 'equal', oldIndex: i - 1, newIndex: j - 1 });
            i--;
            j--;
        } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
            diff.push({ type: 'insert', newIndex: j - 1 });
            j--;
        } else if (i > 0) {
            diff.push({ type: 'delete', oldIndex: i - 1 });
            i--;
        }
    }
    
    return diff.reverse();
}

// Вычисляет diff между оригиналом и текущим содержимым.
// Возвращает массив статусов для каждой строки: 'modified', 'added', 'deleted', null.
function computeLineDiff() {
    const st = editorState;
    const original = st.originalConfig.split('\n');
    const current = st.editor.value.split('\n');
    const result = new Array(current.length).fill(null);
    
    const diff = computeLCS(original, current);
    
    // Проходим по diff и группируем соседние delete/insert блоки
    let i = 0;
    let lastEqualNewIndex = -1;
    
    while (i < diff.length) {
        const op = diff[i];
        
        if (op.type === 'equal') {
            lastEqualNewIndex = op.newIndex;
            i++;
            continue;
        }
        
        // Собираем блок delete/insert операций
        const deleteOps = [];
        const insertOps = [];
        
        while (i < diff.length && diff[i].type !== 'equal') {
            if (diff[i].type === 'delete') {
                deleteOps.push(diff[i]);
            } else if (diff[i].type === 'insert') {
                insertOps.push(diff[i]);
            }
            i++;
        }
        
        // Если есть хоть один insert, это замена/добавление, не чистое удаление
        if (insertOps.length > 0) {
            // Сопоставляем delete и insert в этом блоке
            const matchedCount = Math.min(deleteOps.length, insertOps.length);
            
            for (let j = 0; j < matchedCount; j++) {
                result[insertOps[j].newIndex] = 'modified';
            }
            
            // Оставшиеся insert — added
            for (let j = matchedCount; j < insertOps.length; j++) {
                result[insertOps[j].newIndex] = 'added';
            }
        } else if (deleteOps.length > 0) {
            // Чистый блок удалений без вставок
            // Показываем deleted индикатор на последней equal строке перед блоком
            if (lastEqualNewIndex >= 0 && lastEqualNewIndex < current.length) {
                result[lastEqualNewIndex] = 'deleted';
            }
        }
    }
    
    return result;
}

function renderGutter(count) {
    const st = editorState;
    const gutter = st.gutterInner;
    const current = gutter.childElementCount;

    // Вычисляем diff статусы.
    const diff = computeLineDiff();

    if (count > current) {
        const fragment = document.createDocumentFragment();
        for (let i = current + 1; i <= count; i++) {
            const el = document.createElement('div');
            el.textContent = i;
            el.className = 'gutter-line';
            if (diff[i - 1]) {
                el.classList.add('line-' + diff[i - 1]);
            }
            fragment.appendChild(el);
        }
        gutter.appendChild(fragment);
    } else {
        for (let i = current; i > count; i--) gutter.lastElementChild.remove();
    }

    // Обновляем классы для существующих строк.
    const children = gutter.children;
    for (let i = 0; i < count; i++) {
        const el = children[i];
        el.className = 'gutter-line';
        if (diff[i]) {
            el.classList.add('line-' + diff[i]);
        }
    }

    const digits = Math.max(2, String(count).length);
    if (digits !== st.gutterDigits) {
        st.gutterDigits = digits;
        st.container.style.setProperty('--gutter-digits', digits);
    }
}

function scheduleRender() {
    const st = editorState;
    if (st.renderScheduled) return;

    st.renderScheduled = true;
    requestAnimationFrame(() => {
        if (!st.renderScheduled) return; // Отменён извне
        st.renderScheduled = false;
        renderHighlight();
    });
}

function syncScroll() {
    const st = editorState;
    if (st.scrollScheduled) return;

    st.scrollScheduled = true;
    requestAnimationFrame(() => {
        st.scrollScheduled = false;
        const x = st.editor.scrollLeft;
        const y = st.editor.scrollTop;
        st.highlightInner.style.transform = `translate(${-x}px, ${-y}px)`;
        st.gutterInner.style.transform = `translateY(${-y}px)`;
    });
}

function snapshot() {
    const ed = editorState.editor;

    return {
        value: ed.value,
        selectionStart: ed.selectionStart,
        selectionEnd: ed.selectionEnd
    };
}

function pushUndo(state) {
    const st = editorState;
    st.undoStack.push(state);
    if (st.undoStack.length > UNDO_LIMIT) st.undoStack.shift();
    st.redoStack = [];
}

function lineIndexAt(value, pos) {
    let count = 0;
    let idx = value.indexOf('\n');
    while (idx !== -1 && idx < pos) {
        count++;
        idx = value.indexOf('\n', idx + 1);
    }

    return count;
}

function ensureCaretVisible() {
    const st = editorState;
    const ed = st.editor;
    const line = lineIndexAt(ed.value, ed.selectionEnd);
    const lh = st.lineHeight;
    const y = line * lh;
    const viewTop = ed.scrollTop;
    const viewHeight = ed.clientHeight;

    if (y < viewTop + lh) {
        ed.scrollTop = Math.max(0, y - lh);
    } else if (y + lh * 2 > viewTop + viewHeight) {
        ed.scrollTop = y + lh * 2 - viewHeight;
    }
}

function restoreState(state) {
    const st = editorState;
    st.renderScheduled = false; // Отменяем запланированный рендер
    st.editor.value = state.value;
    st.editor.setSelectionRange(state.selectionStart, state.selectionEnd);
    st.lastState = snapshot();
    st.lastInputType = null;
    renderHighlight();
    updateConfigButtons();
    ensureCaretVisible();
    syncScroll();
}

// Применяет программное изменение текста с записью в историю.
function applyEdit(value, selectionStart, selectionEnd) {
    pushUndo(snapshot());
    restoreState({ value, selectionStart, selectionEnd });
}

function undo() {
    const st = editorState;
    if (st.undoStack.length === 0) return;

    st.redoStack.push(snapshot());
    restoreState(st.undoStack.pop());
}

function redo() {
    const st = editorState;
    if (st.redoStack.length === 0) return;

    st.undoStack.push(snapshot());
    restoreState(st.redoStack.pop());
}

// Возвращает индексы первой и последней строки, попадающих в выделение.
function selectedLineRange(lines, start, end) {
    let pos = 0;
    let startLine = -1;
    let endLine = -1;

    for (let i = 0; i < lines.length; i++) {
        const lineEnd = pos + lines[i].length;
        if (startLine === -1 && start <= lineEnd) startLine = i;
        if (end <= lineEnd) {
            endLine = i;
            break;
        }
        pos = lineEnd + 1;
    }

    if (startLine === -1) startLine = lines.length - 1;
    if (endLine === -1) endLine = lines.length - 1;

    // Если выделение заканчивается в начале строки, эту строку не трогаем.
    if (endLine > startLine) {
        let lineStart = 0;
        for (let i = 0; i < endLine; i++) lineStart += lines[i].length + 1;
        if (end === lineStart) endLine--;
    }

    return { startLine, endLine };
}

function handleTabKey(e) {
    const ed = e.target;
    const start = ed.selectionStart;
    const end = ed.selectionEnd;
    const value = ed.value;
    const multiLine = start !== end && value.slice(start, end).includes('\n');

    if (!multiLine && !e.shiftKey) {
        const newValue = value.slice(0, start) + INDENT + value.slice(end);
        applyEdit(newValue, start + INDENT.length, start + INDENT.length);

        return;
    }

    const lines = value.split('\n');
    const { startLine, endLine } = selectedLineRange(lines, start, end);
    let newStart = start;
    let newEnd = end;
    let firstLineStart = 0;
    for (let i = 0; i < startLine; i++) firstLineStart += lines[i].length + 1;

    for (let i = startLine; i <= endLine; i++) {
        let delta = 0;

        if (e.shiftKey) {
            const m = lines[i].match(/^ {1,2}|^\t/);
            if (m) {
                lines[i] = lines[i].slice(m[0].length);
                delta = -m[0].length;
            }
        } else {
            lines[i] = INDENT + lines[i];
            delta = INDENT.length;
        }

        if (i === startLine) {
            newStart = Math.max(firstLineStart, start + delta);
        }
        newEnd += delta;
    }

    applyEdit(lines.join('\n'), newStart, Math.max(newStart, newEnd));
}

function handleEnterKey(e) {
    const ed = e.target;
    const start = ed.selectionStart;
    const end = ed.selectionEnd;
    const value = ed.value;
    const lineStart = value.lastIndexOf('\n', start - 1) + 1;
    const indent = value.slice(lineStart, start).match(/^[ \t]*/)[0];
    const before = value.slice(lineStart, start).trimEnd();
    const lastChar = before[before.length - 1];
    const nextChar = value[end];
    const opens = lastChar === '{' || lastChar === '[';
    const closes = (lastChar === '{' && nextChar === '}') ||
        (lastChar === '[' && nextChar === ']');

    let insert = '\n' + indent;
    if (opens) insert += INDENT;
    const caret = start + insert.length;
    if (closes) insert += '\n' + indent;

    applyEdit(value.slice(0, start) + insert + value.slice(end), caret, caret);
}

function toggleComment(e) {
    const ed = e.target;
    const start = ed.selectionStart;
    const end = ed.selectionEnd;
    const lines = ed.value.split('\n');
    const { startLine, endLine } = selectedLineRange(lines, start, end);

    let allCommented = true;
    for (let i = startLine; i <= endLine; i++) {
        if (lines[i].trim() === '') continue;
        if (!lines[i].trim().startsWith('//')) {
            allCommented = false;
            break;
        }
    }

    let offsetStart = 0;
    let offsetEnd = 0;

    for (let i = startLine; i <= endLine; i++) {
        if (lines[i].trim() === '' && !allCommented) continue;

        let delta = 0;
        if (allCommented) {
            const m = lines[i].match(/^(\s*)\/\/ ?/);
            if (m) {
                lines[i] = m[1] + lines[i].slice(m[0].length);
                delta = -(m[0].length - m[1].length);
            }
        } else {
            const indent = lines[i].match(/^\s*/)[0];
            lines[i] = indent + '// ' + lines[i].slice(indent.length);
            delta = 3;
        }

        if (i === startLine) offsetStart += delta;
        offsetEnd += delta;
    }

    const newStart = Math.max(0, start + offsetStart);
    applyEdit(lines.join('\n'), newStart, Math.max(newStart, end + offsetEnd));
}

function handleEditorKeydown(e) {
    const mod = e.ctrlKey || e.metaKey;
    const code = e.code;

    if (e.key === 'Tab') {
        e.preventDefault();
        handleTabKey(e);

        return;
    }

    if (e.key === 'Enter' && !mod && !e.shiftKey && !e.altKey) {
        e.preventDefault();
        handleEnterKey(e);

        return;
    }

    if (!mod || e.altKey) return;

    if (code === 'KeyZ' || e.key === 'z') {
        e.preventDefault();
        if (e.shiftKey) redo(); else undo();
    } else if (code === 'KeyY' || e.key === 'y') {
        e.preventDefault();
        redo();
    } else if (code === 'Slash' || e.key === '/') {
        e.preventDefault();
        toggleComment(e);
    } else if (code === 'KeyS' || e.key === 's') {
        e.preventDefault();
        const saveBtn = document.getElementById('saveConfigBtn');
        if (saveBtn && !saveBtn.disabled) saveTempConfig();
    }
}

function handleEditorInput(e) {
    const st = editorState;
    const now = performance.now();
    const type = e.inputType || '';
    const groupable = type === 'insertText' ||
        type === 'deleteContentBackward' ||
        type === 'deleteContentForward';

    if (!groupable || type !== st.lastInputType || now - st.lastInputTime > UNDO_GROUP_MS) {
        pushUndo(st.lastState);
    }

    st.lastInputType = type;
    st.lastInputTime = now;
    st.lastState = snapshot();

    scheduleRender();
    updateConfigButtons();
}

function isConfigModified() {
    const st = editorState;

    return st.initialized && st.editor.value !== st.loadedConfig;
}

function updateConfigButtons() {
    const saveBtn = document.getElementById('saveConfigBtn');
    const applyBtn = document.getElementById('applyConfigBtn');
    const discardBtn = document.getElementById('discardConfigBtn');
    const statusSpan = document.getElementById('configStatus');

    if (!saveBtn || !applyBtn || !discardBtn || !statusSpan) return;

    const st = editorState;
    const isModified = isConfigModified();

    saveBtn.disabled = !isModified;
    discardBtn.disabled = !isModified && !st.hasPendingChanges;
    applyBtn.disabled = !st.hasPendingChanges;

    let text = '';
    let cls = 'config-status';

    if (isModified) {
        text = '✏️ Есть несохранённые изменения';
        cls += ' modified';
    } else if (st.hasPendingChanges) {
        text = '⚠️ Сохранено, ожидает применения';
        cls += ' saved';
    }

    if (statusSpan.textContent !== text) statusSpan.textContent = text;
    if (statusSpan.className !== cls) statusSpan.className = cls;
}

function initEditor() {
    const st = editorState;
    if (st.initialized) return true;

    const editor = document.getElementById('configEditor');
    const highlightInner = document.getElementById('syntaxHighlight');
    const gutterInner = document.getElementById('lineNumbers');
    const container = document.getElementById('editorContainer');
    if (!editor || !highlightInner || !gutterInner || !container) return false;

    st.editor = editor;
    st.highlightInner = highlightInner;
    st.gutterInner = gutterInner;
    st.container = container;
    st.lineHeight = parseFloat(getComputedStyle(editor).lineHeight) || 22;
    st.initialized = true;

    editor.addEventListener('keydown', handleEditorKeydown);
    editor.addEventListener('input', handleEditorInput);
    editor.addEventListener('scroll', syncScroll, { passive: true });

    window.addEventListener('beforeunload', (e) => {
        if (isConfigModified()) {
            e.preventDefault();
            e.returnValue = '';
        }
    });

    return true;
}

function setEditorContent(text) {
    const st = editorState;
    st.editor.value = text;
    st.editor.setSelectionRange(0, 0);
    st.editor.scrollTop = 0;
    st.editor.scrollLeft = 0;
    st.loadedConfig = text; // Запоминаем загруженный контент
    st.undoStack = [];
    st.redoStack = [];
    st.lastState = snapshot();
    st.lastInputType = null;
    renderHighlight();
    syncScroll();
}

// Открытие вкладки: не перезагружаем конфиг, если есть несохранённые
// правки.
function openConfigTab() {
    if (!initEditor()) return;

    if (isConfigModified()) {
        updateConfigButtons();

        return;
    }

    loadConfig();
}

async function loadConfig() {
    if (!initEditor()) return;

    try {
        const response = await fetch('/api/config/get');
        if (!response.ok) {
            throw new Error(await response.text());
        }
        const data = await response.json();

        let text = data.config || '';
        let appliedText = data.appliedConfig || text;
        
        // Форматируем только если это чистый JSON без комментариев
        const hasComments = text.includes('//') || text.includes('/*') || text.includes('#');
        
        if (!hasComments) {
            try {
                text = JSON.stringify(JSON.parse(text), null, 2);
            } catch (e) {
                // Оставляем как есть
            }
        }
        
        const hasCommentsInApplied = appliedText.includes('//') || appliedText.includes('/*') || appliedText.includes('#');
        
        if (!hasCommentsInApplied) {
            try {
                appliedText = JSON.stringify(JSON.parse(appliedText), null, 2);
            } catch (e) {
                // Оставляем как есть
            }
        }

        const st = editorState;
        st.originalConfig = appliedText; // Baseline — последний применённый конфиг
        st.loadedConfig = text; // Текущий загруженный
        st.hasPendingChanges = Boolean(data.hasPending);
        setEditorContent(text);
        updateConfigButtons();
    } catch (error) {
        showMessage('Ошибка загрузки конфига: ' + error.message, 'error');
    }
}

async function saveTempConfig() {
    const st = editorState;
    if (!st.initialized) return;

    const config = st.editor.value;

    try {
        const response = await fetch('/api/config/save-temp', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ config })
        });

        if (!response.ok) {
            const text = await response.text();
            let message = text;
            try {
                message = JSON.parse(text).error || text;
            } catch (e) {
                // Ответ не в формате JSON.
            }
            throw new Error(message || 'Ошибка при сохранении');
        }

        showMessage('Конфиг сохранён временно. Нажмите "Применить" для активации.', 'success');
        st.loadedConfig = config; // Обновляем loaded для сравнения
        st.hasPendingChanges = true;
        updateConfigButtons();
        renderGutter(st.lines.length); // Обновляем индикаторы изменений
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

        const st = editorState;
        const currentValue = st.editor.value;
        st.originalConfig = currentValue; // Новый baseline для diff
        st.loadedConfig = currentValue; // Обновляем loaded
        st.hasPendingChanges = false;
        updateConfigButtons();
        renderGutter(st.lines.length); // Сброс индикаторов
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function discardConfig() {
    if (!confirm('Отменить все несохранённые изменения?')) {
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
        editorState.originalConfig = '';
        editorState.loadedConfig = '';
        await loadConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

restoreActiveTab();
