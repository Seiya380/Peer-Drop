/**
 * Peer-Drop Main Application
 * Integrates WebSocket, WebRTC, and File Transfer modules
 */

// Application state
let deviceId = null;
let deviceName = null;
let peers = new Map();
let selectedPeer = null;
let selectedFiles = [];
let publicRoomCode = null;

// Module instances
let webrtcManager = null;
let fileTransferManager = null;

// Initialize application
document.addEventListener('DOMContentLoaded', () => {
    initializeDevice();
    initializeModules();
    setupUI();
    connect();
});

/**
 * Initialize device identity
 */
function initializeDevice() {
    // Generate or retrieve device ID
    deviceId = localStorage.getItem('peerdrop-device-id');
    if (!deviceId) {
        deviceId = 'device-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
        localStorage.setItem('peerdrop-device-id', deviceId);
    }

    // Get device name
    deviceName = localStorage.getItem('peerdrop-device-name');
    if (!deviceName) {
        deviceName = getDefaultDeviceName();
        localStorage.setItem('peerdrop-device-name', deviceName);
    }

    // Update UI
    document.getElementById('device-name').textContent = deviceName;
    document.getElementById('device-platform').textContent = getPlatformIcon(getPlatform()) + ' ' + getPlatform();
}

/**
 * Initialize modules
 */
function initializeModules() {
    webrtcManager = new WebRTCManager(wsManager);
    fileTransferManager = new FileTransferManager(wsManager, webrtcManager);

    setupWebSocketListeners();
    setupFileTransferListeners();
}

/**
 * Connect to server
 */
function connect() {
    wsManager.connect();
}

/**
 * Set up WebSocket event listeners
 */
function setupWebSocketListeners() {
    wsManager.addEventListener('open', () => {
        console.log('[App] WebSocket connected');
        wsManager.join(deviceName, getPlatform());
        updateConnectionStatus(true);
    });

    wsManager.addEventListener('close', () => {
        console.log('[App] WebSocket disconnected');
        updateConnectionStatus(false);
        peers.clear();
        updatePeerList();
    });

    wsManager.addEventListener('peers', (e) => {
        console.log('[App] Received peer list:', e.detail.payload);
        const peerList = e.detail.payload?.peers || [];
        peers.clear();
        for (const peer of peerList) {
            peers.set(peer.id, peer);
        }
        updatePeerList();
    });

    wsManager.addEventListener('peer-joined', (e) => {
        console.log('[App] Peer joined:', e.detail.payload);
        const peer = e.detail.payload?.peer;
        if (peer) {
            peers.set(peer.id, peer);
            updatePeerList();
            showNotification(`${peer.name} joined`);
        }
    });

    wsManager.addEventListener('peer-left', (e) => {
        console.log('[App] Peer left:', e.detail.payload);
        const peerId = e.detail.payload?.peerId;
        if (peerId) {
            const peer = peers.get(peerId);
            peers.delete(peerId);
            updatePeerList();
            if (peer) {
                showNotification(`${peer.name} left`);
            }
        }
    });

    wsManager.addEventListener('room-created', (e) => {
        console.log('[App] Room created:', e.detail.payload);
        publicRoomCode = e.detail.payload?.code;
        showRoomCode(publicRoomCode);
    });

    wsManager.addEventListener('room-joined', (e) => {
        console.log('[App] Joined room:', e.detail.payload);
        publicRoomCode = e.detail.payload?.code;
        const roomPeers = e.detail.payload?.peers || [];
        for (const peer of roomPeers) {
            peers.set(peer.id, peer);
        }
        updatePeerList();
        showNotification(`Joined room ${publicRoomCode}`);
        closeRoomModal();
    });

    wsManager.addEventListener('room-error', (e) => {
        console.log('[App] Room error:', e.detail.payload);
        showNotification(e.detail.payload?.error || 'Room error', 'error');
    });
}

/**
 * Set up file transfer event listeners
 */
function setupFileTransferListeners() {
    fileTransferManager.addEventListener('receive-request', (e) => {
        const { transferId, peerId, files, totalSize } = e.detail;
        const peer = peers.get(peerId) || { name: 'Unknown' };
        showIncomingTransfer(transferId, peer.name, files, totalSize);
    });

    fileTransferManager.addEventListener('send-pending', (e) => {
        updateSendProgress(0, 'Waiting for acceptance...');
    });

    fileTransferManager.addEventListener('send-accepted', (e) => {
        updateSendProgress(0, 'Connecting...');
    });

    fileTransferManager.addEventListener('send-rejected', (e) => {
        updateSendProgress(0, 'Transfer rejected');
        setTimeout(closeSendModal, 1500);
    });

    fileTransferManager.addEventListener('send-progress', (e) => {
        const { progress } = e.detail;
        updateSendProgress(progress * 100, `Sending... ${Math.round(progress * 100)}%`);
    });

    fileTransferManager.addEventListener('send-complete', (e) => {
        updateSendProgress(100, 'Complete!');
        setTimeout(closeSendModal, 1500);
    });

    fileTransferManager.addEventListener('receive-progress', (e) => {
        const { transferId, progress } = e.detail;
        updateReceiveProgress(transferId, progress * 100);
    });

    fileTransferManager.addEventListener('receive-complete', (e) => {
        const { transferId } = e.detail;
        removeIncomingTransfer(transferId);
        showNotification('File received!');
    });
}

