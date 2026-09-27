let pendingCount = 0;
let urlPendingCount = 0;
let autoRefreshInterval = null;
let groups = []; // Список групп

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
    const navItem = document.querySelector(`.nav-item[data-tab="${tabName}"]`);
    if (!content || !navItem) return false;

    document.querySelectorAll('.tab-content.active').forEach(tab => {
        tab.classList.remove('active');
    });
    document.querySelectorAll('.nav-item.active').forEach(item => {
        item.classList.remove('active');
    });

    content.classList.add('active');
    navItem.classList.add('active');
    localStorage.setItem('activeTab', tabName);
    
    // Обновляем заголовок и кнопки действий
    updateTopBar(tabName);

    if (autoRefreshInterval) {
        clearInterval(autoRefreshInterval);
        autoRefreshInterval = null;
    }

    if (tabName === 'overview') {
        loadOverview();
        autoRefreshInterval = setInterval(loadOverview, 1000);
    } else if (tabName === 'rules') {
        loadRules();
    } else if (tabName === 'url-sources') {
        loadURLSources();
        autoRefreshInterval = setInterval(loadURLSources, 5000);
    } else if (tabName === 'groups') {
        loadGroups();
    } else if (tabName === 'dns-records') {
        loadDNSRecords();
    } else if (tabName === 'outbounds') {
        loadOutbounds();
    } else if (tabName === 'happ') {
        loadHappProfiles();
        loadAmneziaProfiles();
        autoRefreshInterval = setInterval(() => {
            loadHappProfiles();
            loadAmneziaProfiles();
        }, 30000);
    } else if (tabName === 'config') {
        openConfigTab();
    } else if (tabName === 'control') {
        loadClashProxies();
        autoRefreshInterval = setInterval(refreshClashProxies, 5000);
    }

    return true;
}

function switchTab(tabName) {
    activateTab(tabName);
}

function updateTopBar(tabName) {
    const pageTitle = document.getElementById('pageTitle');
    const topBarActions = document.getElementById('topBarActions');
    
    if (!pageTitle || !topBarActions) return;
    
    const titles = {
        'overview': 'Обзор',
        'rules': 'Правила маршрутизации',
        'url-sources': 'URL Источники',
        'groups': 'Группы',
        'dns-records': 'DNS-записи',
        'outbounds': 'Outbounds',
        'happ': 'Подписки',
        'config': 'Конфигурация',
        'control': 'Управление'
    };
    
    pageTitle.textContent = titles[tabName] || 'Sing-Box Configurer';
    
    // Обновляем кнопки действий
    if (tabName === 'rules') {
        topBarActions.innerHTML = '<button id="applyBtn" class="btn btn-warning" onclick="applyRules()">Применить правила</button>';
        updateApplyButton();
    } else if (tabName === 'url-sources') {
        topBarActions.innerHTML = '<button id="applyUrlBtn" class="btn btn-warning" onclick="applyURLSources()">Применить URL источники</button>';
        updateApplyURLButton();
    } else if (tabName === 'control') {
        topBarActions.innerHTML = `<button id="reloadBtn" class="btn btn-warning" onclick="reloadSingBox()">${ICON_RESTART}Перезагрузить Sing-Box</button>`;
    } else {
        topBarActions.innerHTML = '';
    }
}

