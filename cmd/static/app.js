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
