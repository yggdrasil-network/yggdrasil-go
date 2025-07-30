/**
 * Yggdrasil WebUI Application Logic
 * Integrates admin API with the user interface
 */

// Global state
let nodeInfo = null;
let peersData = null;
let isLoading = false;

/**
 * Load and display node information
 */
async function loadNodeInfo() {
    try {
        const info = await window.yggAPI.getSelf();
        nodeInfo = info;
        updateNodeInfoDisplay(info);
        return info;
    } catch (error) {
        console.error('Failed to load node info:', error);
        showError('Failed to load node information: ' + error.message);
        throw error;
    }
}

/**
 * Load and display peers information
 */
async function loadPeers() {
    try {
        const data = await window.yggAPI.getPeers();
        peersData = data;
        updatePeersDisplay(data);
        return data;
    } catch (error) {
        console.error('Failed to load peers:', error);
        showError('Failed to load peers information: ' + error.message);
        throw error;
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
    updateElementText('node-version', `${info.build_name} ${info.build_version}` || 'N/A');
    updateElementText('routing-entries', info.routing_entries || '0');
    
    // Update full key display (for copy functionality)
    updateElementData('node-key-full', info.key || '');
}

/**
 * Update peers information in the UI
 */
function updatePeersDisplay(data) {
    const peersContainer = document.getElementById('peers-list');
    if (!peersContainer) return;
    
    peersContainer.innerHTML = '';
    
    if (!data.peers || data.peers.length === 0) {
        peersContainer.innerHTML = '<div class="no-data">No peers connected</div>';
        return;
    }
    
    data.peers.forEach(peer => {
        const peerElement = createPeerElement(peer);
        peersContainer.appendChild(peerElement);
    });
    
    // Update peer count
    updateElementText('peers-count', data.peers.length.toString());
    updateElementText('peers-online', data.peers.filter(p => p.up).length.toString());
}

/**
 * Create HTML element for a single peer
 */
function createPeerElement(peer) {
    const div = document.createElement('div');
    div.className = 'peer-item';
    
    const statusClass = yggUtils.getPeerStatusClass(peer.up);
    const statusText = yggUtils.getPeerStatusText(peer.up);
    
    div.innerHTML = `
        <div class="peer-header">
            <div class="peer-address">${peer.address || 'N/A'}</div>
            <div class="peer-status ${statusClass}">${statusText}</div>
        </div>
        <div class="peer-details">
            <div class="peer-uri" title="${peer.remote || 'N/A'}">${peer.remote || 'N/A'}</div>
            <div class="peer-stats">
                <span>↓ ${yggUtils.formatBytes(peer.bytes_recvd || 0)}</span>
                <span>↑ ${yggUtils.formatBytes(peer.bytes_sent || 0)}</span>
                ${peer.up && peer.latency ? `<span>RTT: ${(peer.latency / 1000000).toFixed(1)}ms</span>` : ''}
            </div>
        </div>
        ${peer.remote ? `<button class="peer-remove-btn" onclick="removePeerConfirm('${peer.remote}')">Remove</button>` : ''}
    `;
    
    return div;
}

/**
 * Add a new peer
 */
async function addPeer() {
    const uri = prompt('Enter peer URI:\nExamples:\n• tcp://example.com:54321\n• tls://peer.yggdrasil.network:443');
    if (!uri || uri.trim() === '') {
        showWarning('Peer URI is required');
        return;
    }
    
    // Basic URI validation
    if (!uri.includes('://')) {
        showError('Invalid URI format. Must include protocol (tcp://, tls://, etc.)');
        return;
    }
    
    try {
        showInfo('Adding peer...');
        await window.yggAPI.addPeer(uri.trim());
        showSuccess(`Peer added successfully: ${uri.trim()}`);
        await loadPeers(); // Refresh peer list
    } catch (error) {
        showError('Failed to add peer: ' + error.message);
    }
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
        showSuccess('Copied to clipboard');
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
 * Auto-refresh data
 */
function startAutoRefresh() {
    // Refresh every 30 seconds
    setInterval(async () => {
        if (!isLoading) {
            try {
                await Promise.all([loadNodeInfo(), loadPeers()]);
            } catch (error) {
                console.error('Auto-refresh failed:', error);
            }
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
        showInfo('Loading dashboard...');
        
        // Load initial data
        await Promise.all([loadNodeInfo(), loadPeers()]);
        
        showSuccess('Dashboard loaded successfully');
        
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