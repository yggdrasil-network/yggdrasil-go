// Configuration management functions

let currentConfig = null;
let configMeta = null;

// Initialize config section
async function initConfigSection() {
    try {
        await loadConfiguration();
    } catch (error) {
        console.error('Failed to load configuration:', error);
        showNotification('Ошибка загрузки конфигурации', 'error');
    }
}

// Load current configuration
async function loadConfiguration() {
    try {
        const response = await fetch('/api/config/get', {
            method: 'GET',
            credentials: 'same-origin'
        });
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        currentConfig = data.config_data;
        configMeta = {
            path: data.config_path,
            format: data.config_format,
            isWritable: data.is_writable
        };
        
        renderConfigEditor();
        updateConfigStatus();
    } catch (error) {
        console.error('Error loading configuration:', error);
        throw error;
    }
}

// Render configuration editor
function renderConfigEditor() {
    const configSection = document.getElementById('config-section');
    
    const configEditor = `
        <div class="config-container">
            <div class="config-header">
                <div class="config-info">
                    <h3 data-key="configuration_file">Файл конфигурации</h3>
                    <div class="config-meta">
                        <span class="config-path" title="${configMeta.path}">${configMeta.path}</span>
                        <span class="config-format ${configMeta.format}">${configMeta.format.toUpperCase()}</span>
                        <span class="config-status ${configMeta.isWritable ? 'writable' : 'readonly'}">
                            ${configMeta.isWritable ? '✏️ Редактируемый' : '🔒 Только чтение'}
                        </span>
                    </div>
                </div>
                <div class="config-actions">
                    <button onclick="refreshConfiguration()" class="action-btn refresh-btn" data-key="refresh">
                        🔄 Обновить
                    </button>
                    ${configMeta.isWritable ? `
                        <button onclick="saveConfiguration()" class="action-btn save-btn" data-key="save_config">
                            💾 Сохранить
                        </button>
                    ` : ''}
                </div>
            </div>
            
            <div class="config-editor-container">
                <div class="config-groups">
                    ${renderConfigGroups()}
                </div>
            </div>
        </div>
    `;
    
    configSection.innerHTML = configEditor;
    updateTexts();
}

// Render configuration groups
function renderConfigGroups() {
    const groups = [
        {
            key: 'network',
            title: 'Сетевые настройки',
            fields: ['Peers', 'InterfacePeers', 'Listen', 'AllowedPublicKeys']
        },
        {
            key: 'identity',
            title: 'Идентификация',
            fields: ['PrivateKey', 'PrivateKeyPath']
        },
        {
            key: 'interface',
            title: 'Сетевой интерфейс',
            fields: ['IfName', 'IfMTU']
        },
        {
            key: 'multicast',
            title: 'Multicast',
            fields: ['MulticastInterfaces']
        },
        {
            key: 'admin',
            title: 'Администрирование',
            fields: ['AdminListen']
        },
        {
            key: 'webui',
            title: 'Веб-интерфейс',
            fields: ['WebUI']
        },
        {
            key: 'nodeinfo',
            title: 'Информация об узле',
            fields: ['NodeInfo', 'NodeInfoPrivacy', 'LogLookups']
        }
    ];

    return groups.map(group => `
        <div class="config-group">
            <div class="config-group-header" onclick="toggleConfigGroup('${group.key}')">
                <h4>${group.title}</h4>
                <span class="toggle-icon">▼</span>
            </div>
            <div class="config-group-content" id="config-group-${group.key}">
                ${group.fields.map(field => renderConfigField(field)).join('')}
            </div>
        </div>
    `).join('');
}

// Render individual config field
function renderConfigField(fieldName) {
    const value = currentConfig[fieldName];
    const fieldType = getFieldType(fieldName, value);
    const fieldDescription = getFieldDescription(fieldName);
    
    return `
        <div class="config-field">
            <div class="config-field-header">
                <label for="config-${fieldName}">${fieldName}</label>
                <span class="field-type">${fieldType}</span>
            </div>
            <div class="config-field-description">${fieldDescription}</div>
            <div class="config-field-input">
                ${renderConfigInput(fieldName, value, fieldType)}
            </div>
        </div>
    `;
}

