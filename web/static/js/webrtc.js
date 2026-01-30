/**
 * WebRTC peer connection manager for Peer-Drop
 * Handles P2P connections and data channels
 */
class WebRTCManager extends EventTarget {
    constructor(wsManager) {
        super();
        this.wsManager = wsManager;
        this.connections = new Map(); // peerId -> RTCPeerConnection
        this.dataChannels = new Map(); // peerId -> RTCDataChannel
        this.pendingCandidates = new Map(); // peerId -> ICE candidates waiting for connection

        this.rtcConfig = {
            iceServers: [
                { urls: 'stun:stun.l.google.com:19302' },
                { urls: 'stun:stun1.l.google.com:19302' }
            ]
        };

        this.setupSignalingListeners();
    }

    /**
     * Set up listeners for WebSocket signaling messages
     */
    setupSignalingListeners() {
        this.wsManager.addEventListener('offer', (e) => {
            this.handleOffer(e.detail.peerId, e.detail.payload);
        });

        this.wsManager.addEventListener('answer', (e) => {
            this.handleAnswer(e.detail.peerId, e.detail.payload);
        });

        this.wsManager.addEventListener('ice-candidate', (e) => {
            this.handleIceCandidate(e.detail.peerId, e.detail.payload);
        });
    }

    /**
     * Create a new peer connection
     */
    createConnection(peerId) {
        if (this.connections.has(peerId)) {
            return this.connections.get(peerId);
        }

        const pc = new RTCPeerConnection(this.rtcConfig);

        pc.onicecandidate = (event) => {
            if (event.candidate) {
                this.wsManager.sendIceCandidate(peerId, {
                    candidate: event.candidate.candidate,
                    sdpMid: event.candidate.sdpMid,
                    sdpMLineIndex: event.candidate.sdpMLineIndex
                });
            }
        };

        pc.oniceconnectionstatechange = () => {
            console.log(`[WebRTC] ICE state for ${peerId}: ${pc.iceConnectionState}`);

            this.dispatchEvent(new CustomEvent('connection-state-change', {
                detail: { peerId, state: pc.iceConnectionState }
            }));

            if (pc.iceConnectionState === 'failed' || pc.iceConnectionState === 'disconnected') {
                this.dispatchEvent(new CustomEvent('connection-failed', {
                    detail: { peerId }
                }));
            }
        };

        pc.ondatachannel = (event) => {
            console.log(`[WebRTC] Received data channel from ${peerId}`);
            this.setupDataChannel(peerId, event.channel);
        };

        this.connections.set(peerId, pc);
        return pc;
    }

    /**
     * Set up a data channel
     */
    setupDataChannel(peerId, channel) {
        channel.binaryType = 'arraybuffer';

        channel.onopen = () => {
            console.log(`[WebRTC] Data channel open for ${peerId}`);
            this.dispatchEvent(new CustomEvent('datachannel-open', {
                detail: { peerId }
            }));
        };

        channel.onclose = () => {
            console.log(`[WebRTC] Data channel closed for ${peerId}`);
            this.dataChannels.delete(peerId);
            this.dispatchEvent(new CustomEvent('datachannel-close', {
                detail: { peerId }
            }));
        };

        channel.onerror = (error) => {
            console.error(`[WebRTC] Data channel error for ${peerId}:`, error);
            this.dispatchEvent(new CustomEvent('datachannel-error', {
                detail: { peerId, error }
            }));
        };

        channel.onmessage = (event) => {
            this.dispatchEvent(new CustomEvent('datachannel-message', {
                detail: { peerId, data: event.data }
            }));
        };

        this.dataChannels.set(peerId, channel);
    }

    /**
     * Initiate a connection to a peer (caller)
     */
    async connect(peerId) {
        const pc = this.createConnection(peerId);

        // Create data channel
        const channel = pc.createDataChannel('file-transfer', {
            ordered: true
        });
        this.setupDataChannel(peerId, channel);

        // Create and send offer
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);

        this.wsManager.sendOffer(peerId, {
            sdp: offer.sdp,
            type: offer.type
        });

        console.log(`[WebRTC] Sent offer to ${peerId}`);
    }

    /**
     * Handle incoming offer (callee)
     */
    async handleOffer(peerId, payload) {
        console.log(`[WebRTC] Received offer from ${peerId}`);

        const pc = this.createConnection(peerId);

        await pc.setRemoteDescription(new RTCSessionDescription({
            sdp: payload.sdp,
            type: payload.type
        }));

        // Add any pending ICE candidates
        if (this.pendingCandidates.has(peerId)) {
            for (const candidate of this.pendingCandidates.get(peerId)) {
                await pc.addIceCandidate(new RTCIceCandidate(candidate));
            }
            this.pendingCandidates.delete(peerId);
        }

        // Create and send answer
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);

        this.wsManager.sendAnswer(peerId, {
            sdp: answer.sdp,
            type: answer.type
        });

        console.log(`[WebRTC] Sent answer to ${peerId}`);
    }

    /**
     * Handle incoming answer
     */
    async handleAnswer(peerId, payload) {
        console.log(`[WebRTC] Received answer from ${peerId}`);

        const pc = this.connections.get(peerId);
        if (!pc) {
            console.error(`[WebRTC] No connection for ${peerId}`);
            return;
        }

        await pc.setRemoteDescription(new RTCSessionDescription({
            sdp: payload.sdp,
            type: payload.type
        }));

        // Add any pending ICE candidates
        if (this.pendingCandidates.has(peerId)) {
            for (const candidate of this.pendingCandidates.get(peerId)) {
                await pc.addIceCandidate(new RTCIceCandidate(candidate));
            }
            this.pendingCandidates.delete(peerId);
        }
    }

    /**
     * Handle incoming ICE candidate
     */
    async handleIceCandidate(peerId, payload) {
        const pc = this.connections.get(peerId);

        const candidate = new RTCIceCandidate({
            candidate: payload.candidate,
            sdpMid: payload.sdpMid,
            sdpMLineIndex: payload.sdpMLineIndex
        });

        if (!pc || !pc.remoteDescription) {
            // Queue candidate until remote description is set
            if (!this.pendingCandidates.has(peerId)) {
                this.pendingCandidates.set(peerId, []);
            }
            this.pendingCandidates.get(peerId).push(candidate);
            return;
        }

        try {
            await pc.addIceCandidate(candidate);
        } catch (err) {
            console.error(`[WebRTC] Failed to add ICE candidate for ${peerId}:`, err);
        }
    }

    /**
     * Get data channel for a peer
     */
    getDataChannel(peerId) {
        return this.dataChannels.get(peerId);
    }

    /**
     * Check if we have an open data channel to a peer
     */
    hasOpenChannel(peerId) {
        const channel = this.dataChannels.get(peerId);
        return channel && channel.readyState === 'open';
    }

    /**
     * Close connection to a peer
     */
    closeConnection(peerId) {
        const channel = this.dataChannels.get(peerId);
        if (channel) {
            channel.close();
            this.dataChannels.delete(peerId);
        }

        const pc = this.connections.get(peerId);
        if (pc) {
            pc.close();
            this.connections.delete(peerId);
        }

        this.pendingCandidates.delete(peerId);
    }

    /**
     * Close all connections
     */
    closeAll() {
        for (const peerId of this.connections.keys()) {
            this.closeConnection(peerId);
        }
    }
}
