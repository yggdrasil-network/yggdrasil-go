/**
 * Yggdrasil Admin API Client
 * Provides JavaScript interface for accessing Yggdrasil admin functions
 */
class YggdrasilAPI {
    constructor() {
        this.baseURL = '/api/admin';
    }

    /**
     * Generic method to call admin API endpoints
     * @param {string} command - Admin command name
     * @param {Object} args - Command arguments
     * @returns {Promise<Object>} - API response
     */
    async callAdmin(command, args = {}) {
        const url = command ? `${this.baseURL}/${command}` : this.baseURL;
        
        // Create AbortController for timeout functionality
        const controller = new AbortController();
        const timeoutId = setTimeout(() => controller.abort(), 5000); // 5 second timeout
        
        const options = {
            method: Object.keys(args).length > 0 ? 'POST' : 'GET',
            headers: {
                'Content-Type': 'application/json',
            },
            credentials: 'same-origin', // Include session cookies
            signal: controller.signal
        };

        if (Object.keys(args).length > 0) {
            options.body = JSON.stringify(args);
        }

        try {
            const response = await fetch(url, options);
            clearTimeout(timeoutId); // Clear timeout on successful response
            
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            
            const data = await response.json();
            
            if (data.status === 'error') {
                throw new Error(data.error || 'Unknown API error');
            }
            
            return data.response || data.commands;
        } catch (error) {
            clearTimeout(timeoutId); // Clear timeout on error
            
            if (error.name === 'AbortError') {
                console.error(`API call timeout for ${command}:`, error);
                throw new Error('Request timeout - service may be unavailable');
            }
            
            console.error(`API call failed for ${command}:`, error);
            throw error;
        }
    }

    /**
     * Get list of available admin commands
     * @returns {Promise<Array>} - List of available commands
     */
    async getCommands() {
        return await this.callAdmin('');
    }

    /**
     * Get information about this node
     * @returns {Promise<Object>} - Node information
     */
    async getSelf() {
        return await this.callAdmin('getSelf');
    }

    /**
     * Get list of connected peers
     * @returns {Promise<Object>} - Peers information
     */
    async getPeers() {
        return await this.callAdmin('getPeers');
    }

    /**
     * Get tree routing information
     * @returns {Promise<Object>} - Tree information
     */
    async getTree() {
        return await this.callAdmin('getTree');
    }

    /**
     * Get established paths through this node
     * @returns {Promise<Object>} - Paths information
     */
    async getPaths() {
        return await this.callAdmin('getPaths');
    }

    /**
     * Get established traffic sessions with remote nodes
     * @returns {Promise<Object>} - Sessions information
     */
    async getSessions() {
        return await this.callAdmin('getSessions');
    }

    /**
     * Add a peer to the peer list
     * @param {string} uri - Peer URI (e.g., "tls://example.com:12345")
     * @param {string} int - Network interface (optional)
     * @returns {Promise<Object>} - Add peer response
     */
    async addPeer(uri, int = '') {
        return await this.callAdmin('addPeer', { uri, int });
    }

    /**
     * Remove a peer from the peer list
     * @param {string} uri - Peer URI to remove
     * @param {string} int - Network interface (optional)
     * @returns {Promise<Object>} - Remove peer response
     */
    async removePeer(uri, int = '') {
        return await this.callAdmin('removePeer', { uri, int });
    }
}

/**
 * Data formatting utilities
 */
class YggdrasilUtils {
    /**
     * Format bytes to human readable format
     * @param {number} bytes - Bytes count
     * @returns {string} - Formatted string (e.g., "1.5 MB")
     */
    static formatBytes(bytes) {
        if (bytes === 0) return '0 B';
        
        const k = 1024;
        const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
        const i = Math.floor(Math.log(bytes) / Math.log(k));
        
        return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
    }

    /**
     * Format duration to human readable format
     * @param {number} seconds - Duration in seconds
     * @returns {string} - Formatted duration
     */
    static formatDuration(seconds) {
        if (seconds < 60) return `${Math.round(seconds)}s`;
        if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
        if (seconds < 86400) return `${Math.round(seconds / 3600)}h`;
        return `${Math.round(seconds / 86400)}d`;
    }

    /**
     * Format public key for display (show first 8 and last 8 chars)
     * @param {string} key - Full public key
     * @returns {string} - Shortened key
     */
    static formatPublicKey(key) {
        if (!key || key.length < 16) return key;
        return `${key.substring(0, 8)}...${key.substring(key.length - 8)}`;
    }