// Render config input based on type
function renderConfigInput(fieldName, value, fieldType) {
    switch (fieldType) {
        case 'boolean':
            return `
                <label class="switch">
                    <input type="checkbox" id="config-${fieldName}" 
                           ${value ? 'checked' : ''} 
                           onchange="updateConfigValue('${fieldName}', this.checked)">
                    <span class="slider"></span>
                </label>
            `;
        
        case 'number':
            return `
                <input type="number" id="config-${fieldName}" 
                       value="${value || ''}" 
                       onchange="updateConfigValue('${fieldName}', parseInt(this.value) || 0)"
                       class="config-input">
            `;
            
        case 'string':
            return `
                <input type="text" id="config-${fieldName}" 
                       value="${value || ''}" 
                       onchange="updateConfigValue('${fieldName}', this.value)"
                       class="config-input">
            `;
            
        case 'array':
            return `
                <div class="array-input">
                    <textarea id="config-${fieldName}" 
                              rows="4" 
                              onchange="updateConfigArrayValue('${fieldName}', this.value)"
                              class="config-textarea">${Array.isArray(value) ? value.join('\n') : ''}</textarea>
                    <small>Одно значение на строку</small>
                </div>
            `;
            
        case 'object':
            return `
                <div class="object-input">
                    <textarea id="config-${fieldName}" 
                              rows="6" 
                              onchange="updateConfigObjectValue('${fieldName}', this.value)"
                              class="config-textarea">${JSON.stringify(value, null, 2)}</textarea>
                    <small>JSON формат</small>
                </div>
            `;
            
        case 'private_key':
            return `
                <div class="private-key-input">
                    <input type="password" id="config-${fieldName}" 
                           value="${value ? '••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••' : ''}" 
                           readonly
                           class="config-input private-key">
                    <small>Приватный ключ (только для чтения)</small>
                </div>
            `;
            
        default:
            return `
                <textarea id="config-${fieldName}" 
                          rows="3" 
                          onchange="updateConfigValue('${fieldName}', this.value)"
                          class="config-textarea">${JSON.stringify(value, null, 2)}</textarea>
            `;
    }
}

// Get field type for rendering
function getFieldType(fieldName, value) {
    if (fieldName === 'PrivateKey') return 'private_key';
    if (fieldName.includes('MTU') || fieldName === 'Port') return 'number';
    if (typeof value === 'boolean') return 'boolean';
    if (typeof value === 'number') return 'number';
    if (typeof value === 'string') return 'string';
    if (Array.isArray(value)) return 'array';
    if (typeof value === 'object') return 'object';
    return 'string';
}

// Get field description
function getFieldDescription(fieldName) {
    const descriptions = {
        'Peers': 'Список исходящих peer соединений (например: tls://адрес:порт)',
        'InterfacePeers': 'Peer соединения по интерфейсам',
        'Listen': 'Адреса для входящих соединений',
        'AllowedPublicKeys': 'Разрешенные публичные ключи для входящих соединений',
        'PrivateKey': 'Приватный ключ узла (НЕ ПЕРЕДАВАЙТЕ НИКОМУ!)',
        'PrivateKeyPath': 'Путь к файлу с приватным ключом в формате PEM',
        'IfName': 'Имя TUN интерфейса ("auto" для автовыбора, "none" для отключения)',
        'IfMTU': 'Максимальный размер передаваемого блока (MTU) для TUN интерфейса',
        'MulticastInterfaces': 'Настройки multicast интерфейсов для обнаружения peers',
        'AdminListen': 'Адрес для подключения админского интерфейса',
        'WebUI': 'Настройки веб-интерфейса',
        'NodeInfo': 'Дополнительная информация об узле (видна всей сети)',
        'NodeInfoPrivacy': 'Скрыть информацию о платформе и версии',
        'LogLookups': 'Логировать поиск peers и узлов'
    };
    
    return descriptions[fieldName] || 'Параметр конфигурации';
}

