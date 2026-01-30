/**
 * File transfer manager for Peer-Drop
 * Handles chunking, sending, and receiving files via WebRTC DataChannel
 * Falls back to WebSocket relay when P2P fails
 */
class FileTransferManager extends EventTarget {
    constructor(wsManager, webrtcManager) {
        super();
        this.wsManager = wsManager;
        this.webrtcManager = webrtcManager;

        // Transfer state
        this.outgoingTransfers = new Map(); // transferId -> transfer state
        this.incomingTransfers = new Map(); // transferId -> transfer state

        // Constants
        this.CHUNK_SIZE = 64 * 1024; // 64KB chunks
        this.MAX_BUFFER = 16 * 1024 * 1024; // 16MB buffer threshold

        // Message types for binary protocol
        this.MSG_METADATA = 0x01;
        this.MSG_CHUNK = 0x02;
        this.MSG_COMPLETE = 0x03;
        this.MSG_CANCEL = 0x04;

        this.setupListeners();
    }

    /**
     * Set up event listeners
     */
    setupListeners() {
        // Handle incoming transfer requests via WebSocket
        this.wsManager.addEventListener('transfer-request', (e) => {
            this.handleTransferRequest(e.detail.peerId, e.detail.payload);
        });

        // Handle transfer responses
        this.wsManager.addEventListener('transfer-response', (e) => {
            this.handleTransferResponse(e.detail.peerId, e.detail.payload);
        });

        // Handle relay chunks (fallback)
        this.wsManager.addEventListener('relay-chunk', (e) => {
            this.handleRelayChunk(e.detail.peerId, e.detail.payload);
        });

        // Handle DataChannel messages
        this.webrtcManager.addEventListener('datachannel-message', (e) => {
            this.handleDataChannelMessage(e.detail.peerId, e.detail.data);
        });
    }

    /**
     * Generate unique transfer ID
     */
    generateTransferId() {
        return 'transfer-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
    }

    /**
     * Send files to a peer
     */
    async sendFiles(peerId, files) {
        const transferId = this.generateTransferId();

        // Store transfer state
        const transfer = {
            id: transferId,
            peerId,
            files: Array.from(files),
            currentFileIndex: 0,
            currentChunkIndex: 0,
            bytesSent: 0,
            totalBytes: files.reduce((sum, f) => sum + f.size, 0),
            status: 'pending',
            useRelay: false
        };

        this.outgoingTransfers.set(transferId, transfer);

        // Send transfer request via WebSocket
        this.wsManager.sendTransferRequest(peerId, transferId, transfer.files);

        this.dispatchEvent(new CustomEvent('send-pending', {
            detail: { transferId, peerId, files: transfer.files }
        }));

        return transferId;
    }

    /**
     * Handle transfer response (accept/reject)
     */
    async handleTransferResponse(peerId, payload) {
        const transfer = this.outgoingTransfers.get(payload.transferId);
        if (!transfer) return;

        if (!payload.accepted) {
            transfer.status = 'rejected';
            this.dispatchEvent(new CustomEvent('send-rejected', {
                detail: { transferId: payload.transferId }
            }));
            this.outgoingTransfers.delete(payload.transferId);
            return;
        }

        transfer.status = 'accepted';
        this.dispatchEvent(new CustomEvent('send-accepted', {
            detail: { transferId: payload.transferId }
        }));

        // Try to establish WebRTC connection
        try {
            if (!this.webrtcManager.hasOpenChannel(peerId)) {
                await this.webrtcManager.connect(peerId);

                // Wait for data channel to open (with timeout)
                await this.waitForDataChannel(peerId, 10000);
            }

            // Send via WebRTC
            await this.sendViaDataChannel(transfer);
        } catch (err) {
            console.log('[Transfer] WebRTC failed, using relay fallback:', err.message);
            transfer.useRelay = true;
            await this.sendViaRelay(transfer);
        }
    }

    /**
     * Wait for data channel to open
     */
    waitForDataChannel(peerId, timeout) {
        return new Promise((resolve, reject) => {
            if (this.webrtcManager.hasOpenChannel(peerId)) {
                resolve();
                return;
            }

            const timeoutId = setTimeout(() => {
                cleanup();
                reject(new Error('Data channel timeout'));
            }, timeout);

            const onOpen = (e) => {
                if (e.detail.peerId === peerId) {
                    cleanup();
                    resolve();
                }
            };

            const onFailed = (e) => {
                if (e.detail.peerId === peerId) {
                    cleanup();
                    reject(new Error('Connection failed'));
                }
            };

            const cleanup = () => {
                clearTimeout(timeoutId);
                this.webrtcManager.removeEventListener('datachannel-open', onOpen);
                this.webrtcManager.removeEventListener('connection-failed', onFailed);
            };

            this.webrtcManager.addEventListener('datachannel-open', onOpen);
            this.webrtcManager.addEventListener('connection-failed', onFailed);
        });
    }

