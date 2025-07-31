/**
 * Modal System for Yggdrasil Web Interface
 * Provides flexible modal dialogs with multiple action buttons and input forms
 */

// Global modal state
let currentModal = null;
let modalCallbacks = {};

/**
 * Show a modal dialog
 * @param {Object} options - Modal configuration
 * @param {string} options.title - Modal title (supports localization key)
 * @param {string|HTMLElement} options.content - Modal content (supports localization key)
 * @param {Array} options.buttons - Array of button configurations
 * @param {Array} options.inputs - Array of input field configurations
 * @param {Function} options.onClose - Callback when modal is closed
 * @param {boolean} options.closable - Whether modal can be closed by clicking overlay or X button
 * @param {string} options.size - Modal size: 'small', 'medium', 'large'
 */
function showModal(options = {}) {
    const {
        title = 'Modal',
        content = '',
        buttons = [{ text: 'modal_close', type: 'secondary', action: 'close' }],
        inputs = [],
        onClose = null,
        closable = true,
        size = 'medium'
    } = options;

    const overlay = document.getElementById('modal-overlay');
    const container = document.getElementById('modal-container');
    const titleElement = document.getElementById('modal-title');
    const contentElement = document.getElementById('modal-content');
    const footerElement = document.getElementById('modal-footer');
    const closeBtn = document.getElementById('modal-close-btn');

    if (!overlay || !container) {
        console.error('Modal elements not found in DOM');
        return;
    }

    // Set modal size
    container.className = `modal-container modal-${size}`;

    // Set title (with localization support)
    titleElement.textContent = getLocalizedText(title);

    // Set content
    if (typeof content === 'string') {
        contentElement.innerHTML = `<p>${getLocalizedText(content)}</p>`;
    } else if (content instanceof HTMLElement) {
        contentElement.innerHTML = '';
        contentElement.appendChild(content);
    } else {
        contentElement.innerHTML = content;
    }

    // Add input fields if provided
    if (inputs && inputs.length > 0) {
        const formContainer = document.createElement('div');
        formContainer.className = 'modal-form-container';
        
        inputs.forEach((input, index) => {
            const formGroup = createFormGroup(input, index);
            formContainer.appendChild(formGroup);
        });
        
        contentElement.appendChild(formContainer);
    }

    // Create buttons
    footerElement.innerHTML = '';
    buttons.forEach((button, index) => {
        const btn = createModalButton(button, index);
        footerElement.appendChild(btn);
    });

    // Configure close button
    closeBtn.style.display = closable ? 'flex' : 'none';
    
    // Set up event handlers
    modalCallbacks.onClose = onClose;
    
    // Close on overlay click (if closable)
    if (closable) {
        overlay.onclick = (e) => {
            if (e.target === overlay) {
                closeModal();
            }
        };
    } else {
        overlay.onclick = null;
    }

    // Close on Escape key (if closable)
    if (closable) {
        document.addEventListener('keydown', handleEscapeKey);
    }

    // Show modal
    currentModal = options;
    overlay.classList.add('show');
    
    // Focus first input if available
    setTimeout(() => {
        const firstInput = contentElement.querySelector('.modal-form-input');
        if (firstInput) {
            firstInput.focus();
        }
    }, 100);
}

/**
 * Close the current modal
 */
function closeModal() {
    const overlay = document.getElementById('modal-overlay');
    if (!overlay) return;

    overlay.classList.remove('show');
    
    // Clean up event listeners
    document.removeEventListener('keydown', handleEscapeKey);
    overlay.onclick = null;
    
    // Call onClose callback if provided
    if (modalCallbacks.onClose) {
        modalCallbacks.onClose();
    }
    
    // Clear state
    currentModal = null;
    modalCallbacks = {};
}

/**
 * Create a form group element
 */
function createFormGroup(input, index) {
    const {
        type = 'text',
        name = `input_${index}`,
        label = '',
        placeholder = '',
        value = '',
        required = false,
        help = '',
        options = [] // for select inputs
    } = input;

    const formGroup = document.createElement('div');
    formGroup.className = 'modal-form-group';

    // Create label
    if (label) {
        const labelElement = document.createElement('label');
        labelElement.className = 'modal-form-label';
        labelElement.textContent = getLocalizedText(label);
        labelElement.setAttribute('for', name);
        formGroup.appendChild(labelElement);
    }

    // Create input element
    let inputElement;
    
    if (type === 'textarea') {
        inputElement = document.createElement('textarea');
        inputElement.className = 'modal-form-textarea';
    } else if (type === 'select') {
        inputElement = document.createElement('select');
        inputElement.className = 'modal-form-select';
        
        // Add options
        options.forEach(option => {
            const optionElement = document.createElement('option');
            optionElement.value = option.value || option;
            optionElement.textContent = getLocalizedText(option.text || option);
            if (option.selected || option.value === value) {
                optionElement.selected = true;
            }
            inputElement.appendChild(optionElement);
        });
    } else {
        inputElement = document.createElement('input');
        inputElement.type = type;
        inputElement.className = 'modal-form-input';
        inputElement.value = value;
    }

    inputElement.name = name;
    inputElement.id = name;
    
    if (placeholder) {
        inputElement.placeholder = getLocalizedText(placeholder);
    }
    
    if (required) {
        inputElement.required = true;
    }

    formGroup.appendChild(inputElement);

    // Create help text
    if (help) {
        const helpElement = document.createElement('div');
        helpElement.className = 'modal-form-help';
        helpElement.textContent = getLocalizedText(help);
        formGroup.appendChild(helpElement);
    }

    return formGroup;
}