function restoreActiveTab() {
    const savedTab = localStorage.getItem('activeTab') || 'overview';
    if (!activateTab(savedTab)) {
        activateTab('overview');
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

// Groups helpers

async function loadGroupsForSelect() {
    try {
        const response = await fetch('/api/groups');
        const data = await response.json();
        groups = data.groups || [];
        
        // Обновляем выпадающий список для правил
        const ruleGroupSelect = document.getElementById('ruleGroup');
        if (ruleGroupSelect) {
            ruleGroupSelect.innerHTML = groups.map(g => 
                `<option value="${escapeHTML(g.name)}">${escapeHTML(g.name)}</option>`
            ).join('');
        }
        
        // Обновляем выпадающий список для URL источников
        const urlSourceGroupSelect = document.getElementById('urlSourceGroup');
        if (urlSourceGroupSelect) {
            urlSourceGroupSelect.innerHTML = groups.map(g => 
                `<option value="${escapeHTML(g.name)}">${escapeHTML(g.name)}</option>`
            ).join('');
        }
    } catch (error) {
        console.error('Ошибка загрузки групп для селекта:', error);
    }
}

async function loadGroups() {
    try {
        const response = await fetch('/api/groups');
        const data = await response.json();
        groups = data.groups || [];
        displayGroups(groups);
    } catch (error) {
        showMessage('Ошибка загрузки групп: ' + error.message, 'error');
    }
}

function displayGroups(groups) {
    const tbody = document.getElementById('groupsBody');

    if (groups.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">Нет групп.</td></tr>';
        return;
    }

    tbody.innerHTML = groups.map(group => {
        const createdAt = new Date(group.created_at).toLocaleString('ru-RU');
        const isDefault = group.name === 'default';
        const defaultOutbound = group.default_outbound || 'direct';
        const dnsServer = group.dns_server || '';

        return `
            <tr>
                <td>
                    <strong>${escapeHTML(group.name)}</strong>
                    ${isDefault ? '<span class="badge badge-group" style="margin-left: 10px;">По умолчанию</span>' : ''}
                </td>
                <td class="description-cell">${escapeHTML(group.description || '')}</td>
                <td>${escapeHTML(defaultOutbound)}</td>
                <td>${dnsServer ? escapeHTML(dnsServer) : '<span style="color: #8b949e;">dns.final</span>'}</td>
                <td class="date-cell">${createdAt}</td>
                <td class="actions-cell">
                    <button class="btn btn-secondary" onclick="editGroup('${escapeHTML(group.name)}', '${escapeHTML(group.description || '')}', '${escapeHTML(defaultOutbound)}', '${escapeHTML(dnsServer)}')">
                        Редактировать
                    </button>
                    <button class="btn btn-danger" onclick="deleteGroup('${escapeHTML(group.name)}')" ${isDefault ? 'disabled' : ''}>
                        ${isDefault ? 'Нельзя удалить' : 'Удалить'}
                    </button>
                </td>
            </tr>
        `;
    }).join('');
}

async function addGroup(event) {
    event.preventDefault();

    const name = document.getElementById('groupName').value.trim();
    const description = document.getElementById('groupDescription').value;
    const defaultOutbound = document.getElementById('groupDefaultOutbound').value.trim();
    const dnsServer = document.getElementById('groupDNSServer').value.trim();

    if (!name) {
        showMessage('Введите имя группы', 'error');
        return;
    }

    try {
        const response = await fetch('/api/groups/add', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ 
                name, 
                description,
                default_outbound: defaultOutbound || '',
                dns_server: dnsServer
            })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при добавлении группы');
        }

        showMessage('Группа успешно добавлена', 'success');
        document.getElementById('groupName').value = '';
        document.getElementById('groupDescription').value = '';
        document.getElementById('groupDefaultOutbound').value = '';
        document.getElementById('groupDNSServer').value = '';
        await loadGroups();
        await loadGroupsForSelect();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function deleteGroup(name) {
    if (!confirm(`Удалить группу "${name}"?`)) {
        return;
    }

    try {
        const response = await fetch('/api/groups/delete', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ name })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при удалении группы');
        }

        showMessage('Группа успешно удалена', 'success');
        await loadGroups();
        await loadGroupsForSelect();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function editGroup(name, description, defaultOutbound, dnsServer) {
    const modal = `
        <div class="modal-overlay" onclick="closeEditModal(event)">
            <div class="modal" onclick="event.stopPropagation()">
                <h3>Редактировать группу "${escapeHTML(name)}"</h3>
                <form id="editGroupForm" onsubmit="submitEditGroup(event, '${escapeHTML(name)}')">
                    <div class="form-group">
                        <label class="form-label" for="editGroupDescription">Описание</label>
                        <textarea class="form-textarea" id="editGroupDescription" required>${escapeHTML(description)}</textarea>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editGroupDefaultOutbound">Default Outbound</label>
                        <input class="form-input" type="text" id="editGroupDefaultOutbound" value="${escapeHTML(defaultOutbound)}" required>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editGroupDNSServer">DNS-сервер</label>
                        <input class="form-input" type="text" id="editGroupDNSServer" value="${escapeHTML(dnsServer || '')}" placeholder="пусто — через dns.final">
                    </div>
                    <div class="form-actions">
                        <button type="button" class="btn btn-secondary" onclick="closeEditModal()">Отмена</button>
                        <button type="submit" class="btn btn-primary">Сохранить</button>
                    </div>
                </form>
            </div>
        </div>
    `;
    document.body.insertAdjacentHTML('beforeend', modal);
}

async function submitEditGroup(event, name) {
    event.preventDefault();

    const description = document.getElementById('editGroupDescription').value;
    const defaultOutbound = document.getElementById('editGroupDefaultOutbound').value;
    const dnsServer = document.getElementById('editGroupDNSServer').value.trim();

    try {
        const response = await fetch('/api/groups/edit', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({
                name,
                description,
                default_outbound: defaultOutbound,
                dns_server: dnsServer
            })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при редактировании группы');
        }

        showMessage('Группа успешно обновлена', 'success');
        closeEditModal();
        await loadGroups();
        await loadGroupsForSelect();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

function closeEditModal(event) {
    if (event && event.target.classList.contains('modal-overlay')) {
        event.target.remove();
    } else {
        document.querySelector('.modal-overlay')?.remove();
    }
}

async function syncGroups(groupName) {
    try {
        const response = await fetch('/api/config/sync-groups', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ group_name: groupName })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при синхронизации группы');
        }

        showMessage(`Группа "${groupName}" синхронизирована во временный конфиг.`, 'success');
        
        // Обновляем бейдж на вкладке конфига и проверяем статус групп
        checkPendingConfig();
        checkGroupsSync();
        
        // Перезагружаем конфиг в редакторе, если он открыт
        if (document.getElementById('config-tab').classList.contains('active')) {
            await loadConfig();
        }
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function checkGroupsSync() {
    try {
        const response = await fetch('/api/config/check-groups-sync');
        if (!response.ok) {
            console.error('Failed to check groups sync');
            return;
        }

        const data = await response.json();
        const statuses = data.statuses || [];

        displayGroupSyncWarnings(statuses);
    } catch (error) {
        console.error('Failed to check groups sync:', error);
    }
}

function displayGroupSyncWarnings(statuses) {
    const container = document.getElementById('groupSyncWarnings');
    if (!container) return;

    const unsyncedGroups = statuses.filter(s => !s.synced);

    if (unsyncedGroups.length === 0) {
        container.innerHTML = '';
        return;
    }

    container.innerHTML = unsyncedGroups.map(status => {
        const issues = [];
        if (!status.has_rule_set) issues.push('отсутствует rule_set');
        if (!status.has_rule) issues.push('отсутствует rule');
        if (!status.has_selector) issues.push('отсутствует selector');
        if (status.has_selector && status.actual_outbound !== status.default_outbound) {
            issues.push(`outbound: ${status.actual_outbound} → ${status.default_outbound}`);
        }
        if (!status.dns_synced) {
            const actual = status.actual_dns_server || 'dns.final';
            const expected = status.dns_server || 'dns.final';
            issues.push(actual === expected ? `неполные DNS-правила (${expected})` : `DNS: ${actual} → ${expected}`);
        }

        return `
            <div class="warning-box" style="background: #fff3cd; border: 1px solid #ffc107; padding: 15px; border-radius: 5px; margin-bottom: 10px;">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                    <div>
                        <strong>⚠️ Группа "${escapeHTML(status.name)}" не синхронизирована</strong>
                        <div style="margin-top: 5px; color: #856404; font-size: 14px;">
                            Проблемы: ${escapeHTML(issues.join(', '))}
                        </div>
                    </div>
                    <button class="btn btn-warning" onclick="syncGroups('${escapeHTML(status.name)}')" style="white-space: nowrap;">
                        🔄 Синхронизировать
                    </button>
                </div>
            </div>
        `;
    }).join('');
}

// DNS records

let dnsRecords = [];

// Разбирает список IP-адресов, разделенных запятыми или пробелами.
function parseAddresses(value) {
    return value.split(/[\s,;]+/).map(v => v.trim()).filter(v => v !== '');
}

async function loadDNSRecords() {
    try {
        const response = await fetch('/api/dns-records');
        const data = await response.json();
        dnsRecords = data.records || [];
        displayDNSRecords(dnsRecords);
        checkDNSRecordsSync();
    } catch (error) {
        showMessage('Ошибка загрузки DNS-записей: ' + error.message, 'error');
    }
}

function displayDNSRecords(records) {
    const tbody = document.getElementById('dnsRecordsBody');

    if (records.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4" class="empty-state">Нет DNS-записей.</td></tr>';
        return;
    }

    tbody.innerHTML = records.map(record => `
        <tr>
            <td><strong>${escapeHTML(record.domain)}</strong></td>
            <td>${record.addresses.map(a => escapeHTML(a)).join('<br>')}</td>
            <td class="description-cell">${escapeHTML(record.description || '')}</td>
            <td class="actions-cell">
                <button class="btn btn-secondary" onclick="editDNSRecord('${escapeHTML(record.id)}')">Редактировать</button>
                <button class="btn btn-danger" onclick="deleteDNSRecord('${escapeHTML(record.id)}')">Удалить</button>
            </td>
        </tr>
    `).join('');
}

// Отправляет POST-запрос к API DNS-записей и возвращает ответ, бросая ошибку с текстом сервера.
async function postDNSRecords(path, body) {
    const response = await fetch(path, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify(body)
    });

    if (!response.ok) {
        throw new Error((await response.text()) || 'Ошибка запроса');
    }

    return response.json();
}

async function addDNSRecord(event) {
    event.preventDefault();

    const domain = document.getElementById('dnsRecordDomain').value.trim();
    const addresses = parseAddresses(document.getElementById('dnsRecordAddresses').value);
    const description = document.getElementById('dnsRecordDescription').value.trim();

    try {
        await postDNSRecords('/api/dns-records/add', { domain, addresses, description });

        showMessage('DNS-запись добавлена. Синхронизируйте ее в конфиг.', 'success');
        document.getElementById('dnsRecordDomain').value = '';
        document.getElementById('dnsRecordAddresses').value = '';
        document.getElementById('dnsRecordDescription').value = '';
        await loadDNSRecords();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

function editDNSRecord(id) {
    const record = dnsRecords.find(r => r.id === id);
    if (!record) return;

    const modal = `
        <div class="modal-overlay" onclick="closeEditModal(event)">
            <div class="modal" onclick="event.stopPropagation()">
                <h3>Редактировать DNS-запись</h3>
                <form onsubmit="submitEditDNSRecord(event, '${escapeHTML(id)}')">
                    <div class="form-group">
                        <label class="form-label" for="editDNSRecordDomain">Домен</label>
                        <input class="form-input" type="text" id="editDNSRecordDomain" value="${escapeHTML(record.domain)}" required>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editDNSRecordAddresses">IP-адреса</label>
                        <input class="form-input" type="text" id="editDNSRecordAddresses" value="${escapeHTML(record.addresses.join(', '))}" required>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editDNSRecordDescription">Описание</label>
                        <input class="form-input" type="text" id="editDNSRecordDescription" value="${escapeHTML(record.description || '')}">
                    </div>
                    <div class="form-actions">
                        <button type="button" class="btn btn-secondary" onclick="closeEditModal()">Отмена</button>
                        <button type="submit" class="btn btn-primary">Сохранить</button>
                    </div>
                </form>
            </div>
        </div>
    `;
    document.body.insertAdjacentHTML('beforeend', modal);
}

async function submitEditDNSRecord(event, id) {
    event.preventDefault();

    const domain = document.getElementById('editDNSRecordDomain').value.trim();
    const addresses = parseAddresses(document.getElementById('editDNSRecordAddresses').value);
    const description = document.getElementById('editDNSRecordDescription').value.trim();

    try {
        await postDNSRecords('/api/dns-records/edit', { id, domain, addresses, description });

        showMessage('DNS-запись обновлена. Синхронизируйте ее в конфиг.', 'success');
        closeEditModal();
        await loadDNSRecords();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function deleteDNSRecord(id) {
    const record = dnsRecords.find(r => r.id === id);

    if (!confirm(`Удалить DNS-запись "${record ? record.domain : id}"?`)) {
        return;
    }

    try {
        await postDNSRecords('/api/dns-records/delete', { id });

        showMessage('DNS-запись удалена. Синхронизируйте изменения в конфиг.', 'success');
        await loadDNSRecords();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function syncDNSRecords() {
    try {
        await postDNSRecords('/api/dns-records/sync', {});

        showMessage('DNS-записи синхронизированы во временный конфиг.', 'success');
        checkPendingConfig();
        checkDNSRecordsSync();

        if (document.getElementById('config-tab').classList.contains('active')) {
            await loadConfig();
        }
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function checkDNSRecordsSync() {
    try {
        const response = await fetch('/api/dns-records/check-sync');
        if (!response.ok) {
            console.error('Failed to check dns records sync');
            return;
        }

        const data = await response.json();
        const html = data.synced ? '' : `
            <div class="warning-box" style="background: #fff3cd; border: 1px solid #ffc107; padding: 15px; border-radius: 5px; margin-bottom: 20px;">
                <div style="display: flex; justify-content: space-between; align-items: center;">
                    <div>
                        <strong>⚠️ DNS-записи не синхронизированы</strong>
                        <div style="margin-top: 5px; color: #856404; font-size: 14px;">
                            Записи отличаются от конфига sing-box.
                        </div>
                    </div>
                    <button class="btn btn-warning" onclick="syncDNSRecords()" style="white-space: nowrap;">
                        🔄 Синхронизировать
                    </button>
                </div>
            </div>
        `;

        ['dnsRecordsSyncWarning', 'configDNSRecordsSyncWarning'].forEach(id => {
            const container = document.getElementById(id);

            if (container) {
                container.innerHTML = html;
            }
        });
    } catch (error) {
        console.error('Failed to check dns records sync:', error);
    }
}

// Rules

async function loadRules() {
    try {
        const response = await fetch('/api/rules');
        const data = await response.json();
        pendingCount = data.pending_count || 0;
        displayRules(data.rules || []);
        updateApplyButton();
        
        // Загружаем группы для выпадающего списка
        await loadGroupsForSelect();
    } catch (error) {
        showMessage('Ошибка загрузки правил: ' + error.message, 'error');
    }
}

function displayRules(rules) {
    const tbody = document.getElementById('rulesBody');

    if (rules.length === 0) {
        tbody.innerHTML = '<tr><td colspan="6" class="empty-state">Нет правил. Добавьте первое правило выше.</td></tr>';
        return;
    }

    tbody.innerHTML = rules.map(rule => `
        <tr class="${rule.deleted ? 'deleted' : (rule.applied ? '' : 'pending')}">
            <td>
                <span class="status-badge ${rule.deleted ? 'status-deleted' : (rule.applied ? 'status-applied' : 'status-pending')}">
                    ${rule.deleted ? 'К удалению' : (rule.applied ? 'Применено' : 'Ожидает')}
                </span>
            </td>
            <td>${groupBadge(rule)}</td>
            <td><span class="badge badge-${escapeHTML(rule.type)}">${getTypeLabel(rule.type)}</span></td>
            <td><code>${escapeHTML(rule.value)}</code></td>
            <td class="description-cell">${escapeHTML(rule.description || '')}</td>
            <td class="actions-cell">
                <button class="btn btn-secondary" onclick='editRule(${JSON.stringify(rule)})' ${rule.deleted ? 'disabled' : ''}>
                    Редактировать
                </button>
                <button class="btn btn-danger" onclick="deleteRule('${escapeHTML(rule.id)}')" ${rule.deleted ? 'disabled' : ''}>
                    ${rule.deleted ? 'Удалено' : 'Удалить'}
                </button>
            </td>
        </tr>
    `).join('');
}

// Бейдж группы; для исключений из туннелирования группа не используется.
function groupBadge(item) {
    if (item.bypass) {
        return '<span class="badge badge-bypass" title="sing-box не перехватывает этот трафик">мимо туннеля</span>';
    }

    return `<span class="badge badge-group">${escapeHTML(item.group || 'default')}</span>`;
}

// Чекбокс исключения из туннелирования для модальных окон редактирования.
function bypassCheckbox(id, checked) {
    return `
                    <div class="form-group">
                        <label style="display: flex; align-items: center; gap: 8px; cursor: pointer;">
                            <input type="checkbox" id="${id}" ${checked ? 'checked' : ''} style="cursor: pointer;">
                            <span>Мимо туннеля (sing-box не будет перехватывать трафик, группа не используется)</span>
                        </label>
                    </div>`;
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

    const bulkMode = document.getElementById('bulkModeToggle').checked;

    if (bulkMode) {
        await addRuleBulk(event);
    } else {
        await addRuleSingle(event);
    }
}

async function addRuleSingle(event) {
    const type = document.getElementById('ruleType').value;
    const value = document.getElementById('ruleValue').value;
    const description = document.getElementById('ruleDescription').value;
    const group = document.getElementById('ruleGroup').value;
    const bypass = document.getElementById('ruleBypass').checked;

    try {
        const response = await fetch('/api/rules/add', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ type, value, description, group, bypass })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при добавлении правила');
        }

        showMessage('Правило успешно добавлено', 'success');
        document.getElementById('ruleValue').value = '';
        document.getElementById('ruleDescription').value = '';
        await loadRules();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function addRuleBulk(event) {
    const type = document.getElementById('ruleType').value;
    const values = document.getElementById('ruleBulkValues').value;
    const description = document.getElementById('ruleDescription').value;
    const group = document.getElementById('ruleGroup').value;
    const bypass = document.getElementById('ruleBypass').checked;

    if (!values.trim()) {
        showMessage('Введите хотя бы одно значение', 'error');
        return;
    }

    try {
        const response = await fetch('/api/rules/add-bulk', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ type, values, description, group, bypass })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при массовом добавлении');
        }

        const result = await response.json();

        let message = `Добавлено: ${result.success} из ${result.total}`;

        if (result.failed > 0) {
            message += ` (ошибок: ${result.failed})`;
            
            const errorDetails = result.failed_values
                .slice(0, 3)
                .map(f => `${f.value}: ${f.error}`)
                .join('\n');

            if (result.failed > 3) {
                message += `\n\nПервые ошибки:\n${errorDetails}\n... и ещё ${result.failed - 3}`;
            } else {
                message += `\n\nОшибки:\n${errorDetails}`;
            }
        }

        showMessage(message, result.failed > 0 ? 'warning' : 'success');
        document.getElementById('ruleBulkValues').value = '';
        document.getElementById('ruleDescription').value = '';
        await loadRules();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

function toggleBulkMode() {
    const bulkMode = document.getElementById('bulkModeToggle').checked;
    const singleValueRow = document.getElementById('singleValueRow');
    const bulkValuesRow = document.getElementById('bulkValuesRow');
    const ruleValue = document.getElementById('ruleValue');
    const ruleBulkValues = document.getElementById('ruleBulkValues');

    if (bulkMode) {
        singleValueRow.style.display = 'none';
        bulkValuesRow.style.display = 'flex';
        ruleValue.required = false;
        ruleBulkValues.required = true;
    } else {
        singleValueRow.style.display = 'flex';
        bulkValuesRow.style.display = 'none';
        ruleValue.required = true;
        ruleBulkValues.required = false;
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

async function editRule(rule) {
    const modal = `
        <div class="modal-overlay" onclick="closeEditModal(event)">
            <div class="modal" onclick="event.stopPropagation()">
                <h3>Редактировать правило</h3>
                <form id="editRuleForm" onsubmit="submitEditRule(event, '${escapeHTML(rule.id)}')">
                    <div class="form-group">
                        <label class="form-label">Тип</label>
                        <input class="form-input" type="text" value="${getTypeLabel(rule.type)}" disabled>
                    </div>
                    <div class="form-group">
                        <label class="form-label">Значение</label>
                        <input class="form-input" type="text" value="${escapeHTML(rule.value)}" disabled>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editRuleGroup">Группа</label>
                        <select class="form-select" id="editRuleGroup" required>
                            ${groups.map(g => 
                                `<option value="${escapeHTML(g.name)}" ${g.name === rule.group ? 'selected' : ''}>${escapeHTML(g.name)}</option>`
                            ).join('')}
                        </select>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editRuleDescription">Описание</label>
                        <textarea class="form-textarea" id="editRuleDescription">${escapeHTML(rule.description || '')}</textarea>
                    </div>
                    ${bypassCheckbox('editRuleBypass', rule.bypass)}
                    <div class="form-actions">
                        <button type="button" class="btn btn-secondary" onclick="closeEditModal()">Отмена</button>
                        <button type="submit" class="btn btn-primary">Сохранить</button>
                    </div>
                </form>
            </div>
        </div>
    `;
    document.body.insertAdjacentHTML('beforeend', modal);
}

async function submitEditRule(event, id) {
    event.preventDefault();

    const group = document.getElementById('editRuleGroup').value;
    const description = document.getElementById('editRuleDescription').value;
    const bypass = document.getElementById('editRuleBypass').checked;

    try {
        const response = await fetch('/api/rules/edit', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ 
                id,
                group,
                description,
                bypass
            })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при редактировании правила');
        }

        showMessage('Правило успешно обновлено', 'success');
        closeEditModal();
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
        
        // Загружаем группы для выпадающего списка
        await loadGroupsForSelect();
    } catch (error) {
        showMessage('Ошибка загрузки URL источников: ' + error.message, 'error');
    }
}

function displayURLSources(sources) {
    const tbody = document.getElementById('urlSourcesBody');

    if (sources.length === 0) {
        tbody.innerHTML = '<tr><td colspan="8" class="empty-state">Нет URL источников. Добавьте первый источник выше.</td></tr>';
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
                    ${source.last_error ? `<div class="source-error" title="${escapeHTML(source.last_error)}">${escapeHTML(source.last_error)}</div>` : ''}
                </td>
                <td>${groupBadge(source)}</td>
                <td class="url-cell" title="${url}">${url}</td>
                <td class="description-cell">${escapeHTML(source.description || '')}</td>
                <td>${escapeHTML(source.interval)} мин</td>
                <td class="date-cell">${lastUpdate}</td>
                <td>${source.items_count || 0}</td>
                <td class="actions-cell">
                    <button class="btn btn-secondary" onclick="refreshURLSource('${id}')" title="Загрузить список сейчас, не дожидаясь интервала" ${!source.applied || source.deleted || refreshingURLSources.has(source.id) ? 'disabled' : ''}>
                        ${refreshingURLSources.has(source.id) ? 'Загрузка...' : 'Обновить'}
                    </button>
                    <button class="btn btn-info" onclick="viewURLSourceRules('${id}')" ${!source.applied ? 'disabled' : ''}>
                        Посмотреть
                    </button>
                    <button class="btn btn-secondary" onclick='editURLSource(${JSON.stringify(source)})' ${source.deleted ? 'disabled' : ''}>
                        Редактировать
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
    const group = document.getElementById('urlSourceGroup').value;
    const bypass = document.getElementById('urlSourceBypass').checked;

    try {
        const response = await fetch('/api/url-sources/add', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ url, interval, description, group, bypass })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при добавлении URL источника');
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

// ID источников, которые сейчас загружаются вручную (список перерисовывается каждые 5 секунд).
const refreshingURLSources = new Set();

async function refreshURLSource(id) {
    refreshingURLSources.add(id);
    await loadURLSources();

    try {
        const response = await fetch('/api/url-sources/refresh', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ id })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) {
            throw new Error(result.error || `HTTP ${response.status}`);
        }

        showMessage('Источник загружен', 'success');
    } catch (error) {
        showMessage('Ошибка загрузки источника: ' + error.message, 'error');
    } finally {
        refreshingURLSources.delete(id);
        await loadURLSources();
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

async function editURLSource(source) {
    const modal = `
        <div class="modal-overlay" onclick="closeEditModal(event)">
            <div class="modal" onclick="event.stopPropagation()">
                <h3>Редактировать URL источник</h3>
                <form id="editURLSourceForm" onsubmit="submitEditURLSource(event, '${escapeHTML(source.id)}')">
                    <div class="form-group">
                        <label class="form-label">URL</label>
                        <input class="form-input" type="text" value="${escapeHTML(source.url)}" disabled>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editURLSourceGroup">Группа</label>
                        <select class="form-select" id="editURLSourceGroup" required>
                            ${groups.map(g => 
                                `<option value="${escapeHTML(g.name)}" ${g.name === source.group ? 'selected' : ''}>${escapeHTML(g.name)}</option>`
                            ).join('')}
                        </select>
                    </div>
                    <div class="form-group">
                        <label class="form-label" for="editURLSourceDescription">Описание</label>
                        <textarea class="form-textarea" id="editURLSourceDescription">${escapeHTML(source.description || '')}</textarea>
                    </div>
                    ${bypassCheckbox('editURLSourceBypass', source.bypass)}
                    <div class="form-actions">
                        <button type="button" class="btn btn-secondary" onclick="closeEditModal()">Отмена</button>
                        <button type="submit" class="btn btn-primary">Сохранить</button>
                    </div>
                </form>
            </div>
        </div>
    `;
    document.body.insertAdjacentHTML('beforeend', modal);
}

async function submitEditURLSource(event, id) {
    event.preventDefault();

    const group = document.getElementById('editURLSourceGroup').value;
    const description = document.getElementById('editURLSourceDescription').value;
    const bypass = document.getElementById('editURLSourceBypass').checked;

    try {
        const response = await fetch('/api/url-sources/edit', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ 
                id,
                group,
                description,
                bypass
            })
        });

        if (!response.ok) {
            const errorData = await response.text();
            throw new Error(errorData || 'Ошибка при редактировании URL источника');
        }

        showMessage('URL источник успешно обновлен', 'success');
        closeEditModal();
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
        const data = await response.json().catch(() => ({}));

        if (!response.ok) {
            throw new Error(data.error || `HTTP ${response.status}`);
        }

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
    const original = btn ? btn.innerHTML : '';

    if (btn) {
        btn.disabled = true;
        btn.innerHTML = ICON_RESTART + 'Перезагрузка...';
    }

    setLoading(document.getElementById('clashPanel'), true);

    try {
        const response = await fetch('/api/control/reload', {
            method: 'POST'
        });

        const data = await response.json();

        if (!response.ok || !data.success) {
            throw new Error(data.error || 'Ошибка перезагрузки');
        }

        showMessage(data.message || 'Sing-Box успешно перезагружен', 'success');
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        setLoading(document.getElementById('clashPanel'), false);

        if (btn) {
            btn.disabled = false;
            btn.innerHTML = original;
        }
    }
}

// Обзор — сводка и графики
//
// Цвета серий заданы в style.css (--series-down / --series-up) и проверены
// валидатором палитры на тёмной поверхности #0d1117: CVD ΔE 29.3,
// обычное зрение ΔE 33.5. Обе серии живут на одной оси Y (одна единица, B/s).

const TRAFFIC_CAPACITY = 60;
const MEMORY_CAPACITY = 60;

const overviewState = {
    traffic: [],
    memory: [],
    memoryPeak: 0,
    // Храним позицию курсора, а не индекс: окно сдвигается каждую секунду,
    // и крестовина должна оставаться под указателем.
    hoverX: null,
    hoverIndex: null,
    tableOpen: false,
    geometry: null
};

function splitBytes(bytes) {
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let value = Number(bytes) || 0;
    let unit = 0;

    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit++;
    }

    let text;
    if (unit === 0 || value >= 100) {
        text = Math.round(value).toString();
    } else {
        text = value.toFixed(value >= 10 ? 1 : 2).replace(/\.?0+$/, '');
    }

    return { value: text, unit: units[unit] };
}

function formatRate(bytesPerSecond) {
    const parts = splitBytes(bytesPerSecond);
    return `${parts.value} ${parts.unit}/s`;
}

function setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
}

async function loadOverview() {
    try {
        const response = await fetch('/api/clash/overview');

        if (!response.ok) {
            const text = await response.text();
            throw new Error(text.trim() || ('HTTP ' + response.status));
        }

        applyOverview(await response.json());
    } catch (error) {
        // Секундный поллинг: не спамим тостами, показываем статус в плитке
        // и оставляем предыдущий кадр графика на месте.
        const dot = document.getElementById('ovStreamDot');
        if (dot) dot.className = 'stat-dot is-bad';
        setText('ovConnBreakdown', 'Clash API недоступен');
    }
}

function applyOverview(data) {
    const down = splitBytes(data.downloadTotal);
    setText('ovDownTotal', down.value);
    setText('ovDownTotalUnit', down.unit);

    const up = splitBytes(data.uploadTotal);
    setText('ovUpTotal', up.value);
    setText('ovUpTotalUnit', up.unit);

    const connections = data.connections || {};
    setText('ovConnTotal', connections.total ?? 0);
    setText('ovConnBreakdown', `TCP ${connections.tcp ?? 0} · UDP ${connections.udp ?? 0}`);

    const dot = document.getElementById('ovStreamDot');
    if (dot) dot.className = 'stat-dot ' + (data.trafficStreamOk ? 'is-good' : 'is-warn');

    const memory = Number(data.memory) || 0;
    const memoryParts = splitBytes(memory);
    setText('ovMemory', memoryParts.value);
    setText('ovMemoryUnit', memoryParts.unit);

    overviewState.memory.push(memory);
    if (overviewState.memory.length > MEMORY_CAPACITY) {
        overviewState.memory.shift();
    }

    if (memory > overviewState.memoryPeak) {
        overviewState.memoryPeak = memory;
    }

    const peak = splitBytes(overviewState.memoryPeak);
    setText('ovMemPeak', `${peak.value} ${peak.unit}`);
    setText('ovRules', data.rules ?? 0);
    setText('ovVersion', data.version || '—');

    overviewState.traffic = Array.isArray(data.traffic) ? data.traffic : [];

    const latest = overviewState.traffic[overviewState.traffic.length - 1];
    setText('ovDownRate', latest ? formatRate(latest.down) : '—');
    setText('ovUpRate', latest ? formatRate(latest.up) : '—');

    const peakDown = Math.max(0, ...overviewState.traffic.map(s => s.down));
    const peakUp = Math.max(0, ...overviewState.traffic.map(s => s.up));
    setText('ovPeakRate', overviewState.traffic.length
        ? `Пик за окно: ${formatRate(peakDown)} ↓ · ${formatRate(peakUp)} ↑`
        : '');

    renderTrafficChart();
    renderMemorySpark();

    // Окно сдвинулось — под курсором теперь другое измерение.
    if (overviewState.hoverX !== null) {
        const wrap = document.getElementById('ovChartWrap');
        if (wrap) showChartTooltip(wrap.getBoundingClientRect());
    }

    if (overviewState.tableOpen) {
        renderTrafficTable();
    }
}

// Монотонная кубическая интерполяция (Фрич — Карлсон): сглаживает линию,
// но по построению не выходит за пределы соседних значений.
function monotonePath(points) {
    const n = points.length;
    if (n === 0) return '';
    if (n === 1) return `M${points[0].x},${points[0].y}`;

    const dx = [];
    const slope = [];

    for (let i = 0; i < n - 1; i++) {
        dx[i] = points[i + 1].x - points[i].x;
        slope[i] = (points[i + 1].y - points[i].y) / dx[i];
    }

    const tangent = new Array(n);
    tangent[0] = slope[0];
    tangent[n - 1] = slope[n - 2];

    for (let i = 1; i < n - 1; i++) {
        if (slope[i - 1] * slope[i] <= 0) {
            tangent[i] = 0;
        } else {
            const w1 = 2 * dx[i] + dx[i - 1];
            const w2 = dx[i] + 2 * dx[i - 1];
            tangent[i] = (w1 + w2) / (w1 / slope[i - 1] + w2 / slope[i]);
        }
    }

    const r = v => Math.round(v * 100) / 100;
    let d = `M${r(points[0].x)},${r(points[0].y)}`;

    for (let i = 0; i < n - 1; i++) {
        const c1x = points[i].x + dx[i] / 3;
        const c1y = points[i].y + tangent[i] * dx[i] / 3;
        const c2x = points[i + 1].x - dx[i] / 3;
        const c2y = points[i + 1].y - tangent[i + 1] * dx[i] / 3;

        d += `C${r(c1x)},${r(c1y)} ${r(c2x)},${r(c2y)} ${r(points[i + 1].x)},${r(points[i + 1].y)}`;
    }

    return d;
}

// Округляет верх шкалы вверх до круглого числа в единице отображения
// (шаг по 1024), иначе подписи оси выглядят как «9.54 MB/s».
const NICE_STEPS = [1, 2, 2.5, 5, 10, 20, 25, 50, 100, 200, 250, 500, 1024];

function niceMax(value) {
    if (value <= 0) return 1024;

    let scale = 1;
    while (value / scale >= 1024) {
        scale *= 1024;
    }

    const normalized = value / scale;
    const step = NICE_STEPS.find(candidate => candidate >= normalized) || 1024;

    return step * scale;
}

function renderTrafficChart() {
    const svg = document.getElementById('ovChart');
    const wrap = document.getElementById('ovChartWrap');
    if (!svg || !wrap) return;

    const width = wrap.clientWidth;
    const height = wrap.clientHeight;
    if (width <= 0 || height <= 0) return;

    const padL = 58;
    const padR = 14;
    const padT = 12;
    const padB = 22;
    const plotW = width - padL - padR;
    const plotH = height - padT - padB;

    svg.setAttribute('width', width);
    svg.setAttribute('height', height);
    svg.setAttribute('viewBox', `0 0 ${width} ${height}`);

    const samples = overviewState.traffic;
    const n = samples.length;

    const observed = Math.max(0, ...samples.map(s => Math.max(s.down, s.up)));
    const max = niceMax(observed);

    const stepX = plotW / (TRAFFIC_CAPACITY - 1);
    const xs = [];
    for (let i = 0; i < n; i++) {
        xs.push(padL + plotW - (n - 1 - i) * stepX);
    }

    const yAt = v => padT + plotH * (1 - Math.min(v, max) / max);

    overviewState.geometry = { xs, padL, padT, plotW, plotH, stepX };

    // Сетка и подписи оси Y — сплошные хайрлайны, утопленные в фон.
    let grid = '';
    const ticks = 4;
    for (let i = 0; i <= ticks; i++) {
        const value = max * (1 - i / ticks);
        const y = padT + (plotH * i) / ticks;

        grid += `<line class="chart-grid" x1="${padL}" y1="${y.toFixed(1)}" x2="${padL + plotW}" y2="${y.toFixed(1)}"/>`;
        grid += `<text class="chart-tick" x="${padL - 8}" y="${(y + 3.5).toFixed(1)}" text-anchor="end">${escapeHTML(formatRate(value))}</text>`;
    }

    let xLabels = '';
    if (n > 0) {
        const marks = [{ at: 0, text: '−60 с' }, { at: 0.5, text: '−30 с' }, { at: 1, text: 'сейчас' }];
        marks.forEach(mark => {
            const x = padL + plotW * mark.at;
            const anchor = mark.at === 0 ? 'start' : mark.at === 1 ? 'end' : 'middle';
            xLabels += `<text class="chart-tick" x="${x.toFixed(1)}" y="${(padT + plotH + 15).toFixed(1)}" text-anchor="${anchor}">${mark.text}</text>`;
        });
    }

    let series = '';
    if (n >= 1) {
        [['up', s => s.up], ['down', s => s.down]].forEach(([name, pick]) => {
            const points = samples.map((s, i) => ({ x: xs[i], y: yAt(pick(s)) }));
            const line = monotonePath(points);
            const baseline = padT + plotH;
            const area = `${line}L${xs[n - 1].toFixed(2)},${baseline}L${xs[0].toFixed(2)},${baseline}Z`;

            series += `<path class="chart-area" data-series="${name}" d="${area}"/>`;
            series += `<path class="chart-line" data-series="${name}" d="${line}"/>`;
        });
    }

    overviewState.hoverIndex = nearestSampleIndex(overviewState.hoverX, xs);

    let hover = '';
    const idx = overviewState.hoverIndex;
    if (idx !== null) {
        const x = xs[idx].toFixed(2);
        hover += `<line class="chart-crosshair" x1="${x}" y1="${padT}" x2="${x}" y2="${padT + plotH}"/>`;

        [['up', samples[idx].up], ['down', samples[idx].down]].forEach(([name, value]) => {
            hover += `<circle class="chart-dot" data-series="${name}" cx="${x}" cy="${yAt(value).toFixed(2)}" r="4"/>`;
        });
    }

    svg.innerHTML = grid + xLabels + series + hover;

    if (n === 0) {
        svg.innerHTML += `<text class="chart-empty" x="${(padL + plotW / 2).toFixed(1)}" y="${(padT + plotH / 2).toFixed(1)}" text-anchor="middle">Ожидание данных от Clash API…</text>`;
    }
}

function nearestSampleIndex(x, xs) {
    if (x === null || xs.length === 0) return null;

    let nearest = 0;
    let best = Infinity;

    xs.forEach((position, i) => {
        const distance = Math.abs(position - x);
        if (distance < best) {
            best = distance;
            nearest = i;
        }
    });

    return nearest;
}

function handleChartPointer(event) {
    const wrap = document.getElementById('ovChartWrap');
    if (!wrap) return;

    const rect = wrap.getBoundingClientRect();

    overviewState.hoverX = event.clientX - rect.left;
    renderTrafficChart();
    showChartTooltip(rect);
}

function showChartTooltip(rect) {
    const tooltip = document.getElementById('ovTooltip');
    const geometry = overviewState.geometry;
    const index = overviewState.hoverIndex;
    const sample = index === null ? null : overviewState.traffic[index];

    if (!tooltip || !geometry || !sample) return;

    const secondsAgo = overviewState.traffic.length - 1 - index;

    tooltip.textContent = '';

    const time = document.createElement('div');
    time.className = 'tt-time';
    time.textContent = secondsAgo === 0 ? 'сейчас' : `${secondsAgo} с назад`;
    tooltip.appendChild(time);

    [['down', 'Загрузка', sample.down], ['up', 'Отдача', sample.up]].forEach(([series, name, value]) => {
        const row = document.createElement('div');
        row.className = 'tt-row';

        const key = document.createElement('span');
        key.className = 'tt-key';
        key.dataset.series = series;

        const amount = document.createElement('span');
        amount.className = 'tt-value';
        amount.textContent = formatRate(value);

        const label = document.createElement('span');
        label.className = 'tt-name';
        label.textContent = name;

        row.append(key, amount, label);
        tooltip.appendChild(row);
    });

    tooltip.hidden = false;

    const x = geometry.xs[index];
    const flip = x > rect.width / 2;
    tooltip.style.left = `${flip ? x - 12 : x + 12}px`;
    tooltip.style.transform = flip ? 'translateX(-100%)' : 'none';
}

function hideChartTooltip() {
    const tooltip = document.getElementById('ovTooltip');
    if (tooltip) tooltip.hidden = true;

    if (overviewState.hoverX !== null) {
        overviewState.hoverX = null;
        renderTrafficChart();
    }
}

function renderMemorySpark() {
    const svg = document.getElementById('ovMemChart');
    if (!svg || !svg.parentElement) return;

    const width = svg.parentElement.clientWidth;
    const height = svg.parentElement.clientHeight;
    if (width <= 0 || height <= 0) return;

    svg.setAttribute('width', width);
    svg.setAttribute('height', height);
    svg.setAttribute('viewBox', `0 0 ${width} ${height}`);

    const values = overviewState.memory;
    const n = values.length;

    if (n < 2) {
        svg.innerHTML = '';
        return;
    }

    const padT = 6;
    const plotH = height - padT - 2;
    const stepX = width / (MEMORY_CAPACITY - 1);

    // Шкала от нуля исказила бы почти плоский ряд, поэтому берём окно
    // вокруг наблюдаемого диапазона с запасом.
    const min = Math.min(...values);
    const max = Math.max(...values);
    const span = Math.max(max - min, max * 0.05, 1);
    const top = max + span * 0.25;
    const bottom = Math.max(0, min - span * 0.25);

    const points = values.map((value, i) => ({
        x: width - (n - 1 - i) * stepX,
        y: padT + plotH * (1 - (value - bottom) / (top - bottom))
    }));

    const line = monotonePath(points);
    const area = `${line}L${points[n - 1].x.toFixed(2)},${padT + plotH}L${points[0].x.toFixed(2)},${padT + plotH}Z`;

    svg.innerHTML =
        `<path class="chart-area" data-series="down" d="${area}"/>` +
        `<path class="chart-line" data-series="down" d="${line}"/>`;
}

function toggleTrafficTable() {
    const table = document.getElementById('ovTrafficTable');
    const toggle = document.getElementById('ovTableToggle');
    if (!table || !toggle) return;

    overviewState.tableOpen = !overviewState.tableOpen;
    table.hidden = !overviewState.tableOpen;
    toggle.textContent = overviewState.tableOpen ? 'Скрыть таблицу' : 'Показать таблицу';

    if (overviewState.tableOpen) {
        renderTrafficTable();
    }
}

// Табличный двойник графика: те же значения без наведения мышью.
function renderTrafficTable() {
    const container = document.getElementById('ovTrafficTable');
    if (!container) return;

    const samples = overviewState.traffic;

    if (samples.length === 0) {
        container.innerHTML = '<div class="empty-state">Нет данных.</div>';
        return;
    }

    const rows = samples
        .map((sample, i) => {
            const secondsAgo = samples.length - 1 - i;
            const when = secondsAgo === 0 ? 'сейчас' : `−${secondsAgo} с`;

            return `<tr><td>${when}</td><td>${escapeHTML(formatRate(sample.down))}</td><td>${escapeHTML(formatRate(sample.up))}</td></tr>`;
        })
        .reverse()
        .join('');

    container.innerHTML = `
        <table class="table">
            <thead>
                <tr><th>Время</th><th>Загрузка</th><th>Отдача</th></tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;
}

// Clash API — прокси-группы и задержки

// Типы прокси-групп, которые показываем во вкладке "Управление".
const CLASH_GROUP_TYPES = ['Selector', 'URLTest', 'LoadBalance', 'Fallback'];
// GLOBAL — служебный selector sing-box, для этого проекта default outbound
// задаётся в конфиге напрямую, поэтому группу не показываем.
const CLASH_HIDDEN_GROUPS = ['GLOBAL'];

const ICON_SPEEDOMETER = '<svg class="btn-icon" viewBox="0 0 24 24"><path d="M12 16a3 3 0 0 1-3-3c0-1.12.61-2.1 1.5-2.61l9.71-5.62-5.53 9.58c-.5.98-1.51 1.65-2.68 1.65m0-13c1.81 0 3.5.5 4.97 1.32l-2.1 1.21C14 5.19 13 5 12 5a8 8 0 0 0-8 8c0 2.21.89 4.21 2.34 5.65h.01c.39.39.39 1.02 0 1.41-.39.39-1.03.39-1.42.01C3.12 18.26 2 15.76 2 13A10 10 0 0 1 12 3m10 10c0 2.76-1.12 5.26-2.93 7.07-.39.38-1.02.38-1.41-.01a.996.996 0 0 1 0-1.41A7.95 7.95 0 0 0 20 13c0-1-.19-2-.54-2.9l1.21-2.1C21.5 9.5 22 11.19 22 13Z"/></svg>';
const ICON_RESTART = '<svg class="btn-icon" viewBox="0 0 24 24"><path d="M12,4C14.1,4 16.1,4.8 17.6,6.3C20.7,9.4 20.7,14.5 17.6,17.6C15.8,19.5 13.3,20.2 10.9,19.9L11.4,17.9C13.1,18.1 14.9,17.5 16.2,16.2C18.5,13.9 18.5,10.1 16.2,7.7C15.1,6.6 13.5,6 12,6V10.6L7,5.6L12,0.6V4M6.3,17.6C3.7,15 3.3,11 5.1,7.9L6.6,9.4C5.5,11.6 5.9,14.4 7.8,16.2C8.3,16.7 8.9,17.1 9.6,17.4L9,19.4C8,19 7.1,18.4 6.3,17.6Z"/></svg>';

let clashProxiesCache = {};
// Счётчик операций в полёте: пока он > 0, фоновое обновление не трогает DOM.
let clashBusy = 0;

function setLoading(el, on) {
    if (el) el.classList.toggle('is-loading', on);
}

function clashTestUrl() {
    const input = document.getElementById('clashTestUrl');
    const value = input ? input.value.trim() : '';
    return value || 'http://www.gstatic.com/generate_204';
}

function delayClass(delay) {
    if (delay === undefined || delay === null) return '';
    if (delay <= 0) return 'd-bad';
    if (delay < 200) return 'd-good';
    if (delay < 400) return 'd-ok';
    if (delay < 800) return 'd-slow';
    return 'd-bad';
}

function delayText(delay) {
    if (delay === undefined || delay === null) return '—';
    if (delay <= 0) return 'timeout';
    return delay + ' ms';
}

function lastDelay(proxy) {
    if (!proxy || !proxy.history || proxy.history.length === 0) return undefined;
    return proxy.history[proxy.history.length - 1].delay;
}

function clashGroups() {
    return Object.values(clashProxiesCache)
        .filter(p => CLASH_GROUP_TYPES.includes(p.type) && !CLASH_HIDDEN_GROUPS.includes(p.name))
        .sort((a, b) => a.name.localeCompare(b.name));
}

async function fetchClashProxies() {
    const response = await fetch('/api/clash/proxies');

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text.trim() || ('HTTP ' + response.status));
    }

    const data = await response.json();
    return data.proxies || {};
}

// Сохраняет уже замеренные задержки, если sing-box вернул пустую историю.
function mergeClashProxies(fresh) {
    Object.values(fresh).forEach(proxy => {
        const cached = clashProxiesCache[proxy.name];
        const hasFresh = proxy.history && proxy.history.length > 0;
        const hasCached = cached && cached.history && cached.history.length > 0;

        if (!hasFresh && hasCached) {
            proxy.history = cached.history;
        }
    });

    clashProxiesCache = fresh;
}

async function loadClashProxies() {
    const container = document.getElementById('clashGroupsContainer');
    if (!container) return;

    const panel = document.getElementById('clashPanel');
    setLoading(panel, true);
    clashBusy++;

    try {
        mergeClashProxies(await fetchClashProxies());
        renderClashGroups();
    } catch (error) {
        container.innerHTML = `<div class="empty-state">Не удалось получить данные Clash API: ${escapeHTML(error.message)}</div>`;
    } finally {
        clashBusy--;
        setLoading(panel, false);
    }
}

// Фоновое обновление: тихо подтягивает актуальные выборы и задержки.
async function refreshClashProxies() {
    if (clashBusy > 0) return;

    try {
        mergeClashProxies(await fetchClashProxies());

        if (clashBusy === 0) {
            renderClashGroups();
        }
    } catch (error) {
        // Тихо игнорируем: следующая итерация повторит запрос.
    }
}

function renderClashGroups() {
    const container = document.getElementById('clashGroupsContainer');
    if (!container) return;

    const proxies = clashProxiesCache;
    const groups = clashGroups();

    if (groups.length === 0) {
        container.innerHTML = '<div class="empty-state">Прокси-группы не найдены.</div>';
        return;
    }

    container.innerHTML = groups.map(group => {
        const selectable = group.type === 'Selector';
        const groupAttr = escapeHTML(group.name);
        const nowDelay = lastDelay(proxies[group.now]);

        const members = (group.all || []).map(memberName => {
            const member = proxies[memberName] || { name: memberName };
            const delay = lastDelay(member);
            const isActive = memberName === group.now;
            const nameAttr = escapeHTML(memberName);

            return `
                <div class="proxy-node${isActive ? ' active' : ''}${selectable ? ' selectable' : ' readonly'}"
                     ${selectable ? `onclick="selectClashProxy('${groupAttr}', '${nameAttr}')"` : ''}>
                    <div class="proxy-node-row">
                        <span class="proxy-node-name" title="${nameAttr}">${nameAttr}</span>
                        ${member.udp ? '<span class="proxy-udp">UDP</span>' : ''}
                    </div>
                    <div class="proxy-node-row">
                        <span class="proxy-node-type">${escapeHTML(member.type || '—')}</span>
                        <span class="proxy-delay ${delayClass(delay)}">${delayText(delay)}</span>
                    </div>
                </div>
            `;
        }).join('');

        return `
            <div class="clash-group has-progress">
                <div class="progress-line"></div>
                <div class="clash-group-head">
                    <div class="clash-group-meta">
                        <div class="clash-group-title">
                            <strong>${groupAttr}</strong>
                            <span class="clash-type">${escapeHTML(group.type)}</span>
                        </div>
                        <div class="clash-group-now">Активен: ${escapeHTML(group.now || '—')}</div>
                    </div>
                    <div class="clash-group-actions">
                        <span class="proxy-delay ${delayClass(nowDelay)}">${delayText(nowDelay)}</span>
                        <button class="btn btn-secondary btn-sm" onclick="testClashGroupDelay('${groupAttr}', this)">
                            ${ICON_SPEEDOMETER}
                            Проверить
                        </button>
                    </div>
                </div>
                <div class="proxy-grid">${members}</div>
            </div>
        `;
    }).join('');
}

async function selectClashProxy(group, name) {
    try {
        const response = await fetch('/api/clash/proxies/select', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ group, name })
        });

        if (!response.ok) {
            const text = await response.text();
            throw new Error(text || ('HTTP ' + response.status));
        }

        if (clashProxiesCache[group]) {
            clashProxiesCache[group].now = name;
            renderClashGroups();
        }
        showMessage(`${group}: выбран ${name}`, 'success');
    } catch (error) {
        showMessage('Ошибка переключения: ' + error.message, 'error');
    }
}

function applyDelays(delays) {
    Object.entries(delays).forEach(([name, delay]) => {
        if (!clashProxiesCache[name]) clashProxiesCache[name] = { name, history: [] };
        if (!clashProxiesCache[name].history) clashProxiesCache[name].history = [];
        clashProxiesCache[name].history.push({ delay });
    });
}

async function fetchGroupDelay(group) {
    const url = encodeURIComponent(clashTestUrl());
    const response = await fetch(`/api/clash/group/delay?group=${encodeURIComponent(group)}&url=${url}`);

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text.trim() || ('HTTP ' + response.status));
    }

    const data = await response.json();
    return data.delays || {};
}

async function testClashGroupDelay(group, btn) {
    const card = btn ? btn.closest('.clash-group') : null;

    setLoading(card, true);
    if (btn) btn.disabled = true;
    clashBusy++;

    try {
        const delays = await fetchGroupDelay(group);
        applyDelays(delays);
        showMessage(`${group}: проверено узлов — ${Object.keys(delays).length}`, 'success');
    } catch (error) {
        showMessage(`Ошибка замера задержки (${group}): ` + error.message, 'error');
    } finally {
        clashBusy--;
        // Перерисовка пересоздаёт карточку, поэтому индикатор снимать не нужно.
        renderClashGroups();
    }
}

async function testAllClashGroups() {
    const btn = document.getElementById('clashTestAllBtn');
    const panel = document.getElementById('clashPanel');
    const groups = clashGroups();

    if (groups.length === 0) return;

    setLoading(panel, true);
    if (btn) btn.disabled = true;
    clashBusy++;

    let ok = 0;
    const failed = [];

    for (const group of groups) {
        try {
            applyDelays(await fetchGroupDelay(group.name));
            renderClashGroups();
            ok++;
        } catch (error) {
            failed.push(group.name);
        }
    }

    clashBusy--;
    setLoading(panel, false);
    if (btn) btn.disabled = false;

    if (failed.length === 0) {
        showMessage(`Задержка замерена: групп — ${ok}`, 'success');
    } else {
        showMessage(`Замер завершён: успешно ${ok}, с ошибкой ${failed.join(', ')}`, 'error');
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

function highlightCode(code) {
    const lines = code.split('\n');
    let html = '';
    let inBlock = false;
    
    for (const line of lines) {
        const result = tokenizeLine(line, inBlock);
        html += result.html + '\n';
        inBlock = result.inBlock;
    }
    
    return html;
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

// Вычисляет Myers diff между двумя массивами строк.
// Возвращает массив операций: { type: 'equal'|'insert'|'delete', oldIndex, newIndex }
function computeMyersDiff(oldLines, newLines) {
    const m = oldLines.length;
    const n = newLines.length;
    const max = m + n;
    const v = {};
    const trace = [];
    
    v[1] = 0;
    
    for (let d = 0; d <= max; d++) {
        trace.push({ ...v });
        
        for (let k = -d; k <= d; k += 2) {
            let x;
            
            if (k === -d || (k !== d && v[k - 1] < v[k + 1])) {
                x = v[k + 1];
            } else {
                x = v[k - 1] + 1;
            }
            
            let y = x - k;
            
            while (x < m && y < n && oldLines[x] === newLines[y]) {
                x++;
                y++;
            }
            
            v[k] = x;
            
            if (x >= m && y >= n) {
                // Найден путь, восстанавливаем diff.
                return backtrackMyersDiff(oldLines, newLines, trace, m, n);
            }
        }
    }
    
    return [];
}

function backtrackMyersDiff(oldLines, newLines, trace, m, n) {
    const diff = [];
    let x = m;
    let y = n;
    
    for (let d = trace.length - 1; d >= 0; d--) {
        const v = trace[d];
        const k = x - y;
        
        let prevK;
        if (k === -d || (k !== d && v[k - 1] < v[k + 1])) {
            prevK = k + 1;
        } else {
            prevK = k - 1;
        }
        
        const prevX = v[prevK];
        const prevY = prevX - prevK;
        
        while (x > prevX && y > prevY) {
            diff.push({ type: 'equal', oldIndex: x - 1, newIndex: y - 1 });
            x--;
            y--;
        }
        
        if (d > 0) {
            if (x === prevX) {
                diff.push({ type: 'insert', newIndex: y - 1 });
                y--;
            } else {
                diff.push({ type: 'delete', oldIndex: x - 1 });
                x--;
            }
        }
    }
    
    return diff.reverse();
}

// Группирует изменения в hunks (блоки).
// Возвращает массив объектов { startLine, endLine, type, hunkId }.
function computeDiffHunks() {
    const st = editorState;
    const original = st.originalConfig.split('\n');
    const current = st.editor.value.split('\n');
    const hunks = [];
    let hunkId = 0;
    
    const diff = computeMyersDiff(original, current);
    
    let i = 0;
    let lastEqualNewIndex = -1;
    
    while (i < diff.length) {
        const op = diff[i];
        
        if (op.type === 'equal') {
            lastEqualNewIndex = op.newIndex;
            i++;
            continue;
        }
        
        const deleteOps = [];
        const insertOps = [];
        
        // Собираем все операции до следующего equal.
        while (i < diff.length && diff[i].type !== 'equal') {
            if (diff[i].type === 'delete') {
                deleteOps.push(diff[i]);
            } else if (diff[i].type === 'insert') {
                insertOps.push(diff[i]);
            }
            i++;
        }
        
        // Если есть и удаления, и добавления в одном блоке - это modified.
        if (deleteOps.length > 0 && insertOps.length > 0) {
            const startLine = insertOps[0].newIndex;
            const endLine = insertOps[insertOps.length - 1].newIndex;
            
            hunks.push({
                startLine,
                endLine,
                type: 'modified',
                hunkId: hunkId++,
                deleteOps,
                insertOps
            });
        } else if (insertOps.length > 0) {
            const startLine = insertOps[0].newIndex;
            const endLine = insertOps[insertOps.length - 1].newIndex;
            
            hunks.push({
                startLine,
                endLine,
                type: 'added',
                hunkId: hunkId++,
                deleteOps: [],
                insertOps
            });
        } else if (deleteOps.length > 0) {
            if (lastEqualNewIndex >= 0 && lastEqualNewIndex < current.length) {
                hunks.push({
                    startLine: lastEqualNewIndex,
                    endLine: lastEqualNewIndex,
                    type: 'deleted',
                    hunkId: hunkId++,
                    deleteOps,
                    insertOps: []
                });
            }
        }
    }
    
    return hunks;
}

// Вычисляет diff между оригиналом и текущим содержимым.
// Возвращает массив статусов для каждой строки: 'modified', 'added', 'deleted', null.
function computeLineDiff() {
    const hunks = computeDiffHunks();
    const current = editorState.editor.value.split('\n');
    const result = new Array(current.length).fill(null);
    
    for (const hunk of hunks) {
        for (let line = hunk.startLine; line <= hunk.endLine; line++) {
            result[line] = hunk.type;
        }
    }
    
    return result;
}

function renderGutter(count) {
    const st = editorState;
    const gutter = st.gutterInner;
    const current = gutter.childElementCount;

    // Вычисляем diff hunks и статусы.
    const hunks = computeDiffHunks();
    const diff = computeLineDiff();
    
    // Мапа hunkId по индексу строки.
    const lineToHunk = new Map();
    for (const hunk of hunks) {
        for (let line = hunk.startLine; line <= hunk.endLine; line++) {
            lineToHunk.set(line, hunk.hunkId);
        }
    }

    if (count > current) {
        const fragment = document.createDocumentFragment();
        for (let i = current + 1; i <= count; i++) {
            const el = document.createElement('div');
            el.textContent = i;
            el.className = 'gutter-line';
            if (diff[i - 1]) {
                el.classList.add('line-' + diff[i - 1]);
                el.style.cursor = 'pointer';
                el.dataset.lineIndex = i - 1;
                el.dataset.diffType = diff[i - 1];
                el.dataset.hunkId = lineToHunk.get(i - 1);
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
            el.style.cursor = 'pointer';
            el.dataset.lineIndex = i;
            el.dataset.diffType = diff[i];
            el.dataset.hunkId = lineToHunk.get(i);
        } else {
            el.style.cursor = '';
            delete el.dataset.lineIndex;
            delete el.dataset.diffType;
            delete el.dataset.hunkId;
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
    const badge = document.getElementById('configTabBadge');

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
    
    // Показываем/скрываем индикатор на вкладке.
    if (badge) {
        badge.style.display = (isModified || st.hasPendingChanges) ? 'inline-block' : 'none';
    }
}

function handleGutterClick(e) {
    const target = e.target;
    if (!target.dataset.hunkId) return;
    
    const hunkId = parseInt(target.dataset.hunkId, 10);
    const hunks = computeDiffHunks();
    const hunk = hunks.find(h => h.hunkId === hunkId);
    
    if (!hunk) return;
    
    showDiffPopup(hunk, target);
}

// Получает оригинальный контент для hunk.
function getOriginalContent(hunk, originalLines) {
    if (hunk.type === 'deleted') {
        return hunk.deleteOps.map(op => originalLines[op.oldIndex]);
    } else if (hunk.type === 'modified') {
        // Для modified берём диапазон по позициям insertOps в оригинале.
        if (hunk.insertOps.length > 0) {
            const firstInsertLine = Math.min(...hunk.insertOps.map(op => op.newIndex));
            const lastInsertLine = Math.max(...hunk.insertOps.map(op => op.newIndex));
            const insertCount = lastInsertLine - firstInsertLine + 1;
            
            // Берём из оригинала insertCount строк, начиная с firstInsertLine.
            const lines = [];
            for (let i = firstInsertLine; i < firstInsertLine + insertCount && i < originalLines.length; i++) {
                lines.push(originalLines[i]);
            }
            return lines;
        }
    }
    return [];
}

function showDiffPopup(hunk, targetElement) {
    const st = editorState;
    const originalLines = st.originalConfig.split('\n');
    const currentLines = st.editor.value.split('\n');
    
    // Удаляем старый попап, если есть.
    const existing = document.querySelector('.diff-popup');
    if (existing) existing.remove();
    
    // Собираем оригинальный код для hunk.
    let originalContent = '';
    let showContent = true;
    
    if (hunk.type === 'deleted') {
        const lines = getOriginalContent(hunk, originalLines);
        originalContent = lines.join('\n');
    } else if (hunk.type === 'added') {
        // Для added показываем текущий код, который будет удалён.
        originalContent = hunk.insertOps.map(op => currentLines[op.newIndex]).join('\n');
        showContent = false;
    } else if (hunk.type === 'modified') {
        const lines = getOriginalContent(hunk, originalLines);
        originalContent = lines.join('\n');
    }
    
    if (!originalContent) {
        return;
    }
    
    const popup = document.createElement('div');
    popup.className = 'diff-popup';
    
    // Применяем подсветку синтаксиса.
    const highlightedContent = highlightCode(originalContent);
    
    const buttonText = hunk.type === 'added' ? '🗑' : '↶';
    const buttonTitle = hunk.type === 'added' ? 'Удалить добавленные строки' : 'Вернуть оригинал';
    
    popup.innerHTML = `
        ${showContent ? '<div class="diff-popup-content">' + highlightedContent + '</div>' : ''}
        <div class="diff-popup-toolbar">
            <button class="diff-popup-revert" title="${buttonTitle}">${buttonText}</button>
            <button class="diff-popup-close" title="Закрыть">×</button>
        </div>
    `;
    
    // Добавляем попап в контейнер редактора.
    st.container.appendChild(popup);
    
    // Сохраняем ссылку на строку для обновления позиции при скролле.
    popup.dataset.lineIndex = targetElement.dataset.lineIndex;
    
    // Функция обновления позиции попапа.
    function updatePopupPosition() {
        const containerRect = st.container.getBoundingClientRect();
        const targetRect = targetElement.getBoundingClientRect();
        const gutterWidth = st.gutterInner.offsetWidth;
        
        const left = gutterWidth;
        const top = targetRect.bottom - containerRect.top;
        
        popup.style.left = `${left}px`;
        popup.style.top = `${top}px`;
        popup.style.right = '0';
    }
    
    updatePopupPosition();
    
    // Отслеживаем скролл редактора.
    const scrollHandler = () => updatePopupPosition();
    st.editor.addEventListener('scroll', scrollHandler);
    
    // Обработчики.
    popup.querySelector('.diff-popup-close').onclick = (e) => {
        e.stopPropagation();
        st.editor.removeEventListener('scroll', scrollHandler);
        popup.remove();
    };
    popup.querySelector('.diff-popup-revert').onclick = (e) => {
        e.stopPropagation();
        revertHunk(hunk);
        st.editor.removeEventListener('scroll', scrollHandler);
        popup.remove();
    };
    
    // Закрытие при клике вне попапа.
    setTimeout(() => {
        document.addEventListener('click', function closePopup(e) {
            if (!popup.contains(e.target) && !targetElement.contains(e.target)) {
                st.editor.removeEventListener('scroll', scrollHandler);
                popup.remove();
                document.removeEventListener('click', closePopup);
            }
        });
    }, 0);
}

function revertHunk(hunk) {
    const st = editorState;
    const originalLines = st.originalConfig.split('\n');
    const currentLines = st.editor.value.split('\n');
    
    if (hunk.type === 'deleted') {
        // Восстанавливаем удалённые строки после startLine.
        const lines = getOriginalContent(hunk, originalLines);
        currentLines.splice(hunk.startLine + 1, 0, ...lines);
    } else if (hunk.type === 'added') {
        // Удаляем добавленные строки.
        currentLines.splice(hunk.startLine, hunk.endLine - hunk.startLine + 1);
    } else if (hunk.type === 'modified') {
        // Для modified: заменяем диапазон insertOps на оригинальные строки.
        if (hunk.insertOps.length > 0) {
            const firstInsertLine = Math.min(...hunk.insertOps.map(op => op.newIndex));
            const lastInsertLine = Math.max(...hunk.insertOps.map(op => op.newIndex));
            const currentCount = lastInsertLine - firstInsertLine + 1;
            
            // Получаем оригинальные строки той же функцией, что и для отображения.
            const originalContent = getOriginalContent(hunk, originalLines);
            
            // Удаляем currentCount строк и вставляем оригинальные.
            currentLines.splice(firstInsertLine, currentCount, ...originalContent);
        }
    }
    
    const newValue = currentLines.join('\n');
    const cursorPos = st.editor.selectionStart;
    
    applyEdit(newValue, cursorPos, cursorPos);
    
    showMessage('Изменение отменено', 'success');
}

function revertDiff(lineIndex, diffType) {
    const st = editorState;
    const originalLines = st.originalConfig.split('\n');
    const currentLines = st.editor.value.split('\n');
    
    if (diffType === 'modified') {
        // Вернуть оригинальную строку
        currentLines[lineIndex] = originalLines[lineIndex] || '';
    } else if (diffType === 'added') {
        // Удалить добавленную строку
        currentLines.splice(lineIndex, 1);
    } else if (diffType === 'deleted') {
        // Восстановить удалённые строки после lineIndex
        const diff = computeMyersDiff(originalLines, currentLines);
        const deletedLines = [];
        
        let foundBlock = false;
        for (const op of diff) {
            if (op.type === 'equal' && op.newIndex === lineIndex) {
                foundBlock = true;
                continue;
            }
            if (foundBlock && op.type === 'delete') {
                deletedLines.push(originalLines[op.oldIndex]);
            }
            if (foundBlock && op.type === 'equal') {
                break;
            }
        }
        
        // Вставляем удалённые строки после lineIndex
        currentLines.splice(lineIndex + 1, 0, ...deletedLines);
    }
    
    const newValue = currentLines.join('\n');
    const cursorPos = st.editor.selectionStart;
    
    applyEdit(newValue, cursorPos, cursorPos);
    
    // Закрываем модалку
    document.querySelector('.diff-modal-overlay')?.remove();
    
    showMessage('Изменение отменено', 'success');
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
    
    // Обработчик клика на gutter для показа diff
    gutterInner.addEventListener('click', handleGutterClick);

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
    checkGroupsSync();
    checkDNSRecordsSync();
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

        const responseData = JSON.parse(await response.text())

        showMessage('Конфиг сохранён временно. Нажмите "Применить" для активации.', 'success');
        st.loadedConfig = config; // Обновляем loaded для сравнения
        st.hasPendingChanges = responseData.hasPending;
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

        // Сервер при применении синхронизирует группы, поэтому перечитываем итоговый конфиг.
        await loadConfig();
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

// Проверяет наличие несохраненных изменений конфига при загрузке страницы.
async function checkPendingConfig() {
    try {
        const response = await fetch('/api/config/check-pending');
        const data = await response.json();

        if (data.hasPending) {
            const badge = document.getElementById('configTabBadge');

            if (badge) {
                badge.style.display = 'inline-block';
            }
        }
    } catch (error) {
        console.error('Failed to check pending config:', error);
    }
}

function initOverviewInteractions() {
    const wrap = document.getElementById('ovChartWrap');
    if (!wrap) return;

    wrap.addEventListener('pointermove', handleChartPointer);
    wrap.addEventListener('pointerleave', hideChartTooltip);

    window.addEventListener('resize', () => {
        if (document.getElementById('overview-tab').classList.contains('active')) {
            renderTrafficChart();
            renderMemorySpark();
        }
    });
}

initOverviewInteractions();
checkPendingConfig();
restoreActiveTab();
initUpdates();

// ============================================================================
// Outbounds Management
// ============================================================================

// Парсит share-ссылку и возвращает outbound объект для sing-box.
function parseShareUrl(url) {
    try {
        if (url.startsWith('vless://')) {
            return parseVless(url);
        } else if (url.startsWith('hysteria2://') || url.startsWith('hy2://')) {
            return parseHysteria2(url);
        } else if (url.startsWith('vmess://')) {
            return parseVmess(url);
        } else if (url.startsWith('trojan://')) {
            return parseTrojan(url);
        } else if (url.startsWith('ss://')) {
            return parseShadowsocks(url);
        } else {
            throw new Error('Неподдерживаемый протокол. Поддерживаются: vless, hysteria2, vmess, trojan, ss');
        }
    } catch (error) {
        throw new Error('Ошибка парсинга: ' + error.message);
    }
}

function parseVless(url) {
    const u = new URL(url);
    const uuid = u.username;
    const server = u.hostname;
    const port = parseInt(u.port) || 443;
    const params = new URLSearchParams(u.search);
    const tag = decodeURIComponent(u.hash.slice(1)) || `vless-${server}`;
    
    const outbound = {
        type: 'vless',
        tag: tag,
        server: server,
        server_port: port,
        uuid: uuid,
        flow: params.get('flow') || '',
        network: params.get('type') || 'tcp',
        tls: {}
    };
    
    if (params.get('security') === 'tls' || params.get('security') === 'reality') {
        outbound.tls.enabled = true;
        outbound.tls.server_name = params.get('sni') || server;
        
        if (params.get('fp')) {
            outbound.tls.utls = {
                enabled: true,
                fingerprint: params.get('fp')
            };
        }
        
        if (params.get('security') === 'reality') {
            outbound.tls.reality = {
                enabled: true,
                public_key: params.get('pbk') || '',
                short_id: params.get('sid') || ''
            };
        }
    }
    
    // Transport настройки.
    if (outbound.network === 'ws') {
        outbound.transport = {
            type: 'ws',
            path: params.get('path') || '/',
            headers: params.get('host') ? { Host: params.get('host') } : {}
        };
    } else if (outbound.network === 'grpc') {
        outbound.transport = {
            type: 'grpc',
            service_name: params.get('serviceName') || params.get('path') || ''
        };
    } else if (outbound.network === 'xhttp' || outbound.network === 'splithttp') {
        outbound.transport = {
            type: outbound.network,
            path: params.get('path') || '/',
            host: params.get('host') || server
        };
    }
    
    return outbound;
}

function parseHysteria2(url) {
    const u = new URL(url);
    const password = u.username || decodeURIComponent(u.pathname.split('@')[0].slice(2));
    const serverPart = u.username ? u.hostname : u.pathname.split('@')[1].split(':')[0];
    const portPart = u.username ? u.port : u.pathname.split('@')[1].split(':')[1];
    const server = serverPart;
    const port = parseInt(portPart) || 443;
    const params = new URLSearchParams(u.search);
    const tag = decodeURIComponent(u.hash.slice(1)) || `hy2-${server}`;
    
    const outbound = {
        type: 'hysteria2',
        tag: tag,
        server: server,
        server_port: port,
        password: password,
        tls: {
            enabled: true,
            server_name: params.get('sni') || server
        }
    };
    
    if (params.get('obfs')) {
        outbound.obfs = {
            type: params.get('obfs'),
            password: params.get('obfs-password') || ''
        };
    }
    
    return outbound;
}

function parseVmess(url) {
    const base64 = url.slice(8);
    const json = JSON.parse(atob(base64));
    
    const outbound = {
        type: 'vmess',
        tag: json.ps || `vmess-${json.add}`,
        server: json.add,
        server_port: parseInt(json.port),
        uuid: json.id,
        security: json.scy || 'auto',
        alter_id: parseInt(json.aid) || 0
    };
    
    if (json.net && json.net !== 'tcp') {
        outbound.transport = {
            type: json.net,
            path: json.path || '/'
        };
        
        if (json.host) {
            outbound.transport.host = json.host;
        }
    }
    
    if (json.tls === 'tls') {
        outbound.tls = {
            enabled: true,
            server_name: json.sni || json.host || json.add
        };
    }
    
    return outbound;
}

function parseTrojan(url) {
    const u = new URL(url);
    const password = u.username;
    const server = u.hostname;
    const port = parseInt(u.port) || 443;
    const params = new URLSearchParams(u.search);
    const tag = decodeURIComponent(u.hash.slice(1)) || `trojan-${server}`;
    
    const outbound = {
        type: 'trojan',
        tag: tag,
        server: server,
        server_port: port,
        password: password,
        tls: {
            enabled: true,
            server_name: params.get('sni') || server
        }
    };
    
    if (params.get('type') && params.get('type') !== 'tcp') {
        outbound.transport = {
            type: params.get('type'),
            path: params.get('path') || '/'
        };
    }
    
    return outbound;
}

function parseShadowsocks(url) {
    const u = new URL(url);
    const userinfo = atob(u.username);
    const [method, password] = userinfo.split(':');
    const server = u.hostname;
    const port = parseInt(u.port);
    const tag = decodeURIComponent(u.hash.slice(1)) || `ss-${server}`;
    
    return {
        type: 'shadowsocks',
        tag: tag,
        server: server,
        server_port: port,
        method: method,
        password: password
    };
}

async function addOutbound(event) {
    event.preventDefault();
    
    const urlInput = document.getElementById('outboundUrl');
    const shareUrl = urlInput.value.trim();
    
    if (!shareUrl) {
        showMessage('Введите share-ссылку', 'error');
        return;
    }
    
    try {
        const response = await fetch('/api/outbounds/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ shareUrl: shareUrl })
        });
        
        const result = await response.json();
        
        if (!response.ok) {
            throw new Error(result.error || 'Не удалось добавить outbound');
        }
        
        showMessage(result.message || `Outbound "${result.outbound.tag}" добавлен`, 'success');
        urlInput.value = '';
        
        await loadOutbounds();
        
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

async function loadOutbounds() {
    const tbody = document.getElementById('outboundsTableBody');
    if (!tbody) return;
    
    try {
        const response = await fetch('/api/outbounds');
        if (!response.ok) {
            tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: #999;">Не удалось загрузить outbounds</td></tr>';
            return;
        }
        
        const data = await response.json();
        const outbounds = data.outbounds || [];
        
        tbody.innerHTML = '';
        
        if (outbounds.length === 0) {
            tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: #999;">Нет добавленных outbounds</td></tr>';
            return;
        }
        
        outbounds.forEach(outbound => {
            const row = document.createElement('tr');
            row.innerHTML = `
                <td>${escapeHTML(outbound.tag)}</td>
                <td><span class="badge badge-${escapeHTML(outbound.type)}">${escapeHTML(outbound.type)}</span></td>
                <td>${escapeHTML(outbound.server || '-')}</td>
                <td>${outbound.port || '-'}</td>
                <td>
                    <button class="btn btn-danger" onclick="deleteOutbound('${escapeHTML(outbound.tag)}')">Удалить</button>
                </td>
            `;
            tbody.appendChild(row);
        });
        
    } catch (error) {
        console.error('Ошибка загрузки outbounds:', error);
        tbody.innerHTML = '<tr><td colspan="5" style="text-align: center; color: #999;">Ошибка загрузки</td></tr>';
    }
}

async function deleteOutbound(tag) {
    if (!confirm(`Удалить outbound "${tag}"?`)) return;
    
    try {
        const response = await fetch('/api/outbounds/delete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ tag: tag })
        });
        
        const result = await response.json();
        
        if (!response.ok) {
            throw new Error(result.error || 'Не удалось удалить outbound');
        }
        
        showMessage(result.message || `Outbound "${tag}" удалён`, 'success');
        
        await loadOutbounds();
        
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    }
}

// ============================================================================
// Happ Profiles
// ============================================================================

let happBusy = false; // Блокирует автообновление списка во время действий

// Форматирует количество байт в человекочитаемый вид.
function formatHappBytes(bytes) {
    if (!bytes) return '0 Б';

    const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
    const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
    const value = bytes / Math.pow(1024, index);

    return `${value.toFixed(value >= 100 || index === 0 ? 0 : 1)} ${units[index]}`;
}

// Возвращает «5 мин назад» и т.п. для даты.
function formatHappAgo(dateString) {
    const date = new Date(dateString);
    if (isNaN(date) || date.getFullYear() < 2000) return 'никогда';

    const minutes = Math.round((Date.now() - date.getTime()) / 60000);
    if (minutes < 1) return 'только что';
    if (minutes < 60) return `${minutes} мин назад`;

    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours} ч назад`;

    return `${Math.round(hours / 24)} дн назад`;
}

// Склоняет слово «день».
function pluralDays(n) {
    const mod10 = n % 10;
    const mod100 = n % 100;

    if (mod10 === 1 && mod100 !== 11) return 'день';
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return 'дня';

    return 'дней';
}

async function loadHappProfiles() {
    if (happBusy) return;

    const container = document.getElementById('happProfiles');
    if (!container) return;

    try {
        const response = await fetch('/api/happ/profiles');
        if (!response.ok) throw new Error(await response.text());

        const data = await response.json();
        document.getElementById('happHwid').textContent = data.installation_id || '—';

        const profiles = data.profiles || [];
        if (profiles.length === 0) {
            container.innerHTML = '<div class="empty-state">Подписок пока нет. Добавьте ссылку выше.</div>';
            return;
        }

        // Сохраняем раскрытые списки серверов между перерисовками.
        const openIds = new Set([...container.querySelectorAll('details[open]')].map(d => d.dataset.id));
        container.innerHTML = profiles.map(p => renderHappProfile(p, openIds.has(p.id))).join('');
    } catch (error) {
        console.error('Ошибка загрузки подписок:', error);
        container.innerHTML = '<div class="empty-state">Не удалось загрузить подписки</div>';
    }
}

function renderHappProfile(profile, serversOpen) {
    const info = profile.info || {};
    const id = escapeHTML(profile.id);

    // Трафик.
    const used = (info.upload || 0) + (info.download || 0);
    const total = info.total || 0;
    let trafficValue = formatHappBytes(used);
    let trafficFoot = 'Без лимита трафика';
    let trafficBar = '';

    if (total > 0) {
        const percent = Math.min(100, used / total * 100);
        const level = percent >= 90 ? 'is-bad' : percent >= 75 ? 'is-warn' : 'is-good';
        trafficValue += ` <span class="stat-unit">из ${formatHappBytes(total)}</span>`;
        trafficFoot = `Осталось ${formatHappBytes(Math.max(0, total - used))}`;
        trafficBar = `<div class="happ-meter"><div class="happ-meter-fill ${level}" style="width: ${percent.toFixed(1)}%"></div></div>`;
    }

    // Срок действия.
    let expireValue = '∞';
    let expireFoot = 'Бессрочно';
    let expireDot = 'is-good';

    if (info.expire) {
        const expire = new Date(info.expire);
        const days = Math.floor((expire.getTime() - Date.now()) / 86400000);
        expireValue = expire.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' });

        if (days < 0) {
            expireFoot = 'Подписка истекла';
            expireDot = 'is-bad';
        } else {
            expireFoot = `Осталось ${days} ${pluralDays(days)}`;
            expireDot = days < 3 ? 'is-bad' : days < 7 ? 'is-warn' : 'is-good';
        }
    }

    // Серверы.
    const servers = profile.servers || [];
    const warnings = profile.warnings || [];
    const proxies = servers.filter(s => s.type === 'proxy').length;
    const autoGroups = servers.length - proxies;

    const statusDot = profile.last_error ? 'is-bad' : profile.out_of_sync ? 'is-warn' : 'is-good';
    const statusText = profile.last_error
        ? 'Ошибка обновления'
        : profile.out_of_sync ? 'Серверы изменились, обновите конфиг' : 'Синхронизировано с конфигом';

    const links = [
        info.support_url ? `<a href="${escapeHTML(info.support_url)}" target="_blank" rel="noopener">Поддержка</a>` : '',
        info.web_page_url ? `<a href="${escapeHTML(info.web_page_url)}" target="_blank" rel="noopener">Кабинет</a>` : '',
    ].filter(Boolean).join('<span class="happ-sep">·</span>');

    const serverRows = servers.map(s => {
        const address = s.address
            ? escapeHTML(`${s.address}:${s.port}`)
            : `<span class="card-hint">самый быстрый из ${(s.outbound.outbounds || []).length}</span>`;

        return `
        <tr>
            <td>${escapeHTML(s.name)}</td>
            <td><span class="badge badge-${s.type === 'urltest' ? 'group' : 'domain'}">${escapeHTML(s.protocol)}</span></td>
            <td>${address}</td>
        </tr>`;
    }).join('');

    return `
    <div class="add-form-card happ-card">
        <div class="happ-card-head">
            <div class="happ-card-title">
                <div class="form-header" style="margin-bottom: 4px;">${escapeHTML(profile.name)}</div>
                <div class="happ-card-sub">
                    <span class="stat-dot ${statusDot}"></span>
                    <span>${statusText}</span>
                    <span class="happ-sep">·</span>
                    <span title="${escapeHTML(profile.url)}">обновлено ${formatHappAgo(profile.last_update)}</span>
                    ${links ? `<span class="happ-sep">·</span>${links}` : ''}
                </div>
            </div>
            <div class="happ-card-actions">
                <button class="btn btn-secondary btn-sm" onclick="happProfileAction('refresh', '${id}', this)">Обновить</button>
                <button class="btn ${profile.out_of_sync ? 'btn-warning' : 'btn-secondary'} btn-sm" onclick="happProfileAction('sync', '${id}', this)">В конфиг</button>
                <button class="btn btn-danger btn-sm" onclick="deleteHappProfile('${id}', this)">Удалить</button>
            </div>
        </div>

        ${profile.last_error ? `<div class="happ-note is-bad">${escapeHTML(profile.last_error)}</div>` : ''}
        ${info.announce ? `<div class="happ-note">${escapeHTML(info.announce)}</div>` : ''}

        <div class="stat-row happ-stats">
            <div class="stat-tile">
                <div class="stat-label">Потрачено трафика</div>
                <div class="stat-value">${trafficValue}</div>
                ${trafficBar}
                <div class="stat-foot">${trafficFoot}</div>
            </div>
            <div class="stat-tile">
                <div class="stat-label">Действует до</div>
                <div class="stat-value happ-date">${escapeHTML(expireValue)}</div>
                <div class="stat-foot"><span class="stat-dot ${expireDot}"></span>${expireFoot}</div>
            </div>
            <div class="stat-tile">
                <div class="stat-label">Серверы</div>
                <div class="stat-value">${proxies}<span class="stat-unit">${autoGroups ? `+ ${autoGroups} авто` : 'шт'}</span></div>
                <div class="stat-foot">${warnings.length ? `Пропущено: ${warnings.length}` : 'Все поддерживаются sing-box'}</div>
            </div>
        </div>

        ${warnings.length ? `<div class="happ-note">Не поддерживаются sing-box: ${warnings.map(escapeHTML).join('; ')}</div>` : ''}

        <details class="happ-servers" data-id="${id}" ${serversOpen ? 'open' : ''}>
            <summary>Список серверов</summary>
            <table class="table">
                <thead><tr><th>Название</th><th>Тип</th><th>Адрес</th></tr></thead>
                <tbody>${serverRows}</tbody>
            </table>
        </details>
    </div>`;
}

async function addHappProfile(event) {
    event.preventDefault();

    const urlInput = document.getElementById('happProfileUrl');
    const nameInput = document.getElementById('happProfileName');
    const button = document.getElementById('happAddBtn');

    button.disabled = true;
    button.textContent = 'Загружаю подписку...';
    happBusy = true;

    try {
        const response = await fetch('/api/happ/profiles/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: urlInput.value.trim(), name: nameInput.value.trim() })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Не удалось добавить подписку');

        showMessage(`${result.message}. Примените его на вкладке «Конфиг».`, 'success');
        urlInput.value = '';
        nameInput.value = '';
        checkPendingConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        button.disabled = false;
        button.textContent = 'Добавить';
        happBusy = false;
        await loadHappProfiles();
    }
}

async function happProfileAction(action, id, button) {
    const original = button.textContent;
    button.disabled = true;
    button.textContent = '...';
    happBusy = true;

    try {
        const response = await fetch(`/api/happ/profiles/${action}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Действие не выполнено');

        showMessage(result.message, 'success');

        if (action !== 'refresh') checkPendingConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        button.disabled = false;
        button.textContent = original;
        happBusy = false;
        await loadHappProfiles();
    }
}

async function deleteHappProfile(id, button) {
    if (!confirm('Удалить подписку и её серверы из конфига?\n\nМесто устройства в подписке освободите в боте провайдера.')) return;

    await happProfileAction('delete', id, button);
}

async function copyHappHwid() {
    const hwid = document.getElementById('happHwid').textContent;

    try {
        await navigator.clipboard.writeText(hwid);
        showMessage('HWID скопирован', 'success');
    } catch (error) {
        showMessage('Не удалось скопировать: ' + hwid, 'error');
    }
}

// ============================================================================
// Updates
// ============================================================================

const UPDATE_STEP_TITLES = {
    'start': 'Запуск',
    'prepare': 'Проверка',
    'pull': 'Загрузка образов',
    'update-controller': 'Обновление docker-controller',
    'backup': 'Резервная копия',
    'update-configurer': 'Обновление конфигуратора',
    'update-compose': 'Обновление compose-файла',
    'commit': 'Завершение',
    'rollback': 'Откат',
};

const updateState = {
    check: null,           // Результат /api/update/check
    status: null,          // Результат /api/update/status
    polling: null,         // Таймер опроса статуса
    serviceDown: false,    // Сервис недоступен (перезапускается во время обновления)
    loadedVersion: null,   // Версия, с которой загружена страница
};

async function initUpdates() {
    await checkUpdates(false);
    await refreshUpdateStatus();

    // Если обновление уже идет (например, страницу перезагрузили), продолжаем следить за ним.
    if (updateState.status && updateState.status.running) {
        startUpdatePolling();
    }
}

async function checkUpdates(force) {
    const button = document.getElementById('updateCheckBtn');
    const panel = document.getElementById('updatePanel');

    if (button) button.disabled = true;
    if (panel) panel.classList.add('is-loading');

    try {
        const response = await fetch(`/api/update/check${force ? '?force=true' : ''}`);
        if (!response.ok) throw new Error(await response.text());

        updateState.check = await response.json();

        if (!updateState.loadedVersion) {
            updateState.loadedVersion = updateState.check.current_version;
        }
    } catch (error) {
        updateState.check = { error: error.message, available: [] };
    } finally {
        if (button) button.disabled = false;
        if (panel) panel.classList.remove('is-loading');
        renderUpdatePanel();
    }
}

async function refreshUpdateStatus() {
    try {
        const response = await fetch('/api/update/status');
        const data = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(data.error || 'Не удалось получить статус');

        updateState.status = data;
        updateState.serviceDown = false;
    } catch (error) {
        // Во время обновления конфигуратор перезапускается — это ожидаемо.
        if (updateState.polling) {
            updateState.serviceDown = true;
        } else {
            updateState.status = { exists: false, error_fetch: error.message };
        }
    }

    renderUpdatePanel();
}

function startUpdatePolling() {
    if (updateState.polling) return;

    updateState.polling = setInterval(async () => {
        await refreshUpdateStatus();

        const status = updateState.status;

        if (!updateState.serviceDown && status && status.exists && !status.running && status.result) {
            stopUpdatePolling();
            onUpdateFinished(status);
        }
    }, 2000);
}

function stopUpdatePolling() {
    clearInterval(updateState.polling);
    updateState.polling = null;
}

async function onUpdateFinished(status) {
    if (status.result === 'succeeded') {
        showMessage(`✅ Обновлено до ${status.to_version}`, 'success');
    } else if (status.result === 'rolled_back') {
        showMessage('⚠️ Обновление не удалось, выполнен откат на прежнюю версию', 'error');
    } else {
        showMessage('❌ Обновление завершилось ошибкой', 'error');
    }

    await checkUpdates(true);

    // Новая версия отдает новую статику — перезагружаем страницу.
    if (updateState.check && updateState.check.current_version !== updateState.loadedVersion) {
        setTimeout(() => window.location.reload(), 3000);
    }
}

async function startUpdate() {
    const select = document.getElementById('updateVersionSelect');
    const version = select ? select.value : '';

    if (!version) return;

    const current = updateState.check ? updateState.check.current_version : '';

    if (!confirm(`Обновить ${current} → ${version}?\n\nВеб-интерфейс будет недоступен несколько секунд. sing-box продолжит работать.\nПри ошибке обновление откатится автоматически.`)) {
        return;
    }

    const button = document.getElementById('updateStartBtn');
    if (button) button.disabled = true;

    try {
        const response = await fetch('/api/update/start', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ version })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Не удалось запустить обновление');

        updateState.status = result;
        showMessage(`Обновление до ${version} запущено`, 'success');
        startUpdatePolling();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        if (button) button.disabled = false;
        renderUpdatePanel();
    }
}

function renderUpdatePanel() {
    const check = updateState.check || {};
    const status = updateState.status || {};
    const summary = document.getElementById('updateSummary');
    const body = document.getElementById('updateBody');
    const versionLabel = document.getElementById('appVersion');
    const badge = document.getElementById('updateTabBadge');

    if (versionLabel && check.current_version) {
        versionLabel.textContent = check.current_version;
    }

    const available = check.available || [];
    const inProgress = Boolean(updateState.polling) || Boolean(status.running);

    if (badge) {
        badge.style.display = available.length > 0 && !inProgress ? 'inline-block' : 'none';
    }

    if (!summary || !body) return;

    // Сводка.
    if (inProgress) {
        summary.textContent = updateState.serviceDown
            ? 'Идёт обновление: сервис перезапускается...'
            : `Идёт обновление до ${status.target || status.to_version || ''}...`;
    } else if (check.error) {
        summary.textContent = `Текущая версия ${check.current_version || '—'}. Не удалось проверить обновления: ${check.error}`;
    } else if (available.length > 0) {
        summary.textContent = `Текущая версия ${check.current_version}. Доступна ${check.latest_version}.`;
    } else if (check.current_version) {
        summary.textContent = `Текущая версия ${check.current_version} — последняя.`;
    }

    let html = '';

    // Выбор версии и запуск.
    if (available.length > 0 && !inProgress) {
        const options = available.map((release, i) =>
            `<option value="${escapeHTML(release.version)}">${escapeHTML(release.version)}${i === 0 ? ' (последняя)' : ''}</option>`
        ).join('');

        html += `
            <div class="update-actions">
                <select class="form-select" id="updateVersionSelect" onchange="renderUpdateChangelog()">${options}</select>
                <button class="btn btn-warning" id="updateStartBtn" onclick="startUpdate()">Обновить</button>
            </div>
            <div id="updateChangelog"></div>`;
    }

    // Прогресс или результат последнего обновления.
    if (status.exists) {
        html += renderUpdateStatus(status, inProgress);
    }

    // Сохраняем выбранную версию между перерисовками.
    const previous = document.getElementById('updateVersionSelect');
    const selected = previous ? previous.value : null;

    body.innerHTML = html;

    const select = document.getElementById('updateVersionSelect');

    if (select && selected && [...select.options].some(o => o.value === selected)) {
        select.value = selected;
    }

    renderUpdateChangelog();
}

function renderUpdateChangelog() {
    const container = document.getElementById('updateChangelog');
    const select = document.getElementById('updateVersionSelect');

    if (!container || !select) return;

    // Показываем изменения всех версий от текущей до выбранной.
    const available = (updateState.check && updateState.check.available) || [];
    const index = available.findIndex(r => r.version === select.value);
    const releases = available.slice(index).filter(r => r.changelog);

    if (releases.length === 0) {
        container.innerHTML = '<p class="card-hint" style="margin: 12px 0 0;">Список изменений не указан.</p>';
        return;
    }

    container.innerHTML = releases.map(r => `
        <div class="update-changelog">
            <div class="update-changelog-version">${escapeHTML(r.version)}</div>
            <pre>${escapeHTML(r.changelog)}</pre>
        </div>`).join('');
}

function renderUpdateStatus(status, inProgress) {
    const results = {
        'succeeded': { dot: 'is-good', text: `Обновлено ${status.from_version} → ${status.to_version}` },
        'rolled_back': { dot: 'is-warn', text: `Обновление до ${status.to_version || status.target} не удалось, выполнен откат` },
        'failed': { dot: 'is-bad', text: `Обновление до ${status.to_version || status.target} завершилось ошибкой` },
    };

    const header = inProgress
        ? { dot: 'is-warn', text: `Обновление до ${status.target} выполняется` }
        : (results[status.result] || { dot: '', text: 'Последнее обновление' });

    const steps = (status.steps || []).map(step => {
        const time = step.time ? new Date(step.time).toLocaleTimeString('ru-RU') : '';
        const level = step.level === 'ERROR' ? 'is-bad' : step.level === 'WARN' ? 'is-warn' : '';

        return `
            <div class="update-step ${level}">
                <span class="update-step-time">${escapeHTML(time)}</span>
                <span class="update-step-name">${escapeHTML(UPDATE_STEP_TITLES[step.step] || step.step || '')}</span>
                <span class="update-step-msg">${escapeHTML(step.message)}${step.error ? `<br><span class="update-step-error">${escapeHTML(step.error)}</span>` : ''}</span>
            </div>`;
    }).join('');

    return `
        <div class="update-status">
            <div class="update-status-head">
                <span class="stat-dot ${header.dot}"></span>
                <span>${escapeHTML(header.text)}</span>
                ${status.finished_at && !inProgress && !status.finished_at.startsWith('0001') ? `<span class="happ-sep">·</span><span class="card-hint">${escapeHTML(new Date(status.finished_at).toLocaleString('ru-RU'))}</span>` : ''}
            </div>
            ${status.error ? `<pre class="update-error">${escapeHTML(status.error)}</pre>` : ''}
            <details class="happ-servers" ${inProgress ? 'open' : ''}>
                <summary>Журнал обновления</summary>
                <div class="update-steps">${steps || '<span class="card-hint">Пока пусто</span>'}</div>
            </details>
        </div>`;
}

// ============================================================================
// Amnezia
// ============================================================================

let amneziaBusy = false; // Блокирует автообновление списка во время действий

async function loadAmneziaProfiles() {
    if (amneziaBusy) return;

    const container = document.getElementById('amneziaProfiles');
    if (!container) return;

    try {
        const response = await fetch('/api/amnezia/profiles');
        if (!response.ok) throw new Error(await response.text());

        const data = await response.json();
        renderAmneziaSupport(data.awg_support || {});

        const profiles = data.profiles || [];
        if (profiles.length === 0) {
            container.innerHTML = '<div class="empty-state">Конфигураций пока нет. Вставьте ключ vpn:// выше.</div>';
            return;
        }

        const awgSupported = Boolean(data.awg_support && data.awg_support.supported);
        container.innerHTML = profiles.map(p => renderAmneziaProfile(p, awgSupported)).join('');
    } catch (error) {
        console.error('Ошибка загрузки конфигураций Amnezia:', error);
        container.innerHTML = '<div class="empty-state">Не удалось загрузить конфигурации</div>';
    }
}

function renderAmneziaSupport(support) {
    const note = document.getElementById('amneziaSupport');
    if (!note) return;

    if (support.supported) {
        note.className = 'happ-note';
        note.innerHTML = `✅ ${escapeHTML(support.version)} поддерживает AmneziaWG.`;
        return;
    }

    note.className = 'happ-note is-warn';

    const version = support.version ? escapeHTML(support.version) : 'sing-box';
    const reason = support.error ? ` (${escapeHTML(support.error)})` : '';

    note.innerHTML = `⚠️ ${version} не поддерживает AmneziaWG${reason}. WireGuard и Xray импортируются как обычно,
        а AmneziaWG сохранится, но не попадёт в конфиг: официальный sing-box не запустится с параметрами обфускации.
        Нужен форк <a href="https://github.com/Leadaxe/sing-box-lx" target="_blank" rel="noopener">sing-box-lx</a>.`;
}

function renderAmneziaProfile(profile, awgSupported) {
    const id = escapeHTML(profile.id);
    const items = profile.items || [];
    const warnings = profile.warnings || [];

    const premium = profile.premium;

    let statusDot = 'is-good';
    let statusText = 'Записано в конфиг';

    if (profile.last_error) {
        statusDot = 'is-bad';
        statusText = 'Ошибка обновления';
    } else if (!profile.synced && profile.requires_awg && !awgSupported) {
        statusDot = 'is-warn';
        statusText = 'Не в конфиге: нужен sing-box с AmneziaWG';
    } else if (!profile.synced) {
        statusDot = 'is-warn';
        statusText = 'Не в конфиге';
    } else if (profile.out_of_sync) {
        statusDot = 'is-warn';
        statusText = 'Серверы изменились, обновите конфиг';
    }

    const syncDisabled = profile.requires_awg && !awgSupported ? 'disabled title="Нужен sing-box с поддержкой AmneziaWG"' : '';
    const syncHighlight = !profile.synced || profile.out_of_sync;

    const rows = items.map(item => `
        <tr>
            <td>${escapeHTML(item.name)}</td>
            <td><span class="badge badge-${item.requires_awg ? 'group' : 'domain'}">${item.requires_awg ? 'AmneziaWG' : escapeHTML(item.protocol)}</span></td>
            <td>${escapeHTML(item.server)}</td>
        </tr>`).join('');

    return `
    <div class="add-form-card happ-card">
        <div class="happ-card-head">
            <div class="happ-card-title">
                <div class="form-header" style="margin-bottom: 4px;">${escapeHTML(profile.name)}</div>
                <div class="happ-card-sub">
                    <span class="stat-dot ${statusDot}"></span>
                    <span>${statusText}</span>
                    ${profile.server ? `<span class="happ-sep">·</span><span>${escapeHTML(profile.server)}</span>` : ''}
                    ${premium ? `<span class="happ-sep">·</span><span>обновлено ${formatHappAgo(profile.last_update)}</span>` : ''}
                </div>
            </div>
            <div class="happ-card-actions">
                ${premium ? `<button class="btn btn-secondary btn-sm" onclick="amneziaProfileAction('refresh', '${id}', this)">Обновить</button>` : ''}
                <button class="btn ${syncHighlight ? 'btn-warning' : 'btn-secondary'} btn-sm" onclick="amneziaProfileAction('sync', '${id}', this)" ${syncDisabled}>В конфиг</button>
                <button class="btn btn-danger btn-sm" onclick="deleteAmneziaProfile('${id}', this)">Удалить</button>
            </div>
        </div>

        ${profile.last_error ? `<div class="happ-note is-bad">${escapeHTML(profile.last_error)}</div>` : ''}
        ${premium ? renderAmneziaPremium(profile) : ''}
        ${warnings.length ? `<div class="happ-note">${warnings.map(escapeHTML).join('<br>')}</div>` : ''}

        <table class="table">
            <thead><tr><th>Протокол</th><th>Тип</th><th>Сервер</th></tr></thead>
            <tbody>${rows}</tbody>
        </table>
    </div>`;
}

// Форматирует дату шлюза Amnezia ("2026-10-06 12:54:28+00:00") и число оставшихся дней.
function formatAmneziaDate(value) {
    if (!value) return null;

    const date = new Date(value.replace(' ', 'T'));
    if (isNaN(date)) return { text: value, days: null };

    return {
        text: date.toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' }),
        days: Math.floor((date.getTime() - Date.now()) / 86400000),
    };
}

function renderAmneziaPremium(profile) {
    const premium = profile.premium;
    const id = escapeHTML(profile.id);
    const subscription = formatAmneziaDate(premium.subscription_end);
    const expires = formatAmneziaDate(premium.config_expires_at);

    const dateTile = (label, date, emptyText) => {
        if (!date) {
            return `<div class="stat-tile"><div class="stat-label">${label}</div><div class="stat-value happ-date">—</div><div class="stat-foot">${emptyText}</div></div>`;
        }

        let dot = 'is-good';
        let foot = '';

        if (date.days !== null) {
            dot = date.days < 3 ? 'is-bad' : date.days < 7 ? 'is-warn' : 'is-good';
            foot = date.days < 0 ? 'Истекло' : `Осталось ${date.days} ${pluralDays(date.days)}`;
        }

        return `
            <div class="stat-tile">
                <div class="stat-label">${label}</div>
                <div class="stat-value happ-date">${escapeHTML(date.text)}</div>
                <div class="stat-foot"><span class="stat-dot ${dot}"></span>${foot}</div>
            </div>`;
    };

    const countries = (premium.available_countries || []).map(country =>
        `<option value="${escapeHTML(country.code)}" ${country.code === premium.server_country_code ? 'selected' : ''}>${escapeHTML(country.name)}</option>`
    ).join('');

    return `
        <div class="stat-row happ-stats">
            ${dateTile('Подписка до', subscription, 'Нет данных')}
            ${dateTile('Конфигурация действует до', expires, 'Без срока')}
            <div class="stat-tile">
                <div class="stat-label">Страна сервера</div>
                ${countries
                    ? `<select class="form-select amnezia-country" onchange="setAmneziaCountry('${id}', this)">${countries}</select>`
                    : `<div class="stat-value happ-date">${escapeHTML(premium.server_country_name || '—')}</div>`}
                <div class="stat-foot">Смена страны запрашивает новую конфигурацию</div>
            </div>
        </div>`;
}

async function setAmneziaCountry(id, select) {
    select.disabled = true;
    amneziaBusy = true;

    try {
        const response = await fetch('/api/amnezia/profiles/country', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id, country: select.value })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Не удалось сменить страну');

        showMessage(`${result.message}. Нажмите «В конфиг», чтобы записать новый сервер.`, 'success');
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        select.disabled = false;
        amneziaBusy = false;
        await loadAmneziaProfiles();
    }
}

async function addAmneziaProfile(event) {
    event.preventDefault();

    const keyInput = document.getElementById('amneziaKey');
    const nameInput = document.getElementById('amneziaName');
    const button = document.getElementById('amneziaAddBtn');

    button.disabled = true;
    button.textContent = 'Импортирую...';
    amneziaBusy = true;

    try {
        const response = await fetch('/api/amnezia/profiles/add', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ key: keyInput.value.trim(), name: nameInput.value.trim() })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Не удалось импортировать ключ');

        showMessage(result.message, result.synced ? 'success' : 'error');
        keyInput.value = '';
        nameInput.value = '';

        if (result.synced) checkPendingConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        button.disabled = false;
        button.textContent = 'Импортировать';
        amneziaBusy = false;
        await loadAmneziaProfiles();
    }
}

async function amneziaProfileAction(action, id, button) {
    button.disabled = true;
    amneziaBusy = true;

    try {
        const response = await fetch(`/api/amnezia/profiles/${action}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ id })
        });
        const result = await response.json().catch(() => ({}));

        if (!response.ok) throw new Error(result.error || 'Действие не выполнено');

        showMessage(result.message, result.warning ? 'error' : 'success');

        if (action !== 'refresh') checkPendingConfig();
    } catch (error) {
        showMessage('Ошибка: ' + error.message, 'error');
    } finally {
        button.disabled = false;
        amneziaBusy = false;
        await loadAmneziaProfiles();
    }
}

async function deleteAmneziaProfile(id, button) {
    if (!confirm('Удалить конфигурацию и её серверы из конфига?\n\nДля Amnezia Premium освободится место устройства в подписке.')) return;

    await amneziaProfileAction('delete', id, button);
}