    /**
     * Send files via WebRTC DataChannel
     */
    async sendViaDataChannel(transfer) {
        const channel = this.webrtcManager.getDataChannel(transfer.peerId);
        if (!channel || channel.readyState !== 'open') {
            throw new Error('Data channel not available');
        }

        transfer.status = 'transferring';

        for (let fileIndex = 0; fileIndex < transfer.files.length; fileIndex++) {
            const file = transfer.files[fileIndex];
            transfer.currentFileIndex = fileIndex;

            // Send metadata
            const metadata = {
                transferId: transfer.id,
                fileIndex,
                name: file.name,
                size: file.size,
                type: file.type,
                totalChunks: Math.ceil(file.size / this.CHUNK_SIZE)
            };
            channel.send(this.createMetadataMessage(metadata));

            // Send file chunks
            let offset = 0;
            let chunkIndex = 0;

            while (offset < file.size) {
                // Backpressure handling
                while (channel.bufferedAmount > this.MAX_BUFFER) {
                    await this.sleep(50);
                }

                const chunk = file.slice(offset, offset + this.CHUNK_SIZE);
                const arrayBuffer = await chunk.arrayBuffer();

                channel.send(this.createChunkMessage(fileIndex, chunkIndex, arrayBuffer));

                offset += this.CHUNK_SIZE;
                chunkIndex++;
                transfer.currentChunkIndex = chunkIndex;
                transfer.bytesSent += arrayBuffer.byteLength;

                // Emit progress
                this.dispatchEvent(new CustomEvent('send-progress', {
                    detail: {
                        transferId: transfer.id,
                        progress: transfer.bytesSent / transfer.totalBytes,
                        bytesSent: transfer.bytesSent,
                        totalBytes: transfer.totalBytes
                    }
                }));
            }
        }

        // Send complete message
        channel.send(this.createCompleteMessage(transfer.id));

        transfer.status = 'completed';
        this.dispatchEvent(new CustomEvent('send-complete', {
            detail: { transferId: transfer.id }
        }));

        this.outgoingTransfers.delete(transfer.id);
    }

    /**
     * Send files via WebSocket relay (fallback)
     */
    async sendViaRelay(transfer) {
        transfer.status = 'transferring';

        for (let fileIndex = 0; fileIndex < transfer.files.length; fileIndex++) {
            const file = transfer.files[fileIndex];
            transfer.currentFileIndex = fileIndex;

            let offset = 0;
            let chunkIndex = 0;
            const totalChunks = Math.ceil(file.size / this.CHUNK_SIZE);

            while (offset < file.size) {
                const chunk = file.slice(offset, offset + this.CHUNK_SIZE);
                const arrayBuffer = await chunk.arrayBuffer();
                const base64 = this.arrayBufferToBase64(arrayBuffer);

                const isLast = (fileIndex === transfer.files.length - 1) &&
                    (offset + this.CHUNK_SIZE >= file.size);

                this.wsManager.sendRelayChunk(
                    transfer.peerId,
                    transfer.id,
                    fileIndex,
                    chunkIndex,
                    base64,
                    isLast
                );

                offset += this.CHUNK_SIZE;
                chunkIndex++;
                transfer.bytesSent += arrayBuffer.byteLength;

                // Emit progress
                this.dispatchEvent(new CustomEvent('send-progress', {
                    detail: {
                        transferId: transfer.id,
                        progress: transfer.bytesSent / transfer.totalBytes,
                        bytesSent: transfer.bytesSent,
                        totalBytes: transfer.totalBytes
                    }
                }));

                // Rate limit relay to avoid overwhelming WebSocket
                await this.sleep(10);
            }
        }

        transfer.status = 'completed';
        this.dispatchEvent(new CustomEvent('send-complete', {
            detail: { transferId: transfer.id }
        }));

        this.outgoingTransfers.delete(transfer.id);
    }

    /**
     * Handle incoming transfer request
     */
    handleTransferRequest(peerId, payload) {
        const transfer = {
            id: payload.transferId,
            peerId,
            files: payload.files,
            totalSize: payload.totalSize,
            receivedFiles: [],
            currentFileData: [],
            currentFileIndex: 0,
            bytesReceived: 0,
            status: 'pending'
        };

        this.incomingTransfers.set(payload.transferId, transfer);

        this.dispatchEvent(new CustomEvent('receive-request', {
            detail: {
                transferId: payload.transferId,
                peerId,
                files: payload.files,
                totalSize: payload.totalSize
            }
        }));
    }

