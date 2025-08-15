// Configuration management functions

let currentConfigJSON = null;
let configMeta = null;
let configEditor = null;

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
        currentConfigJSON = data.config_json;
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

// Render configuration editor with JSON syntax highlighting
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
                    <button onclick="formatJSON()" class="action-btn format-btn" data-key="format">
                        📝 Форматировать
                    </button>
                    <button onclick="validateJSON()" class="action-btn validate-btn" data-key="validate">
                        ✓ Проверить
                    </button>
                    ${configMeta.isWritable ? `
                        <button onclick="saveConfiguration()" class="action-btn save-btn" data-key="save_config">
                            💾 Сохранить
                        </button>
                        <button onclick="saveAndRestartConfiguration()" class="action-btn restart-btn" data-key="save_and_restart">
                            🔄 Сохранить и перезапустить
                        </button>
                    ` : ''}
                </div>
            </div>
            
            <div class="config-editor-container">
                <div class="config-json-editor">
                    <div class="editor-header">
                        <span class="editor-title" data-key="json_configuration">JSON Конфигурация</span>
                        <div class="editor-controls">
                            <span class="line-numbers-toggle">
                                <input type="checkbox" id="line-numbers" checked onchange="toggleLineNumbers()">
                                <label for="line-numbers" data-key="line_numbers">Номера строк</label>
                            </span>
                        </div>
                    </div>
                    <div class="editor-wrapper">
                        <div class="line-numbers" id="line-numbers-container"></div>
                        <textarea 
                            id="config-json-textarea" 
                            class="json-editor" 
                            spellcheck="false"
                            ${configMeta.isWritable ? '' : 'readonly'}
                            placeholder="Загрузка конфигурации..."
                            oninput="onConfigChange()"
                            onscroll="syncLineNumbers()"
                        >${currentConfigJSON || ''}</textarea>
                    </div>
                    <div class="editor-status">
                        <span id="editor-status" class="status-text"></span>
                        <span id="cursor-position" class="cursor-position"></span>
                    </div>
                </div>
            </div>
        </div>
    `;
    
    configSection.innerHTML = configEditor;
    updateTexts();
    initJSONEditor();
}