export default {
    name: 'DeviceManager',
    props: {
        wsBasePath: {
            type: String,
            default: ''
        }
    },
    data() {
        return {
            deviceList: [],
            selectedDeviceId: '',
            deviceIdInput: '',
            isCreatingDevice: false,
            deviceToDelete: { id: '', jid: '', state: '' },
            isDeleting: false
        }
    },
    computed: {
        selectedDevice() {
            if (!this.selectedDeviceId) return null;
            return this.deviceList.find(d => (d.id || d.device) === this.selectedDeviceId) || null;
        },
        isSelectedDeviceLoggedIn() {
            return this.selectedDevice?.state === 'logged_in';
        }
    },
    methods: {
        async fetchDevices() {
            try {
                const res = await window.http.get(`/devices`);
                this.deviceList = res.data.results || [];
                if (!this.selectedDeviceId && this.deviceList.length > 0) {
                    const first = this.deviceList[0].id || this.deviceList[0].device;
                    this.setDeviceContext(first);
                }
                // Emit devices to parent for other components
                this.$emit('devices-updated', this.deviceList);
            } catch (err) {
                console.error(err);
            }
        },
        setDeviceContext(id) {
            if (!id) {
                showErrorInfo('Device ID is required');
                return;
            }
            this.selectedDeviceId = id;
            this.$emit('device-selected', id);
            showSuccessInfo(`Using device ${id}`);
        },
        async createDevice() {
            try {
                this.isCreatingDevice = true;
                const payload = this.deviceIdInput ? {device_id: this.deviceIdInput} : {};
                const res = await window.http.post('/devices', payload);
                const deviceID = res.data?.results?.id || res.data?.results?.device_id || this.deviceIdInput;
                this.setDeviceContext(deviceID);
                this.deviceIdInput = '';
            } catch (err) {
                const msg = err.response?.data?.message || err.message || 'Failed to create device';
                showErrorInfo(msg);
            } finally {
                this.isCreatingDevice = false;
            }
        },
        useDeviceFromInput() {
            if (!this.deviceIdInput) {
                showErrorInfo('Enter a device_id or create one first.');
                return;
            }
            this.setDeviceContext(this.deviceIdInput);
        },
        openDeleteModal(deviceId, jid) {
            const device = this.deviceList.find(d => (d.id || d.device) === deviceId);
            this.deviceToDelete = { id: deviceId, jid: jid || '', state: device?.state || '' };
            $('#deleteDeviceModal').modal({
                closable: false,
                onApprove: () => {
                    this.executeDelete();
                    return false;
                },
                onDeny: () => {
                    this.resetDeleteState();
                }
            }).modal('show');
        },
        resetDeleteState() {
            this.deviceToDelete = { id: '', jid: '', state: '' };
            this.isDeleting = false;
        },
        async executeDelete() {
            const deviceId = this.deviceToDelete.id;
            if (!deviceId) {
                showErrorInfo('No device selected for deletion');
                return;
            }
            try {
                this.isDeleting = true;
                
                // Logout first (fire and forget), then delete
                window.http.get(`/app/logout`, {
                    headers: { 'X-Device-Id': encodeURIComponent(deviceId) }
                }).catch(() => {});
                
                await window.http.delete(`/devices/${encodeURIComponent(deviceId)}`);
                showSuccessInfo(`Device ${deviceId} deleted successfully`);
                $('#deleteDeviceModal').modal('hide');
                
                if (this.selectedDeviceId === deviceId) {
                    this.selectedDeviceId = '';
                    this.$emit('device-selected', '');
                }
                
                await this.fetchDevices();
                this.resetDeleteState();
            } catch (err) {
                const msg = err.response?.data?.message || err.message || 'Failed to delete device';
                showErrorInfo(msg);
                this.isDeleting = false;
            }
        },
        // Called by parent to refresh devices
        refresh() {
            this.fetchDevices();
        },
        // Called by parent to update device list from websocket
        updateDeviceList(devices) {
            if (Array.isArray(devices)) {
                this.deviceList = devices;
                this.$emit('devices-updated', devices);
            }
        }
    },
    mounted() {
        this.fetchDevices();
    },
    template: `
    <div class="ui stackable grid">
        <div class="ten wide column">
            <div class="ui segment">
                <h3 class="ui header">
                    <i class="play icon"></i>
                    <div class="content">
                        Device setup
                        <div class="sub header">Create or select a device_id, then open login.</div>
                    </div>
                </h3>
                <div class="ui form">
                    <div class="two fields">
                        <div class="field">
                            <label>Device ID (optional)</label>
                            <input type="text" v-model="deviceIdInput" placeholder="Leave empty to auto-generate">
                        </div>
                        <div class="field">
                            <label>Actions</label>
                            <div class="ui buttons">
                                <button class="ui primary button" :class="{loading: isCreatingDevice}" @click="createDevice">
                                    Create device
                                </button>
                                <div class="or"></div>
                                <button class="ui button" @click="useDeviceFromInput">Use this device</button>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="ui divider"></div>
                
                <!-- Device List -->
                <div class="ui relaxed list" v-if="deviceList.length">
                    <div class="item" v-for="dev in deviceList" :key="dev.id || dev.device">
                        <i class="mobile alternate icon"></i>
                        <div class="content">
                            <div class="header">{{ dev.id || dev.device }}</div>
                            <div class="description">
                                <span>State: {{ dev.state || 'unknown' }}</span>
                                <span v-if="dev.jid"> · JID: {{ dev.jid }}</span>
                            </div>
                        </div>
                        <div class="right floated content">
                            <button class="ui mini button" 
                                    :class="{active: selectedDeviceId === (dev.id || dev.device)}"
                                    @click="setDeviceContext(dev.id || dev.device)">
                                {{ selectedDeviceId === (dev.id || dev.device) ? 'Selected' : 'Use' }}
                            </button>
                            <button class="ui mini red icon button" 
                                    @click="openDeleteModal(dev.id || dev.device, dev.jid)" 
                                    :class="{loading: isDeleting && deviceToDelete.id === (dev.id || dev.device)}">
                                <i class="trash icon" style="margin: 0;"></i>
                            </button>
                        </div>
                    </div>
                </div>
                <div class="ui message" v-else>
                    No devices yet. Create one to begin.
                </div>
            </div>
        </div>
        <div class="six wide column">
            <div class="ui warning message">
                <div class="header">How to log in</div>
                <ul class="list">
                    <li>Step 1: Create a device to get <code>device_id</code>.</li>
                    <li>Step 2: Send <code>X-Device-Id: device_id</code> on REST calls.</li>
                    <li>Step 3: Open Login card to pair (QR or code).</li>
                    <li>WebSocket URL: <code>{{ wsBasePath }}/ws?device_id=&lt;device_id&gt;</code></li>
                </ul>
            </div>
        </div>

        <!-- Delete Device Confirmation Modal -->
        <div class="ui small modal" id="deleteDeviceModal">
            <div class="header">
                <i class="trash alternate icon"></i>
                Confirm Delete Device
            </div>
            <div class="content">
                <p>Are you sure you want to delete this device?</p>
                <div class="ui segment">
                    <p><strong>Device ID:</strong> <code>{{ deviceToDelete.id }}</code></p>
                    <p v-if="deviceToDelete.jid"><strong>JID:</strong> <code>{{ deviceToDelete.jid }}</code></p>
                </div>
                <div class="ui warning message">
                    <div class="header">Warning</div>
                    <p>This action will permanently delete the device and all associated data including chats and messages. This cannot be undone.</p>
                </div>
            </div>
            <div class="actions">
                <button class="ui cancel button">Cancel</button>
                <button class="ui red approve button" :class="{loading: isDeleting}">
                    <i class="trash icon"></i>
                    Delete Device
                </button>
            </div>
        </div>
    </div>
    `
}
