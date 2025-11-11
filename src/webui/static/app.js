/**
 * Yggdrasil WebUI Application Logic
 * Integrates admin API with the user interface
 */

// Global state - expose to window for access from other scripts
window.nodeInfo = null;
window.peersData = null;
let isLoading = false;
let isLoadingNodeInfo = false;
let isLoadingPeers = false;



/**
 * Load and display node information
 */
async function loadNodeInfo() {
    if (isLoadingNodeInfo) {
        console.log('Node info request already in progress, skipping...');
        return window.nodeInfo;
    }
    
    try {
        isLoadingNodeInfo = true;
        const info = await window.yggAPI.getSelf();
        window.nodeInfo = info;
        updateNodeInfoDisplay(info);
        return info;
    } catch (error) {
        console.error('Failed to load node info:', error);
        showError('Failed to load node information: ' + error.message);
        throw error;
    } finally {
        isLoadingNodeInfo = false;
    }
}

/**
 * Load and display peers information
 */
async function loadPeers() {
    if (isLoadingPeers) {
        console.log('Peers request already in progress, skipping...');
        return window.peersData;
    }
    
    try {
        isLoadingPeers = true;
        const data = await window.yggAPI.getPeers();
        window.peersData = data;
        updatePeersDisplay(data);
        return data;
    } catch (error) {
        console.error('Failed to load peers:', error);
        showError('Failed to load peers information: ' + error.message);
        throw error;
    } finally {
        isLoadingPeers = false;
    }
}

/**
 * Update node information in the UI
 */
function updateNodeInfoDisplay(info) {
    // Update status section elements
    updateElementText('node-address', info.address || 'N/A');
    updateElementText('node-subnet', info.subnet || 'N/A');
    updateElementText('node-key', yggUtils.formatPublicKey(info.key) || 'N/A');
    updateElementText('node-version', info.build_name && info.build_version ? `${info.build_name} ${info.build_version}` : 'N/A');
    updateElementText('routing-entries', info.routing_entries || '0');
    
    // Update footer version
    updateElementText('footer-version', info.build_version || 'unknown');
    
    // Update full values for copy functionality
    updateElementData('node-key-full', info.key || '');
    updateElementData('node-address', info.address || '');
    updateElementData('node-subnet', info.subnet || '');
}

/**
 * Update peers information in the UI
 */
function updatePeersDisplay(data) {
    const peersContainer = document.getElementById('peers-list');
    if (!peersContainer) return;
    
    peersContainer.innerHTML = '';
    
    // Always update peer counts, even if no peers
    const peersCount = data.peers ? data.peers.length : 0;
    const onlineCount = data.peers ? data.peers.filter(p => p.up).length : 0;
    
    updateElementText('peers-count', peersCount.toString());
    updateElementText('peers-online', onlineCount.toString());
    
    if (!data.peers || data.peers.length === 0) {
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        const message = window.translations && window.translations[currentLang] 
            ? window.translations[currentLang]['no_peers_connected'] || 'No peers connected'
            : 'No peers connected';
        peersContainer.innerHTML = `<div class="no-data">${message}</div>`;
        return;
    }
    
    data.peers.forEach(peer => {
        // Debug: log NodeInfo for each peer
        if (peer.nodeinfo) {
            console.log(`[DEBUG WebUI] Peer ${peer.address} has NodeInfo:`, peer.nodeinfo);
            try {
                const parsed = JSON.parse(peer.nodeinfo);
                console.log(`[DEBUG WebUI] Parsed NodeInfo for ${peer.address}:`, parsed);
                if (parsed.name) {
                    console.log(`[DEBUG WebUI] Found name for ${peer.address}: ${parsed.name}`);
                }
            } catch (e) {
                console.warn(`[DEBUG WebUI] Failed to parse NodeInfo for ${peer.address}:`, e);
            }
        } else {
            console.log(`[DEBUG WebUI] Peer ${peer.address} has no NodeInfo`);
        }
        
        const peerElement = createPeerElement(peer);
        peersContainer.appendChild(peerElement);
    });
    
    // Update translations for newly added peer elements
    if (typeof updateTexts === 'function') {
        updateTexts();
    }
}

