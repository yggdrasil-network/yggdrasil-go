/**
 * Main JavaScript logic for Yggdrasil Web Interface
 * Handles language switching, theme management, notifications, and UI interactions
 */

// Global state variables
let currentLanguage = localStorage.getItem('yggdrasil-language') || 'ru';

// Export currentLanguage to window for access from other scripts
window.getCurrentLanguage = () => currentLanguage;
let currentTheme = localStorage.getItem('yggdrasil-theme') || 'light';

// Elements that should not be overwritten by translations when they contain data
const dataElements = [
    'node-key', 'node-version', 'routing-entries', 'node-address', 
    'node-subnet', 'peers-count', 'peers-online', 'footer-version'
];

/**
 * Check if an element contains actual data (not just loading text or empty)
 */
function hasDataContent(element) {
    const text = element.textContent.trim();
    const loadingTexts = ['Loading...', 'Загрузка...', 'N/A', '', 'unknown'];
    return !loadingTexts.includes(text);
}

/**
 * Update all text elements based on current language
 */
function updateTexts() {
    const elements = document.querySelectorAll('[data-key]');
    elements.forEach(element => {
        const key = element.getAttribute('data-key');
        const elementId = element.id;
        
        // Skip data elements that already have content loaded
        if (elementId && dataElements.includes(elementId) && hasDataContent(element)) {
            return;
        }
        
        if (window.translations && window.translations[currentLanguage] && window.translations[currentLanguage][key]) {
            // Special handling for footer_text which contains HTML
            if (key === 'footer_text') {
                // Save current version value if it exists
                const versionElement = document.getElementById('footer-version');
                const currentVersion = versionElement ? versionElement.textContent : '';
                
                // Update footer text
                element.innerHTML = window.translations[currentLanguage][key];
                
                // Restore version value if it was there
                if (currentVersion && currentVersion !== '' && currentVersion !== 'unknown') {
                    const newVersionElement = document.getElementById('footer-version');
                    if (newVersionElement) {
                        newVersionElement.textContent = currentVersion;
                    }
                }
            } else {
                element.textContent = window.translations[currentLanguage][key];
            }
        }
    });
    
    // Handle title translations
    const titleElements = document.querySelectorAll('[data-key-title]');
    titleElements.forEach(element => {
        const titleKey = element.getAttribute('data-key-title');
        if (window.translations && window.translations[currentLanguage] && window.translations[currentLanguage][titleKey]) {
            element.title = window.translations[currentLanguage][titleKey];
        }
    });
}

/**
 * Refresh displayed data after language change
 */
function refreshDataDisplay() {
    // If we have node info, refresh its display
    if (window.nodeInfo) {
        window.updateNodeInfoDisplay(window.nodeInfo);
    }
    
    // If we have peers data, refresh its display
    if (window.peersData) {
        window.updatePeersDisplay(window.peersData);
    }
}

/**
 * Toggle between light and dark theme
 */
function toggleTheme() {
    currentTheme = currentTheme === 'light' ? 'dark' : 'light';
    applyTheme();
    localStorage.setItem('yggdrasil-theme', currentTheme);
}

/**
 * Apply the current theme to the document
 */
function applyTheme() {
    document.documentElement.setAttribute('data-theme', currentTheme);
    
    // Update desktop theme button
    const themeBtn = document.getElementById('theme-btn');
    if (themeBtn) {
        const icon = themeBtn.querySelector('.theme-icon');
        if (icon) {
            icon.textContent = currentTheme === 'light' ? '🌙' : '☀️';
        }
    }
    
    // Update mobile theme button
    const themeBtnMobile = document.getElementById('theme-btn-mobile');
    if (themeBtnMobile) {
        const icon = themeBtnMobile.querySelector('.theme-icon');
        if (icon) {
            icon.textContent = currentTheme === 'light' ? '🌙' : '☀️';
        }
    }
}

/**
 * Switch application language
 * @param {string} lang - Language code (ru, en)
 */
function switchLanguage(lang) {
    currentLanguage = lang;
    localStorage.setItem('yggdrasil-language', lang);

    // Update button states for both desktop and mobile
    document.querySelectorAll('.lang-btn').forEach(btn => btn.classList.remove('active'));
    const desktopBtn = document.getElementById('lang-' + lang);
    const mobileBtn = document.getElementById('lang-' + lang + '-mobile');
    
    if (desktopBtn) desktopBtn.classList.add('active');
    if (mobileBtn) mobileBtn.classList.add('active');

    // Update all texts
    updateTexts();
    
    // Refresh data display to preserve loaded data
    refreshDataDisplay();
}

