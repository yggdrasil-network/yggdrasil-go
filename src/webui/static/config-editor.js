// JSON Editor functionality for configuration

// Initialize JSON editor with basic syntax highlighting and features
function initJSONEditor() {
    const textarea = document.getElementById('config-json-textarea');
    const lineNumbersContainer = document.getElementById('line-numbers-container');
    const statusElement = document.getElementById('editor-status');
    const cursorElement = document.getElementById('cursor-position');
    
    if (!textarea) return;
    
    // Set initial content
    textarea.value = currentConfigJSON || '';
    
    // Update line numbers
    updateLineNumbers();
    
    // Always enable line numbers since toggle was removed
    const editorWrapper = document.querySelector('.editor-wrapper');
    if (editorWrapper) {
        editorWrapper.classList.add('with-line-numbers');
    }
    
    // Add event listeners
    textarea.addEventListener('input', function() {
        updateLineNumbers();
        updateEditorStatus();
        onConfigChange();
    });
    
    textarea.addEventListener('scroll', syncLineNumbers);
    textarea.addEventListener('keydown', handleEditorKeydown);
    textarea.addEventListener('selectionchange', updateCursorPosition);
    textarea.addEventListener('click', updateCursorPosition);
    textarea.addEventListener('keyup', updateCursorPosition);
    
    // Initial status update
    updateEditorStatus();
    updateCursorPosition();
}

// Update line numbers
function updateLineNumbers() {
    const textarea = document.getElementById('config-json-textarea');
    const lineNumbersContainer = document.getElementById('line-numbers-container');
    
    if (!textarea || !lineNumbersContainer) return;
    
    const lines = textarea.value.split('\n');
    const lineNumbers = lines.map((_, index) => 
        `<span class="line-number">${index + 1}</span>`
    ).join('');
    
    lineNumbersContainer.innerHTML = lineNumbers;
}

// Sync line numbers scroll with textarea
function syncLineNumbers() {
    const textarea = document.getElementById('config-json-textarea');
    const lineNumbersContainer = document.getElementById('line-numbers-container');
    
    if (!textarea || !lineNumbersContainer) return;
    
    lineNumbersContainer.scrollTop = textarea.scrollTop;
}



// Handle special editor keydown events
function handleEditorKeydown(event) {
    const textarea = event.target;
    
    // Tab handling for indentation
    if (event.key === 'Tab') {
        event.preventDefault();
        const start = textarea.selectionStart;
        const end = textarea.selectionEnd;
        
        if (event.shiftKey) {
            // Remove indentation
            const beforeCursor = textarea.value.substring(0, start);
            const lineStart = beforeCursor.lastIndexOf('\n') + 1;
            const currentLine = textarea.value.substring(lineStart, end);
            
            if (currentLine.startsWith('  ')) {
                textarea.value = textarea.value.substring(0, lineStart) + 
                               currentLine.substring(2) + 
                               textarea.value.substring(end);
                textarea.selectionStart = Math.max(lineStart, start - 2);
                textarea.selectionEnd = end - 2;
            }
        } else {
            // Add indentation
            textarea.value = textarea.value.substring(0, start) + 
                           '  ' + 
                           textarea.value.substring(end);
            textarea.selectionStart = start + 2;
            textarea.selectionEnd = start + 2;
        }
        
        updateLineNumbers();
        updateEditorStatus();
    }
    
    // Auto-closing brackets and quotes
    if (event.key === '{') {
        insertMatchingCharacter(textarea, '{', '}');
        event.preventDefault();
    } else if (event.key === '[') {
        insertMatchingCharacter(textarea, '[', ']');
        event.preventDefault();
    } else if (event.key === '"') {
        insertMatchingCharacter(textarea, '"', '"');
        event.preventDefault();
    }
}

// Insert matching character (auto-closing)
function insertMatchingCharacter(textarea, open, close) {
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    const selectedText = textarea.value.substring(start, end);
    
    textarea.value = textarea.value.substring(0, start) + 
                   open + selectedText + close + 
                   textarea.value.substring(end);
    
    if (selectedText) {
        textarea.selectionStart = start + 1;
        textarea.selectionEnd = start + 1 + selectedText.length;
    } else {
        textarea.selectionStart = start + 1;
        textarea.selectionEnd = start + 1;
    }
}

// Update editor status (validation)
function updateEditorStatus() {
    const textarea = document.getElementById('config-json-textarea');
    const statusElement = document.getElementById('editor-status');
    
    if (!textarea || !statusElement) return;
    
    try {
        if (textarea.value.trim() === '') {
            statusElement.innerHTML = '<span data-key="empty_config">Пустая конфигурация</span>';
            statusElement.className = 'status-text warning';
            return;
        }
        
        JSON.parse(textarea.value);
        statusElement.innerHTML = '<span data-key="valid_json"></span>';
        statusElement.className = 'status-text success';
    } catch (error) {
        statusElement.textContent = `Ошибка JSON: ${error.message}`;
        statusElement.className = 'status-text error';
    }
}

// Update cursor position display
function updateCursorPosition() {
    const textarea = document.getElementById('config-json-textarea');
    const cursorElement = document.getElementById('cursor-position');
    
    if (!textarea || !cursorElement) return;
    
    const cursorPos = textarea.selectionStart;
    const beforeCursor = textarea.value.substring(0, cursorPos);
    const line = beforeCursor.split('\n').length;
    const column = beforeCursor.length - beforeCursor.lastIndexOf('\n');
    
    cursorElement.innerHTML = `<span data-key="line">Строка</span> ${line}, <span data-key="column">Столбец</span> ${column}`;
}