    /**
     * Get status color class based on peer state
     * @param {boolean} up - Whether peer is up
     * @returns {string} - CSS class name
     */
    static getPeerStatusClass(up) {
        return up ? 'status-online' : 'status-offline';
    }

    /**
     * Get status text based on peer state
     * @param {boolean} up - Whether peer is up
     * @returns {string} - Status text
     */
    static getPeerStatusText(up) {
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        if (window.translations && window.translations[currentLang]) {
            return up 
                ? window.translations[currentLang]['peer_status_online'] || 'Online'
                : window.translations[currentLang]['peer_status_offline'] || 'Offline';
        }
        return up ? 'Online' : 'Offline';
    }

    /**
     * Format latency to human readable format
     * @param {number} latency - Latency in nanoseconds
     * @returns {string} - Formatted latency
     */
    static formatLatency(latency) {
        if (!latency || latency === 0) return 'N/A';
        const ms = latency / 1000000;
        if (ms < 1) return `${(latency / 1000).toFixed(0)}μs`;
        if (ms < 1000) return `${ms.toFixed(1)}ms`;
        return `${(ms / 1000).toFixed(2)}s`;
    }

    /**
     * Get direction text and icon
     * @param {boolean} inbound - Whether connection is inbound
     * @returns {Object} - Object with text and icon
     */
    static getConnectionDirection(inbound) {
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        let text;
        if (window.translations && window.translations[currentLang]) {
            text = inbound 
                ? window.translations[currentLang]['peer_direction_inbound'] || 'Inbound'
                : window.translations[currentLang]['peer_direction_outbound'] || 'Outbound';
        } else {
            text = inbound ? 'Inbound' : 'Outbound';
        }
        return {
            text: text,
            icon: inbound ? '↓' : '↑'
        };
    }

    /**
     * Format port number for display
     * @param {number} port - Port number
     * @returns {string} - Formatted port
     */
    static formatPort(port) {
        return port ? `Port ${port}` : 'N/A';
    }

    /**
     * Get quality indicator based on cost
     * @param {number} cost - Connection cost
     * @returns {Object} - Object with class and text
     */
    static getQualityIndicator(cost) {
        const currentLang = window.getCurrentLanguage ? window.getCurrentLanguage() : 'en';
        let text;
        if (window.translations && window.translations[currentLang]) {
            if (!cost || cost === 0) {
                text = window.translations[currentLang]['peer_quality_unknown'] || 'Unknown';
                return { class: 'quality-unknown', text: text };
            }
            if (cost <= 100) {
                text = window.translations[currentLang]['peer_quality_excellent'] || 'Excellent';
                return { class: 'quality-excellent', text: text };
            }
            if (cost <= 200) {
                text = window.translations[currentLang]['peer_quality_good'] || 'Good';
                return { class: 'quality-good', text: text };
            }
            if (cost <= 400) {
                text = window.translations[currentLang]['peer_quality_fair'] || 'Fair';
                return { class: 'quality-fair', text: text };
            }
            text = window.translations[currentLang]['peer_quality_poor'] || 'Poor';
            return { class: 'quality-poor', text: text };
        }
        
        // Fallback to English
        if (!cost || cost === 0) return { class: 'quality-unknown', text: 'Unknown' };
        if (cost <= 100) return { class: 'quality-excellent', text: 'Excellent' };
        if (cost <= 200) return { class: 'quality-good', text: 'Good' };
        if (cost <= 400) return { class: 'quality-fair', text: 'Fair' };
        return { class: 'quality-poor', text: 'Poor' };
    }

    /**
     * Extract name from NodeInfo JSON string
     * @param {string} nodeinfo - NodeInfo JSON string
     * @returns {string} - Extracted name or null
     */
    static extractNodeInfoName(nodeinfo) {
        if (!nodeinfo || typeof nodeinfo !== 'string') {
            return null;
        }
        
        try {
            const parsed = JSON.parse(nodeinfo);
            return parsed.name && typeof parsed.name === 'string' ? parsed.name : null;
        } catch (error) {
            console.warn('Failed to parse NodeInfo:', error);
            return null;
        }
    }

    /**
     * Get display name for peer (from NodeInfo or fallback to address)
     * @param {Object} peer - Peer object with nodeinfo and address
     * @returns {string} - Display name
     */
    static getPeerDisplayName(peer) {
        // Try to get name from NodeInfo first
        if (peer.nodeinfo) {
            const name = this.extractNodeInfoName(peer.nodeinfo);
            if (name) {
                return name;
            }
        }
        
        // Fallback to address or "Anonymous"
        return 'Anonymous';
    }
}

// Create global API instance
window.yggAPI = new YggdrasilAPI();
window.yggUtils = YggdrasilUtils;