// Update config value
function updateConfigValue(fieldName, value) {
    if (currentConfig) {
        currentConfig[fieldName] = value;
        markConfigAsModified();
    }
}

// Update array config value
function updateConfigArrayValue(fieldName, value) {
    if (currentConfig) {
        const lines = value.split('\n').filter(line => line.trim() !== '');
        currentConfig[fieldName] = lines;
        markConfigAsModified();
    }
}

// Update object config value
function updateConfigObjectValue(fieldName, value) {
    if (currentConfig) {
        try {
            currentConfig[fieldName] = JSON.parse(value);
            markConfigAsModified();
        } catch (error) {
            console.error('Invalid JSON for field', fieldName, ':', error);
        }
    }
}

// Mark config as modified
function markConfigAsModified() {
    const saveButton = document.querySelector('.save-btn');
    if (saveButton) {
        saveButton.classList.add('modified');
    }
}

// Toggle config group
function toggleConfigGroup(groupKey) {
    const content = document.getElementById(`config-group-${groupKey}`);
    const icon = content.parentNode.querySelector('.toggle-icon');
    
    if (content.style.display === 'none') {
        content.style.display = 'block';
        icon.textContent = '▼';
    } else {
        content.style.display = 'none';
        icon.textContent = '▶';
    }
}

// Update config status display
function updateConfigStatus() {
    // This function could show config validation status, etc.
}

// Refresh configuration
async function refreshConfiguration() {
    try {
        await loadConfiguration();
        showNotification('Конфигурация обновлена', 'success');
    } catch (error) {
        showNotification('Ошибка обновления конфигурации', 'error');
    }
}

// Save configuration
async function saveConfiguration() {
    if (!configMeta.isWritable) {
        showNotification('Файл конфигурации доступен только для чтения', 'error');
        return;
    }

    showModal({
        title: 'config_save_confirm_title',
        content: `
            <div class="save-config-confirmation">
                <p data-key="config_save_confirm_text">Вы уверены, что хотите сохранить изменения в конфигурационный файл?</p>
                <div class="config-save-info">
                    <p><strong>Файл:</strong> ${configMeta.path}</p>
                    <p><strong>Формат:</strong> ${configMeta.format.toUpperCase()}</p>
                    <p><strong data-key="config_backup_info">Резервная копия:</strong> Будет создана автоматически</p>
                </div>
                <div class="warning">
                    <span data-key="config_warning">⚠️ Внимание: Неправильная конфигурация может привести к сбою работы узла!</span>
                </div>
            </div>
        `,
        buttons: [
            {
                text: 'modal_cancel',
                type: 'secondary',
                action: 'close'
            },
            {
                text: 'save_config',
                type: 'danger',
                callback: () => {
                    confirmSaveConfiguration();
                    return true; // Close modal
                }
            }
        ]
    });
}

// Confirm and perform save
async function confirmSaveConfiguration() {
    
    try {
        const response = await fetch('/api/config/set', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                config_data: currentConfig,
                config_path: configMeta.path,
                format: configMeta.format
            })
        });
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        
        if (data.success) {
            showNotification(`Конфигурация сохранена: ${data.config_path}`, 'success');
            if (data.backup_path) {
                showNotification(`Резервная копия: ${data.backup_path}`, 'info');
            }
            
            // Remove modified indicator
            const saveButton = document.querySelector('.save-btn');
            if (saveButton) {
                saveButton.classList.remove('modified');
            }
        } else {
            showNotification(`Ошибка сохранения: ${data.message}`, 'error');
        }
    } catch (error) {
        console.error('Error saving configuration:', error);
        showNotification('Ошибка сохранения конфигурации', 'error');
    }
} 