// Expose update functions to window for access from other scripts
window.updateNodeInfoDisplay = updateNodeInfoDisplay;
window.updatePeersDisplay = updatePeersDisplay;



// Expose copy functions to window for access from HTML onclick handlers
window.copyNodeKey = copyNodeKey;
window.copyNodeAddress = copyNodeAddress;
window.copyNodeSubnet = copyNodeSubnet;
window.copyPeerAddress = copyPeerAddress;
window.copyPeerKey = copyPeerKey;

/**
 * Create HTML element for a single peer
 */
function createPeerElement(peer) {
    const div = document.createElement('div');
    div.className = 'peer-item';
    
    const statusClass = yggUtils.getPeerStatusClass(peer.up);
    const statusText = yggUtils.getPeerStatusText(peer.up);
    const direction = yggUtils.getConnectionDirection(peer.inbound);
    const quality = yggUtils.getQualityIndicator(peer.cost);
    const uptimeText = peer.uptime ? yggUtils.formatDuration(peer.uptime) : 'N/A';
    const latencyText = yggUtils.formatLatency(peer.latency);
    
    // Get translations for labels
    const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
    const t = window.translations && window.translations[currentLang] ? window.translations[currentLang] : {};
    
    const labels = {
        connection: t['peer_connection'] || 'Connection',
        performance: t['peer_performance'] || 'Performance',
        traffic: t['peer_traffic'] || 'Traffic',
        uptime: t['peer_uptime'] || 'Uptime',
        port: t['peer_port'] || 'Port',
        priority: t['peer_priority'] || 'Priority',
        latency: t['peer_latency'] || 'Latency',
        cost: t['peer_cost'] || 'Cost',
        quality: t['peer_quality'] || 'Quality',
        received: t['peer_received'] || '↓ Received',
        sent: t['peer_sent'] || '↑ Sent',
        total: t['peer_total'] || 'Total',
        remove: t['peer_remove'] || 'Remove'
    };
    
    // Extract name from NodeInfo
    const displayName = yggUtils.getPeerDisplayName(peer);
    
    div.innerHTML = `
        <div class="peer-header">
            <div class="peer-address-section">
                <div class="peer-address copyable" onclick="copyPeerAddress('${peer.address || ''}')" data-key-title="copy_address_tooltip">
                    ${displayName} ${peer.address ? `(${peer.address})` : ''}
                </div>
                <div class="peer-key copyable" onclick="copyPeerKey('${peer.key || ''}')" data-key-title="copy_key_tooltip">${yggUtils.formatPublicKey(peer.key) || 'N/A'}</div>
            </div>
            <div class="peer-status-section">
                <div class="peer-status ${statusClass}">${statusText}</div>
                <div class="peer-direction ${peer.inbound ? 'inbound' : 'outbound'}" title="${direction.text}">
                    ${direction.icon} ${direction.text}
                </div>
            </div>
        </div>
        <div class="peer-details">
            <div class="peer-uri" title="${peer.remote || 'N/A'}">${peer.remote || 'N/A'}</div>
            <div class="peer-info-grid">
                <div class="peer-info-section">
                    <div class="peer-info-title">${labels.connection}</div>
                    <div class="peer-info-stats">
                        <span class="info-item">
                            <span class="info-label">${labels.uptime}:</span>
                            <span class="info-value">${uptimeText}</span>
                        </span>
                        <span class="info-item">
                            <span class="info-label">${labels.port}:</span>
                            <span class="info-value">${peer.port || 'N/A'}</span>
                        </span>
                        <span class="info-item">
                            <span class="info-label">${labels.priority}:</span>
                            <span class="info-value">${peer.priority !== undefined ? peer.priority : 'N/A'}</span>
                        </span>
                    </div>
                </div>
                <div class="peer-info-section">
                    <div class="peer-info-title">${labels.performance}</div>
                    <div class="peer-info-stats">
                        <span class="info-item">
                            <span class="info-label">${labels.latency}:</span>
                            <span class="info-value">${latencyText}</span>
                        </span>
                        <span class="info-item">
                            <span class="info-label">${labels.cost}:</span>
                            <span class="info-value">${peer.cost !== undefined ? peer.cost : 'N/A'}</span>
                        </span>
                        <span class="info-item quality-indicator">
                            <span class="info-label">${labels.quality}:</span>
                            <span class="info-value ${quality.class}">${quality.text}</span>
                        </span>
                    </div>
                </div>
                <div class="peer-info-section">
                    <div class="peer-info-title">${labels.traffic}</div>
                    <div class="peer-info-stats">
                        <span class="info-item">
                            <span class="info-label">${labels.received}:</span>
                            <span class="info-value">${yggUtils.formatBytes(peer.bytes_recvd || 0)}</span>
                        </span>
                        <span class="info-item">
                            <span class="info-label">${labels.sent}:</span>
                            <span class="info-value">${yggUtils.formatBytes(peer.bytes_sent || 0)}</span>
                        </span>
                        <span class="info-item">
                            <span class="info-label">${labels.total}:</span>
                            <span class="info-value">${yggUtils.formatBytes((peer.bytes_recvd || 0) + (peer.bytes_sent || 0))}</span>
                        </span>
                    </div>
                </div>
            </div>
        </div>
        ${peer.remote ? `<button class="peer-remove-btn" onclick="removePeerConfirm('${peer.remote}')">${labels.remove}</button>` : ''}
    `;
    
    return div;
}

