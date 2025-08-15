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
            format: data.config_format
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
                    </div>
                </div>
            </div>
            
            <div class="config-editor-container">
                <div class="config-json-editor">
                    <div class="editor-header">
                        <span class="editor-title" data-key="json_configuration">JSON Конфигурация</span>
                        <div class="editor-controls">
                            <div class="action-buttons-group">
                                <div onclick="refreshConfiguration()" class="action-btn">
                                    <span data-key="refresh">Обновить</span>
                                </div>
                                <div onclick="formatJSON()" class="action-btn">
                                    <span data-key="format">Форматировать</span>
                                </div>
                                <div onclick="validateJSON()" class="action-btn">
                                    <span data-key="validate">Проверить</span>
                                </div>
                                <div onclick="saveConfiguration()" class="action-btn">
                                    <span data-key="save_config">Сохранить</span>
                                </div>
                                <div onclick="saveAndRestartConfiguration()" class="action-btn">
                                    <span data-key="save_and_restart">Сохранить и перезапустить</span>
                                </div>
                            </div>
                        </div>
                    </div>
                    <div class="editor-wrapper">
                        <div class="line-numbers" id="line-numbers-container"></div>
                        <textarea 
                            id="config-json-textarea" 
                            class="json-editor" 
                            spellcheck="false"
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