/**
 * Set up UI event handlers
 */
function setupUI() {
    setupDropZone();
    setupFileInput();

    // Room buttons
    document.getElementById('create-room-btn')?.addEventListener('click', () => {
        wsManager.createRoom();
    });

    document.getElementById('join-room-btn')?.addEventListener('click', () => {
        openJoinRoomModal();
    });

    // Edit device name
    document.getElementById('device-name')?.addEventListener('click', editDeviceName);
}

/**
 * Update peer list UI
 */
function updatePeerList() {
    const container = document.getElementById('peers-list');
    if (!container) return;

    if (peers.size === 0) {
        container.innerHTML = '<p class="empty-state">Searching for devices...</p>';
        return;
    }

    container.innerHTML = Array.from(peers.values()).map(peer => `
        <div class="peer-card">
            <div class="peer-info">
                <div class="peer-icon">${getPlatformIcon(peer.platform)}</div>
                <div class="peer-details">
                    <h3>${escapeHtml(peer.name)}</h3>
                    <p>${peer.platform || 'unknown'}</p>
                </div>
            </div>
            <button class="btn btn-primary" onclick="openSendModal('${escapeHtml(peer.id)}')">
                Send
            </button>
        </div>
    `).join('');
}

/**
 * Open send modal for a peer
 */
function openSendModal(peerId) {
    const peer = peers.get(peerId);
    if (!peer) return;

    selectedPeer = peer;
    selectedFiles = [];

    document.getElementById('send-to-name').textContent = peer.name;
    document.getElementById('send-modal').classList.remove('hidden');
    document.getElementById('selected-files').classList.add('hidden');
    document.getElementById('send-progress').classList.add('hidden');
    document.getElementById('send-btn').disabled = true;
    document.getElementById('file-input').value = '';
}

/**
 * Close send modal
 */
function closeSendModal() {
    document.getElementById('send-modal').classList.add('hidden');
    selectedPeer = null;
    selectedFiles = [];
}

/**
 * Set up drop zone
 */
function setupDropZone() {
    const dropZone = document.getElementById('drop-zone');
    if (!dropZone) return;

    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('dragover');
    });

    dropZone.addEventListener('dragleave', () => {
        dropZone.classList.remove('dragover');
    });

    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.classList.remove('dragover');
        handleFiles(e.dataTransfer.files);
    });
}

/**
 * Set up file input
 */
function setupFileInput() {
    const input = document.getElementById('file-input');
    if (!input) return;

    input.addEventListener('change', (e) => {
        handleFiles(e.target.files);
    });
}

/**
 * Handle selected files
 */
function handleFiles(files) {
    selectedFiles = Array.from(files);
    updateSelectedFiles();
}

/**
 * Update selected files UI
 */
function updateSelectedFiles() {
    const container = document.getElementById('selected-files');
    const fileList = document.getElementById('file-list');
    const fileCount = document.getElementById('file-count');
    const sendBtn = document.getElementById('send-btn');

    if (selectedFiles.length === 0) {
        container.classList.add('hidden');
        sendBtn.disabled = true;
        return;
    }

    container.classList.remove('hidden');
    fileCount.textContent = selectedFiles.length;
    fileList.innerHTML = selectedFiles.map(f =>
        `<li>${escapeHtml(f.name)} (${formatSize(f.size)})</li>`
    ).join('');
    sendBtn.disabled = false;
}

/**
 * Send files to selected peer
 */
async function sendFiles() {
    if (!selectedPeer || selectedFiles.length === 0) return;

    document.getElementById('send-progress').classList.remove('hidden');
    document.getElementById('send-btn').disabled = true;

    await fileTransferManager.sendFiles(selectedPeer.id, selectedFiles);
}

/**
 * Update send progress UI
 */
function updateSendProgress(percent, text) {
    document.getElementById('progress-fill').style.width = `${percent}%`;
    document.getElementById('progress-text').textContent = text;
}

/**
 * Show incoming transfer
 */