/**
 * Create a modal button
 */
function createModalButton(button, index) {
    const {
        text = 'Button',
        type = 'secondary',
        action = null,
        callback = null,
        disabled = false
    } = button;

    const btn = document.createElement('button');
    btn.className = `modal-btn modal-btn-${type}`;
    btn.textContent = getLocalizedText(text);
    btn.disabled = disabled;

    btn.onclick = () => {
        if (action === 'close') {
            closeModal();
        } else if (callback) {
            const formData = getModalFormData();
            const result = callback(formData);
            
            // If callback returns false, don't close modal
            if (result !== false) {
                closeModal();
            }
        }
    };

    return btn;
}

/**
 * Get form data from modal inputs
 */
function getModalFormData() {
    const formData = {};
    const inputs = document.querySelectorAll('#modal-content .modal-form-input, #modal-content .modal-form-textarea, #modal-content .modal-form-select');
    
    inputs.forEach(input => {
        formData[input.name] = input.value;
    });
    
    return formData;
}

/**
 * Handle Escape key press
 */
function handleEscapeKey(e) {
    if (e.key === 'Escape' && currentModal) {
        closeModal();
    }
}

/**
 * Get localized text or return original if not found
 */
function getLocalizedText(key) {
    if (typeof key !== 'string') return key;
    
    const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
    
    if (window.translations && 
        window.translations[currentLang] && 
        window.translations[currentLang][key]) {
        return window.translations[currentLang][key];
    }
    
    return key;
}

// Convenience functions for common modal types

/**
 * Show a confirmation dialog
 */
function showConfirmModal(options = {}) {
    const {
        title = 'modal_confirm',
        message = 'modal_confirm_message',
        confirmText = 'modal_confirm_yes',
        cancelText = 'modal_cancel',
        onConfirm = null,
        onCancel = null,
        type = 'danger' // danger, primary, success
    } = options;

    showModal({
        title,
        content: message,
        closable: true,
        buttons: [
            {
                text: cancelText,
                type: 'secondary',
                action: 'close',
                callback: onCancel
            },
            {
                text: confirmText,
                type: type,
                callback: () => {
                    if (onConfirm) onConfirm();
                    return true; // Close modal
                }
            }
        ]
    });
}

/**
 * Show an alert dialog
 */
function showAlertModal(options = {}) {
    const {
        title = 'modal_alert',
        message = '',
        buttonText = 'modal_ok',
        type = 'primary',
        onClose = null
    } = options;

    showModal({
        title,
        content: message,
        closable: true,
        onClose,
        buttons: [
            {
                text: buttonText,
                type: type,
                action: 'close'
            }
        ]
    });
}

/**
 * Show a prompt dialog with input
 */
function showPromptModal(options = {}) {
    const {
        title = 'modal_input',
        message = '',
        inputLabel = '',
        inputPlaceholder = '',
        inputValue = '',
        inputType = 'text',
        inputRequired = true,
        confirmText = 'modal_ok',
        cancelText = 'modal_cancel',
        onConfirm = null,
        onCancel = null
    } = options;

    showModal({
        title,
        content: message,
        closable: true,
        inputs: [
            {
                type: inputType,
                name: 'input_value',
                label: inputLabel,
                placeholder: inputPlaceholder,
                value: inputValue,
                required: inputRequired
            }
        ],
        buttons: [
            {
                text: cancelText,
                type: 'secondary',
                action: 'close',
                callback: onCancel
            },
            {
                text: confirmText,
                type: 'primary',
                callback: (formData) => {
                    if (inputRequired && !formData.input_value.trim()) {
                        return false; // Don't close modal
                    }
                    if (onConfirm) onConfirm(formData.input_value);
                    return true; // Close modal
                }
            }
        ]
    });
}

// Export functions to global scope
window.showModal = showModal;
window.closeModal = closeModal;
window.showConfirmModal = showConfirmModal;
window.showAlertModal = showAlertModal;
window.showPromptModal = showPromptModal; 