/**
 * Show a specific content section and hide others
 * @param {string} sectionName - Name of the section to show
 */
function showSection(sectionName) {
    // Hide all sections
    const sections = document.querySelectorAll('.content-section');
    sections.forEach(section => section.classList.remove('active'));

    // Remove active class from all nav items
    const navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(item => item.classList.remove('active'));

    // Show selected section
    const targetSection = document.getElementById(sectionName + '-section');
    if (targetSection) {
        targetSection.classList.add('active');
    }

    // Add active class to clicked nav item
    if (event && event.target) {
        event.target.closest('.nav-item').classList.add('active');
    }
}

/**
 * Logout function with modal confirmation
 */
function logout() {
    showConfirmModal({
        title: 'modal_confirm',
        message: 'logout_confirm',
        confirmText: 'modal_confirm_yes',
        cancelText: 'modal_cancel',
        type: 'danger',
        onConfirm: () => {
            // Clear stored preferences
            localStorage.removeItem('yggdrasil-language');
            localStorage.removeItem('yggdrasil-theme');
            
            // Redirect or refresh
            window.location.reload();
        }
    });
}

// Notification system
let notificationId = 0;

/**
 * Show a notification to the user
 * @param {string} message - Notification message
 * @param {string} type - Notification type (info, success, error, warning)
 * @param {string} title - Optional custom title
 * @param {number} duration - Auto-hide duration in milliseconds (0 = no auto-hide)
 * @returns {number} Notification ID
 */
function showNotification(message, type = 'info', title = null, duration = 5000) {
    const container = document.getElementById('notifications-container');
    const id = ++notificationId;

    const icons = {
        success: '✅',
        error: '❌',
        warning: '⚠️',
        info: 'ℹ️'
    };

    const titles = {
        success: window.translations[currentLanguage]['notification_success'] || 'Success',
        error: window.translations[currentLanguage]['notification_error'] || 'Error',
        warning: window.translations[currentLanguage]['notification_warning'] || 'Warning',
        info: window.translations[currentLanguage]['notification_info'] || 'Information'
    };

    const notification = document.createElement('div');
    notification.className = `notification ${type}`;
    notification.id = `notification-${id}`;

    notification.innerHTML = `
        <div class="notification-icon">${icons[type] || icons.info}</div>
        <div class="notification-content">
            <div class="notification-title">${title || titles[type]}</div>
            <div class="notification-message">${message}</div>
        </div>
        <button class="notification-close" onclick="removeNotification(${id})">&times;</button>
    `;

    container.appendChild(notification);

    // Auto remove after duration
    if (duration > 0) {
        setTimeout(() => {
            removeNotification(id);
        }, duration);
    }

    return id;
}

/**
 * Remove a notification by ID
 * @param {number} id - Notification ID to remove
 */
function removeNotification(id) {
    const notification = document.getElementById(`notification-${id}`);
    if (notification) {
        notification.classList.add('removing');
        setTimeout(() => {
            if (notification.parentNode) {
                notification.parentNode.removeChild(notification);
            }
        }, 300);
    }
}

/**
 * Show success notification
 * @param {string} message - Success message
 * @param {string} title - Optional custom title
 * @returns {number} Notification ID
 */
function showSuccess(message, title = null) {
    return showNotification(message, 'success', title);
}

/**
 * Show error notification
 * @param {string} message - Error message
 * @param {string} title - Optional custom title
 * @returns {number} Notification ID
 */
function showError(message, title = null) {
    return showNotification(message, 'error', title);
}

/**
 * Show warning notification
 * @param {string} message - Warning message
 * @param {string} title - Optional custom title
 * @returns {number} Notification ID
 */
function showWarning(message, title = null) {
    return showNotification(message, 'warning', title);
}

/**
 * Show info notification
 * @param {string} message - Info message
 * @param {string} title - Optional custom title
 * @returns {number} Notification ID
 */
function showInfo(message, title = null) {
    return showNotification(message, 'info', title);
}

/**
 * Initialize the application when DOM is loaded
 */
function initializeMain() {
    // Set active language button for both desktop and mobile
    const desktopBtn = document.getElementById('lang-' + currentLanguage);
    const mobileBtn = document.getElementById('lang-' + currentLanguage + '-mobile');
    
    if (desktopBtn) desktopBtn.classList.add('active');
    if (mobileBtn) mobileBtn.classList.add('active');
    
    // Update all texts
    updateTexts();
    
    // Apply saved theme
    applyTheme();
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', initializeMain); 