// Format JSON with proper indentation
function formatJSON() {
    const textarea = document.getElementById('config-json-textarea');
    
    if (!textarea) return;
    
    try {
        const parsed = JSON.parse(textarea.value);
        const formatted = JSON.stringify(parsed, null, 2);
        textarea.value = formatted;
        updateLineNumbers();
        updateEditorStatus();
        const formattedMessage = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['json_formatted'] 
            ? window.translations[currentLanguage]['json_formatted'] 
            : 'JSON отформатирован';
        showNotification(formattedMessage, 'success');
    } catch (error) {
        showNotification(`Ошибка форматирования: ${error.message}`, 'error');
    }
}

// Validate JSON configuration
function validateJSON() {
    const textarea = document.getElementById('config-json-textarea');
    
    if (!textarea) return;
    
    try {
        JSON.parse(textarea.value);
        const validationMessage = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['json_validation_success'] 
            ? window.translations[currentLanguage]['json_validation_success'] 
            : 'JSON конфигурация валидна';
        showNotification(validationMessage, 'success');
    } catch (error) {
        showNotification(`Ошибка валидации JSON: ${error.message}`, 'error');
    }
}

// Save configuration
async function saveConfiguration(restart = false) {
    const textarea = document.getElementById('config-json-textarea');
    
    if (!textarea) {
        showNotification('Редактор не найден', 'error');
        return;
    }
    
    // Validate JSON before saving
    try {
        JSON.parse(textarea.value);
    } catch (error) {
        showNotification(`Невозможно сохранить: ${error.message}`, 'error');
        return;
    }
    
    try {
        const response = await fetch('/api/config/set', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            credentials: 'same-origin',
            body: JSON.stringify({
                config_json: textarea.value,
                restart: restart
            })
        });
        
        const result = await response.json();
        
        if (result.success) {
            currentConfigJSON = textarea.value;
            if (restart) {
                const restartMessage = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['config_saved_restarting'] 
                    ? window.translations[currentLanguage]['config_saved_restarting'] 
                    : 'Конфигурация сохранена. Сервер перезапускается...';
                showNotification(restartMessage, 'success');
            } else {
                const successMessage = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['config_saved_success'] 
                    ? window.translations[currentLanguage]['config_saved_success'] 
                    : 'Конфигурация сохранена успешно';
                showNotification(successMessage, 'success');
            }
            onConfigChange(); // Update button states
        } else {
            showNotification(`Ошибка сохранения: ${result.message}`, 'error');
        }
    } catch (error) {
        console.error('Error saving configuration:', error);
        showNotification(`Ошибка сохранения: ${error.message}`, 'error');
    }
}

// Save configuration and restart server
async function saveAndRestartConfiguration() {
    const title = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['save_and_restart_title'] 
        ? window.translations[currentLanguage]['save_and_restart_title'] 
        : 'Сохранить и перезапустить';
    
    const message = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['save_and_restart_message'] 
        ? window.translations[currentLanguage]['save_and_restart_message'] 
        : 'Сохранить конфигурацию и перезапустить сервер?\n\nВнимание: Соединение будет прервано на время перезапуска.';
    
    showConfirmModal({
        title: title,
        message: message,
        type: 'danger',
        onConfirm: async () => {
            await saveConfiguration(true);
        }
    });
}

// Refresh configuration from server
async function refreshConfiguration() {
    const textarea = document.getElementById('config-json-textarea');
    
    // Check if there are unsaved changes
    if (textarea && textarea.value !== currentConfigJSON) {
        const title = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['refresh_unsaved_changes_title'] 
            ? window.translations[currentLanguage]['refresh_unsaved_changes_title'] 
            : 'Несохраненные изменения';
        
        const message = window.translations && window.translations[currentLanguage] && window.translations[currentLanguage]['refresh_unsaved_changes_message'] 
            ? window.translations[currentLanguage]['refresh_unsaved_changes_message'] 
            : 'У вас есть несохраненные изменения. Продолжить обновление?';
        
        showConfirmModal({
            title: title,
            message: message,
            type: 'warning',
            onConfirm: async () => {
                try {
                    await loadConfiguration();
                    showNotification('Конфигурация обновлена', 'success');
                } catch (error) {
                    showNotification('Ошибка обновления конфигурации', 'error');
                }
            }
        });
        return;
    }
    

}

// Update configuration status
function updateConfigStatus() {
    // This function can be used to show additional status information
    // Currently handled by updateEditorStatus
}

// Handle configuration changes
function onConfigChange() {
    // Mark configuration as modified
    const saveBtn = document.querySelector('.save-btn');
    const restartBtn = document.querySelector('.restart-btn');
    
    const textarea = document.getElementById('config-json-textarea');
    const hasChanges = textarea && textarea.value !== currentConfigJSON;
    
    if (saveBtn) {
        saveBtn.style.fontWeight = hasChanges ? 'bold' : 'normal';
    }
    if (restartBtn) {
        restartBtn.style.fontWeight = hasChanges ? 'bold' : 'normal';
    }
}