/**
 * Add a new peer with modal form
 */
async function addPeer() {
    showModal({
        title: 'add_peer',
        content: 'add_peer_modal_description',
        size: 'medium',
        inputs: [
            {
                type: 'text',
                name: 'peer_uri',
                label: 'peer_uri_label',
                placeholder: 'peer_uri_placeholder',
                required: true,
                help: 'peer_uri_help'
            }
        ],
        buttons: [
            {
                text: 'modal_cancel',
                type: 'secondary',
                action: 'close'
            },
            {
                text: 'add_peer_btn',
                type: 'primary',
                callback: async (formData) => {
                    const uri = formData.peer_uri?.trim();
                    
                    if (!uri) {
                        showWarning('Peer URI is required');
                        return false; // Don't close modal
                    }
                    
                    // Basic URI validation
                    if (!uri.includes('://')) {
                        showError('Invalid URI format. Must include protocol (tcp://, tls://, etc.)');
                        return false; // Don't close modal
                    }
                    
                    try {
                        showInfo('Adding peer...');
                        await window.yggAPI.addPeer(uri);
                        showSuccess(`Peer added successfully: ${uri}`);
                        await loadPeers(); // Refresh peer list
                        return true; // Close modal
                    } catch (error) {
                        showError('Failed to add peer: ' + error.message);
                        return false; // Don't close modal
                    }
                }
            }
        ]
    });
}

/**
 * Remove peer with confirmation
 */
function removePeerConfirm(uri) {
    if (confirm(`Remove peer?\n${uri}`)) {
        removePeer(uri);
    }
}

/**
 * Remove a peer
 */
async function removePeer(uri) {
    try {
        showInfo('Removing peer...');
        await window.yggAPI.removePeer(uri);
        showSuccess(`Peer removed successfully: ${uri}`);
        await loadPeers(); // Refresh peer list
    } catch (error) {
        showError('Failed to remove peer: ' + error.message);
    }
}

/**
 * Helper function to update element text content
 */
function updateElementText(id, text) {
    const element = document.getElementById(id);
    if (element) {
        element.textContent = text;
    }
}

/**
 * Helper function to update element data attribute
 */
function updateElementData(id, data) {
    const element = document.getElementById(id);
    if (element) {
        element.setAttribute('data-value', data);
    }
}

