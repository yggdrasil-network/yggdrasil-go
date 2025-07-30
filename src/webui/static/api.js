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
        const options = {
            method: Object.keys(args).length > 0 ? 'POST' : 'GET',
            headers: {
                'Content-Type': 'application/json',
            },
            credentials: 'same-origin' // Include session cookies
        };

        if (Object.keys(args).length > 0) {
            options.body = JSON.stringify(args);
        }

        try {
            const response = await fetch(url, options);
            
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            
            const data = await response.json();
            
            if (data.status === 'error') {
                throw new Error(data.error || 'Unknown API error');
            }
            
            return data.response || data.commands;
        } catch (error) {
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
        return up ? 'Online' : 'Offline';
    }
}

// Create global API instance
window.yggAPI = new YggdrasilAPI();
window.yggUtils = YggdrasilUtils;