    /**
     * Accept an incoming transfer
     */
    acceptTransfer(transferId) {
        const transfer = this.incomingTransfers.get(transferId);
        if (!transfer) return;

        transfer.status = 'accepted';
        this.wsManager.sendTransferResponse(transfer.peerId, transferId, true);

        this.dispatchEvent(new CustomEvent('receive-accepted', {
            detail: { transferId }
        }));
    }

    /**
     * Reject an incoming transfer
     */
    rejectTransfer(transferId) {
        const transfer = this.incomingTransfers.get(transferId);
        if (!transfer) return;

        transfer.status = 'rejected';
        this.wsManager.sendTransferResponse(transfer.peerId, transferId, false);
        this.incomingTransfers.delete(transferId);

        this.dispatchEvent(new CustomEvent('receive-rejected', {
            detail: { transferId }
        }));
    }

    /**
     * Handle DataChannel message (binary)
     */
    handleDataChannelMessage(peerId, data) {
        if (typeof data === 'string') {
            // JSON message
            try {
                const msg = JSON.parse(data);
                this.handleJsonMessage(peerId, msg);
            } catch (e) {
                console.error('[Transfer] Failed to parse JSON message:', e);
            }
        } else {
            // Binary message
            this.handleBinaryMessage(peerId, data);
        }
    }

    /**
     * Handle JSON message from DataChannel
     */
    handleJsonMessage(peerId, msg) {
        switch (msg.type) {
            case 'metadata':
                this.handleMetadata(peerId, msg);
                break;
            case 'complete':
                this.handleComplete(msg.transferId);
                break;
        }
    }

    /**
     * Handle binary message from DataChannel
     */
    handleBinaryMessage(peerId, data) {
        const view = new DataView(data);
        const msgType = view.getUint8(0);

        switch (msgType) {
            case this.MSG_METADATA:
                const metadataLength = view.getUint32(1);
                const metadataJson = new TextDecoder().decode(data.slice(5, 5 + metadataLength));
                this.handleMetadata(peerId, JSON.parse(metadataJson));
                break;

            case this.MSG_CHUNK:
                const fileIndex = view.getUint16(1);
                const chunkIndex = view.getUint32(3);
                const chunkData = data.slice(7);
                this.handleChunk(peerId, fileIndex, chunkIndex, chunkData);
                break;

            case this.MSG_COMPLETE:
                const transferIdLength = view.getUint8(1);
                const transferId = new TextDecoder().decode(data.slice(2, 2 + transferIdLength));
                this.handleComplete(transferId);
                break;
        }
    }

    /**
     * Handle file metadata
     */
    handleMetadata(peerId, metadata) {
        const transfer = this.findTransferByPeer(peerId);
        if (!transfer) return;

        transfer.currentFileIndex = metadata.fileIndex;
        transfer.currentFileData = [];
        transfer.currentFileMetadata = metadata;
        transfer.status = 'transferring';
    }

    /**
     * Handle file chunk
     */
    handleChunk(peerId, fileIndex, chunkIndex, chunkData) {
        const transfer = this.findTransferByPeer(peerId);
        if (!transfer) return;

        transfer.currentFileData.push(chunkData);
        transfer.bytesReceived += chunkData.byteLength;

        // Emit progress
        this.dispatchEvent(new CustomEvent('receive-progress', {
            detail: {
                transferId: transfer.id,
                progress: transfer.bytesReceived / transfer.totalSize,
                bytesReceived: transfer.bytesReceived,
                totalSize: transfer.totalSize
            }
        }));

        // Check if file is complete
        const expectedChunks = transfer.currentFileMetadata?.totalChunks || 0;
        if (transfer.currentFileData.length >= expectedChunks) {
            this.saveReceivedFile(transfer);
        }
    }