function showIncomingTransfer(transferId, senderName, files, totalSize) {
    const container = document.getElementById('transfers-list');
    const emptyState = container.querySelector('.empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    const fileNames = files.map(f => f.name).join(', ');

    const html = `
        <div class="transfer-card" id="transfer-${transferId}">
            <div class="transfer-info">
                <div class="peer-icon">📥</div>
                <div class="transfer-details">
                    <h3>From ${escapeHtml(senderName)}</h3>
                    <p>${files.length} file(s) - ${formatSize(totalSize)}</p>
                    <p class="file-names">${escapeHtml(fileNames)}</p>
                    <div class="transfer-progress hidden">
                        <div class="progress-bar">
                            <div class="progress-fill" style="width: 0%"></div>
                        </div>
                    </div>
                </div>
            </div>
            <div class="transfer-actions">
                <button class="btn btn-success" onclick="acceptTransfer('${transferId}')">Accept</button>
                <button class="btn btn-danger" onclick="rejectTransfer('${transferId}')">Reject</button>
            </div>
        </div>
    `;

    container.insertAdjacentHTML('beforeend', html);
}

/**
 * Accept incoming transfer
 */
function acceptTransfer(transferId) {
    fileTransferManager.acceptTransfer(transferId);

    const card = document.getElementById(`transfer-${transferId}`);
    if (card) {
        card.querySelector('.transfer-actions').classList.add('hidden');
        card.querySelector('.transfer-progress').classList.remove('hidden');
    }
}

/**
 * Reject incoming transfer
 */
function rejectTransfer(transferId) {
    fileTransferManager.rejectTransfer(transferId);
    removeIncomingTransfer(transferId);
}

/**
 * Update receive progress
 */
function updateReceiveProgress(transferId, percent) {
    const card = document.getElementById(`transfer-${transferId}`);
    if (card) {
        const fill = card.querySelector('.progress-fill');
        if (fill) {
            fill.style.width = `${percent}%`;
        }
    }
}

/**
 * Remove incoming transfer from UI
 */
function removeIncomingTransfer(transferId) {
    const card = document.getElementById(`transfer-${transferId}`);
    if (card) {
        card.remove();
    }

    const container = document.getElementById('transfers-list');
    if (container && container.children.length === 0) {
        container.innerHTML = '<p class="empty-state">No pending transfers</p>';
    }
}

/**
 * Show room code
 */
function showRoomCode(code) {
    const modal = document.getElementById('room-code-modal');
    if (modal) {
        document.getElementById('room-code-display').textContent = code;
        modal.classList.remove('hidden');
    } else {
        showNotification(`Room code: ${code}`);
    }
}

/**
 * Open join room modal
 */
function openJoinRoomModal() {
    const modal = document.getElementById('join-room-modal');
    if (modal) {
        modal.classList.remove('hidden');
        document.getElementById('room-code-input').value = '';
        document.getElementById('room-code-input').focus();
    }
}

/**
 * Close room modal
 */
function closeRoomModal() {
    document.getElementById('room-code-modal')?.classList.add('hidden');
    document.getElementById('join-room-modal')?.classList.add('hidden');
}

/**
 * Submit room code to join
 */
function submitRoomCode() {
    const input = document.getElementById('room-code-input');
    const code = input.value.trim().toUpperCase();
    if (code.length === 5) {
        wsManager.joinRoom(code);
    }
}

/**
 * Edit device name
 */
function editDeviceName() {
    const newName = prompt('Enter device name:', deviceName);
    if (newName && newName.trim()) {
        deviceName = newName.trim();
        localStorage.setItem('peerdrop-device-name', deviceName);
        document.getElementById('device-name').textContent = deviceName;
        // Reconnect with new name
        wsManager.disconnect();
        setTimeout(() => wsManager.connect(), 100);
    }
}

/**
 * Update connection status UI
 */
function updateConnectionStatus(connected) {
    const indicator = document.getElementById('connection-status');
    if (indicator) {
        indicator.classList.toggle('connected', connected);
        indicator.classList.toggle('disconnected', !connected);
    }
}

/**
 * Show notification
 */
function showNotification(message, type = 'info') {
    const container = document.getElementById('notifications') || createNotificationContainer();

    const notification = document.createElement('div');
    notification.className = `notification ${type}`;
    notification.textContent = message;

    container.appendChild(notification);

    setTimeout(() => {
        notification.classList.add('fade-out');
        setTimeout(() => notification.remove(), 300);
    }, 3000);
}

/**
 * Create notification container
 */
function createNotificationContainer() {
    const container = document.createElement('div');
    container.id = 'notifications';
    document.body.appendChild(container);
    return container;
}

// Utility functions

function getPlatform() {
    const ua = navigator.userAgent.toLowerCase();
    if (ua.includes('iphone') || ua.includes('ipad')) return 'ios';
    if (ua.includes('android')) return 'android';
    if (ua.includes('mac')) return 'darwin';
    if (ua.includes('win')) return 'windows';
    if (ua.includes('linux')) return 'linux';
    return 'unknown';
}

function getDefaultDeviceName() {
    const platform = getPlatform();
    const names = {
        'ios': 'iPhone',
        'android': 'Android Phone',
        'darwin': 'Mac',
        'windows': 'Windows PC',
        'linux': 'Linux PC'
    };
    return names[platform] || 'Device';
}

function getPlatformIcon(platform) {
    const icons = {
        'darwin': '🍎',
        'windows': '🪟',
        'linux': '🐧',
        'android': '🤖',
        'ios': '📱'
    };
    return icons[platform] || '💻';
}

function formatSize(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