/**
 * Copy text to clipboard
 */
async function copyToClipboard(text) {
    try {
        await navigator.clipboard.writeText(text);
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        const message = window.translations && window.translations[currentLang] 
            ? window.translations[currentLang]['copied_to_clipboard'] || 'Copied to clipboard'
            : 'Copied to clipboard';
        showSuccess(message);
    } catch (error) {
        console.error('Failed to copy:', error);
        showError('Failed to copy to clipboard');
    }
}

/**
 * Copy node key to clipboard
 */
function copyNodeKey() {
    const element = document.getElementById('node-key-full');
    if (element) {
        const key = element.getAttribute('data-value');
        if (key) {
            copyToClipboard(key);
        }
    }
}

/**
 * Copy node address to clipboard
 */
function copyNodeAddress() {
    const element = document.getElementById('node-address');
    if (element) {
        const address = element.getAttribute('data-value') || element.textContent;
        if (address && address !== 'N/A' && address !== 'Загрузка...') {
            copyToClipboard(address);
        }
    }
}

/**
 * Copy node subnet to clipboard
 */
function copyNodeSubnet() {
    const element = document.getElementById('node-subnet');
    if (element) {
        const subnet = element.getAttribute('data-value') || element.textContent;
        if (subnet && subnet !== 'N/A' && subnet !== 'Загрузка...') {
            copyToClipboard(subnet);
        }
    }
}

/**
 * Copy peer address to clipboard
 */
function copyPeerAddress(address) {
    if (address && address !== 'N/A') {
        copyToClipboard(address);
    }
}

/**
 * Copy peer key to clipboard
 */
function copyPeerKey(key) {
    if (key && key !== 'N/A') {
        copyToClipboard(key);
    }
}



/**
 * Auto-refresh data
 */
function startAutoRefresh() {
    // Refresh every 30 seconds
    setInterval(async () => {
        // Only proceed if individual requests are not already in progress
        if (!isLoadingNodeInfo && !isLoadingPeers) {
            try {
                await Promise.all([loadNodeInfo(), loadPeers()]);
            } catch (error) {
                console.error('Auto-refresh failed:', error);
            }
        } else {
            console.log('Skipping auto-refresh - requests already in progress');
        }
    }, 30000);
}

/**
 * Initialize application
 */
async function initializeApp() {
    try {
        // Ensure API is available
        if (typeof window.yggAPI === 'undefined') {
            console.error('yggAPI is not available');
            showError('Failed to initialize API client');
            return;
        }
        
        isLoading = true;
        
        // Initialize peer counts to 0 immediately to replace "Loading..." text
        updateElementText('peers-count', '0');
        updateElementText('peers-online', '0');
        
        // Load initial data
        await Promise.all([loadNodeInfo(), loadPeers()]);
        
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        const message = window.translations && window.translations[currentLang] 
            ? window.translations[currentLang]['dashboard_loaded'] || 'Dashboard loaded successfully'
            : 'Dashboard loaded successfully';
        showSuccess(message);
        
        // Start auto-refresh
        startAutoRefresh();
        
    } catch (error) {
        showError('Failed to initialize dashboard: ' + error.message);
    } finally {
        isLoading = false;
    }
}

// Wait for DOM and API to be ready
function waitForAPI() {
    console.log('Checking for yggAPI...', typeof window.yggAPI);
    
    if (typeof window.yggAPI !== 'undefined') {
        console.log('yggAPI found, initializing app...');
        initializeApp();
    } else {
        console.log('yggAPI not ready yet, retrying...');
        // Retry after a short delay
        setTimeout(waitForAPI, 100);
    }
}

// Initialize when DOM is ready
console.log('App.js loaded, document ready state:', document.readyState);

if (document.readyState === 'loading') {
    console.log('Document still loading, waiting for DOMContentLoaded...');
    document.addEventListener('DOMContentLoaded', waitForAPI);
} else {
    console.log('Document already loaded, starting API check...');
    initializeApp();
} 