    /**
     * Handle relay chunk (WebSocket fallback)
     */
    handleRelayChunk(peerId, payload) {
        let transfer = this.incomingTransfers.get(payload.transferId);
        if (!transfer) return;

        const chunkData = this.base64ToArrayBuffer(payload.data);

        // Initialize file data array if needed
        if (!transfer.fileChunks) {
            transfer.fileChunks = [];
        }
        if (!transfer.fileChunks[payload.fileIndex]) {
            transfer.fileChunks[payload.fileIndex] = [];
        }

        transfer.fileChunks[payload.fileIndex][payload.chunkIndex] = chunkData;
        transfer.bytesReceived += chunkData.byteLength;
        transfer.status = 'transferring';

        // Emit progress
        this.dispatchEvent(new CustomEvent('receive-progress', {
            detail: {
                transferId: transfer.id,
                progress: transfer.bytesReceived / transfer.totalSize,
                bytesReceived: transfer.bytesReceived,
                totalSize: transfer.totalSize
            }
        }));

        // If this is the last chunk, save all files
        if (payload.isLast) {
            this.saveAllRelayedFiles(transfer);
        }
    }

    /**
     * Save all files received via relay
     */
    saveAllRelayedFiles(transfer) {
        for (let i = 0; i < transfer.files.length; i++) {
            const fileInfo = transfer.files[i];
            const chunks = transfer.fileChunks[i] || [];
            const blob = new Blob(chunks, { type: fileInfo.type });
            this.downloadBlob(blob, fileInfo.name);
        }

        transfer.status = 'completed';
        this.dispatchEvent(new CustomEvent('receive-complete', {
            detail: { transferId: transfer.id }
        }));

        this.incomingTransfers.delete(transfer.id);
    }

    /**
     * Save a received file
     */
    saveReceivedFile(transfer) {
        const metadata = transfer.currentFileMetadata;
        const blob = new Blob(transfer.currentFileData, { type: metadata.type });

        transfer.receivedFiles.push({
            name: metadata.name,
            blob
        });

        // Download the file
        this.downloadBlob(blob, metadata.name);

        // Reset for next file
        transfer.currentFileData = [];
        transfer.currentFileMetadata = null;
    }

    /**
     * Handle transfer complete
     */
    handleComplete(transferId) {
        const transfer = this.incomingTransfers.get(transferId);
        if (!transfer) return;

        transfer.status = 'completed';
        this.dispatchEvent(new CustomEvent('receive-complete', {
            detail: { transferId }
        }));

        this.incomingTransfers.delete(transferId);
    }

    /**
     * Find transfer by peer ID
     */
    findTransferByPeer(peerId) {
        for (const transfer of this.incomingTransfers.values()) {
            if (transfer.peerId === peerId && transfer.status !== 'completed') {
                return transfer;
            }
        }
        return null;
    }

    /**
     * Download a blob as a file
     */
    downloadBlob(blob, filename) {
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }

    /**
     * Create metadata message (binary)
     */
    createMetadataMessage(metadata) {
        const json = JSON.stringify(metadata);
        const jsonBytes = new TextEncoder().encode(json);
        const buffer = new ArrayBuffer(5 + jsonBytes.length);
        const view = new DataView(buffer);
        const uint8 = new Uint8Array(buffer);

        view.setUint8(0, this.MSG_METADATA);
        view.setUint32(1, jsonBytes.length);
        uint8.set(jsonBytes, 5);

        return buffer;
    }

    /**
     * Create chunk message (binary)
     */
    createChunkMessage(fileIndex, chunkIndex, data) {
        const buffer = new ArrayBuffer(7 + data.byteLength);
        const view = new DataView(buffer);
        const uint8 = new Uint8Array(buffer);

        view.setUint8(0, this.MSG_CHUNK);
        view.setUint16(1, fileIndex);
        view.setUint32(3, chunkIndex);
        uint8.set(new Uint8Array(data), 7);

        return buffer;
    }

    /**
     * Create complete message (binary)
     */
    createCompleteMessage(transferId) {
        const idBytes = new TextEncoder().encode(transferId);
        const buffer = new ArrayBuffer(2 + idBytes.length);
        const view = new DataView(buffer);
        const uint8 = new Uint8Array(buffer);

        view.setUint8(0, this.MSG_COMPLETE);
        view.setUint8(1, idBytes.length);
        uint8.set(idBytes, 2);

        return buffer;
    }

    /**
     * Convert ArrayBuffer to Base64
     */
    arrayBufferToBase64(buffer) {
        const bytes = new Uint8Array(buffer);
        let binary = '';
        for (let i = 0; i < bytes.byteLength; i++) {
            binary += String.fromCharCode(bytes[i]);
        }
        return btoa(binary);
    }

    /**
     * Convert Base64 to ArrayBuffer
     */
    base64ToArrayBuffer(base64) {
        const binary = atob(base64);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) {
            bytes[i] = binary.charCodeAt(i);
        }
        return bytes.buffer;
    }

    /**
     * Sleep helper
     */
    sleep(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }
}
