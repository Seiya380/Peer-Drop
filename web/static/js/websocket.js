/**
 * WebSocket connection manager for Peer-Drop
 * Handles connection, reconnection, and message routing
 */
class WebSocketManager extends EventTarget {
    constructor() {
        super();
        this.ws = null;
        this.url = null;
        this.reconnectAttempts = 0;
        this.maxReconnectAttempts = 10;
        this.reconnectDelay = 1000;
        this.messageQueue = [];
        this.isConnected = false;
        this.pingInterval = null;
    }

    /**
     * Connect to WebSocket server
     */
    connect() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        this.url = `${protocol}//${window.location.host}/ws`;

        this.ws = new WebSocket(this.url);

        this.ws.onopen = () => {
            console.log('[WS] Connected');
            this.isConnected = true;
            this.reconnectAttempts = 0;
            this.reconnectDelay = 1000;

            // Send queued messages
            while (this.messageQueue.length > 0) {
                const msg = this.messageQueue.shift();
                this.ws.send(JSON.stringify(msg));
            }

            // Start ping interval
            this.startPing();

            this.dispatchEvent(new CustomEvent('open'));
        };

        this.ws.onclose = (event) => {
            console.log('[WS] Disconnected', event.code, event.reason);
            this.isConnected = false;
            this.stopPing();

            this.dispatchEvent(new CustomEvent('close', { detail: { code: event.code } }));

            // Attempt reconnection
            if (this.reconnectAttempts < this.maxReconnectAttempts) {
                setTimeout(() => {
                    this.reconnectAttempts++;
                    console.log(`[WS] Reconnecting... (attempt ${this.reconnectAttempts})`);
                    this.connect();
                }, this.reconnectDelay);

                // Exponential backoff
                this.reconnectDelay = Math.min(this.reconnectDelay * 1.5, 30000);
            }
        };

        this.ws.onerror = (error) => {
            console.error('[WS] Error:', error);
            this.dispatchEvent(new CustomEvent('error', { detail: error }));
        };

        this.ws.onmessage = (event) => {
            this.handleMessage(event.data);
        };
    }

    /**
     * Handle incoming message
     */
    handleMessage(data) {
        try {
            // Handle multiple messages (newline separated)
            const messages = data.split('\n').filter(m => m.trim());

            for (const msgStr of messages) {
                const msg = JSON.parse(msgStr);

                // Dispatch event based on message type
                this.dispatchEvent(new CustomEvent(msg.type, {
                    detail: {
                        peerId: msg.peerId,
                        targetId: msg.targetId,
                        payload: msg.payload
                    }
                }));
            }
        } catch (err) {
            console.error('[WS] Failed to parse message:', err, data);
        }
    }

    /**
     * Send a message to the server
     */
    send(type, payload = null, targetId = null) {
        const msg = { type };
        if (payload) msg.payload = payload;
        if (targetId) msg.targetId = targetId;

        if (this.isConnected && this.ws.readyState === WebSocket.OPEN) {
            this.ws.send(JSON.stringify(msg));
        } else {
            // Queue message for later
            this.messageQueue.push(msg);
        }
    }

    /**
     * Join with device info
     */
    join(name, platform) {
        this.send('join', { name, platform });
    }

    /**
     * Create a public room
     */
    createRoom() {
        this.send('create-room');
    }

    /**
     * Join a public room by code
     */
    joinRoom(code) {
        this.send('join-room', { code: code.toUpperCase() });
    }

    /**
     * Leave the current public room
     */
    leaveRoom() {
        this.send('leave-room');
    }

    /**
     * Send WebRTC offer to peer
     */
    sendOffer(targetId, sdp) {
        this.send('offer', sdp, targetId);
    }

    /**
     * Send WebRTC answer to peer
     */
    sendAnswer(targetId, sdp) {
        this.send('answer', sdp, targetId);
    }

    /**
     * Send ICE candidate to peer
     */
    sendIceCandidate(targetId, candidate) {
        this.send('ice-candidate', candidate, targetId);
    }

    /**
     * Send transfer request to peer
     */
    sendTransferRequest(targetId, transferId, files) {
        const fileInfos = files.map(f => ({
            name: f.name,
            size: f.size,
            type: f.type || 'application/octet-stream'
        }));

        const totalSize = files.reduce((sum, f) => sum + f.size, 0);

        this.send('transfer-request', {
            transferId,
            files: fileInfos,
            totalSize
        }, targetId);
    }

    /**
     * Send transfer response to peer
     */
    sendTransferResponse(targetId, transferId, accepted) {
        this.send('transfer-response', { transferId, accepted }, targetId);
    }

    /**
     * Send relay chunk (fallback when WebRTC fails)
     */
    sendRelayChunk(targetId, transferId, fileIndex, chunkIndex, data, isLast) {
        this.send('relay-chunk', {
            transferId,
            fileIndex,
            chunkIndex,
            data, // base64 encoded
            isLast
        }, targetId);
    }

    /**
     * Start ping interval to keep connection alive
     */
    startPing() {
        this.stopPing();
        this.pingInterval = setInterval(() => {
            if (this.isConnected) {
                this.send('ping');
            }
        }, 30000);
    }

    /**
     * Stop ping interval
     */
    stopPing() {
        if (this.pingInterval) {
            clearInterval(this.pingInterval);
            this.pingInterval = null;
        }
    }

    /**
     * Disconnect from server
     */
    disconnect() {
        this.maxReconnectAttempts = 0; // Prevent reconnection
        this.stopPing();
        if (this.ws) {
            this.ws.close();
        }
    }
}

// Export singleton instance
const wsManager = new WebSocketManager();
