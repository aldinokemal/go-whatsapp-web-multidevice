export default {
    name: 'Calls',
    data() {
        return {
            phone: '',
            activeCall: null,
            incomingCall: null,
            loading: false,
            statusText: 'Idle',
            muted: false,
            recordCall: false,
            peerConnection: null,
            dataChannel: null,
            audioContext: null,
            localStream: null,
            sourceNode: null,
            processorNode: null,
        }
    },
    computed: {
        canStart() {
            return this.phone.trim().length > 0 && !this.loading && !this.activeCall;
        },
        hasActiveCall() {
            return !!this.activeCall;
        },
        activeCallLabel() {
            if (!this.activeCall) return '';
            return this.activeCall.peer_jid || this.activeCall.call_id;
        },
    },
    methods: {
        openModal() {
            $('#modalCalls').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async startCall() {
            if (!this.canStart) return;
            this.loading = true;
            try {
                const response = await window.http.post('/call', { phone: this.phone.trim(), record: this.recordCall });
                this.activeCall = response.data.results.call;
                this.statusText = 'Ringing';
                await this.setupWebRTC(this.activeCall.call_id);
                showSuccessInfo('Call started');
            } catch (err) {
                showErrorInfo(err.response?.data?.message || err.message || 'Failed to start call');
                await this.cleanupMedia();
            } finally {
                this.loading = false;
            }
        },
        async acceptIncomingCall() {
            if (!this.incomingCall) return;
            this.loading = true;
            try {
                const callID = this.incomingCall.call_id;
                await window.http.post(`/call/${encodeURIComponent(callID)}/accept`, { record: this.recordCall });
                this.activeCall = this.incomingCall;
                this.incomingCall = null;
                $('#incomingCallModal').modal('hide');
                this.statusText = 'Connecting';
                await this.setupWebRTC(callID);
            } catch (err) {
                showErrorInfo(err.response?.data?.message || err.message || 'Failed to accept call');
                await this.cleanupMedia();
            } finally {
                this.loading = false;
            }
        },
        async rejectIncomingCall() {
            if (!this.incomingCall) return;
            const callID = this.incomingCall.call_id;
            try {
                await window.http.post(`/call/${encodeURIComponent(callID)}/reject`);
                showSuccessInfo('Call rejected');
            } catch (err) {
                showErrorInfo(err.response?.data?.message || err.message || 'Failed to reject call');
            } finally {
                this.incomingCall = null;
                $('#incomingCallModal').modal('hide');
            }
        },
        async endCall() {
            if (!this.activeCall) return;
            const callID = this.activeCall.call_id;
            try {
                await window.http.delete(`/call/${encodeURIComponent(callID)}`);
                showSuccessInfo('Call ended');
            } catch (err) {
                showErrorInfo(err.response?.data?.message || err.message || 'Failed to end call');
            } finally {
                await this.cleanupMedia();
                this.activeCall = null;
                this.statusText = 'Idle';
            }
        },
        toggleMute() {
            this.muted = !this.muted;
            if (this.localStream) {
                this.localStream.getAudioTracks().forEach(track => {
                    track.enabled = !this.muted;
                });
            }
        },
        async setupWebRTC(callID) {
            this.audioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 48000 });
            this.localStream = await navigator.mediaDevices.getUserMedia({ audio: true });
            this.peerConnection = new RTCPeerConnection();
            this.dataChannel = this.peerConnection.createDataChannel('pcm', { ordered: false });

            this.dataChannel.binaryType = 'arraybuffer';
            this.dataChannel.onopen = () => {
                this.statusText = 'Audio connected';
                this.startCapture();
            };
            this.dataChannel.onmessage = event => {
                this.playPCM(event.data);
            };

            const offer = await this.peerConnection.createOffer();
            await this.peerConnection.setLocalDescription(offer);
            await this.waitForIceGathering();

            const response = await window.http.post(`/call/${encodeURIComponent(callID)}/webrtc`, {
                sdp_offer: this.peerConnection.localDescription.sdp
            });
            await this.peerConnection.setRemoteDescription({
                type: 'answer',
                sdp: response.data.results.sdp_answer
            });
        },
        waitForIceGathering() {
            if (this.peerConnection.iceGatheringState === 'complete') {
                return Promise.resolve();
            }
            return new Promise(resolve => {
                const checkState = () => {
                    if (this.peerConnection.iceGatheringState === 'complete') {
                        this.peerConnection.removeEventListener('icegatheringstatechange', checkState);
                        resolve();
                    }
                };
                this.peerConnection.addEventListener('icegatheringstatechange', checkState);
            });
        },
        startCapture() {
            if (!this.audioContext || !this.localStream || !this.dataChannel) return;
            this.sourceNode = this.audioContext.createMediaStreamSource(this.localStream);
            this.processorNode = this.audioContext.createScriptProcessor(4096, 1, 1);
            this.processorNode.onaudioprocess = event => {
                if (this.muted || !this.dataChannel || this.dataChannel.readyState !== 'open') return;
                const input = event.inputBuffer.getChannelData(0);
                const pcm = this.floatToInt16PCM(this.downsample(input, this.audioContext.sampleRate, 16000));
                this.dataChannel.send(pcm);
            };
            this.sourceNode.connect(this.processorNode);
            this.processorNode.connect(this.audioContext.destination);
        },
        downsample(input, inputRate, outputRate) {
            if (inputRate === outputRate) return input;
            const ratio = inputRate / outputRate;
            const length = Math.floor(input.length / ratio);
            const output = new Float32Array(length);
            for (let i = 0; i < length; i++) {
                output[i] = input[Math.floor(i * ratio)];
            }
            return output;
        },
        floatToInt16PCM(float32) {
            const buffer = new ArrayBuffer(float32.length * 2);
            const view = new DataView(buffer);
            for (let i = 0; i < float32.length; i++) {
                const sample = Math.max(-1, Math.min(1, float32[i]));
                view.setInt16(i * 2, sample < 0 ? sample * 0x8000 : sample * 0x7fff, true);
            }
            return buffer;
        },
        playPCM(buffer) {
            if (!this.audioContext || !buffer) return;
            const view = new DataView(buffer);
            const samples = new Float32Array(view.byteLength / 2);
            for (let i = 0; i < samples.length; i++) {
                samples[i] = view.getInt16(i * 2, true) / 0x8000;
            }
            const audioBuffer = this.audioContext.createBuffer(1, samples.length, 16000);
            audioBuffer.copyToChannel(samples, 0);
            const source = this.audioContext.createBufferSource();
            source.buffer = audioBuffer;
            source.connect(this.audioContext.destination);
            source.start();
        },
        async cleanupMedia() {
            if (this.processorNode) {
                this.processorNode.disconnect();
                this.processorNode = null;
            }
            if (this.sourceNode) {
                this.sourceNode.disconnect();
                this.sourceNode = null;
            }
            if (this.localStream) {
                this.localStream.getTracks().forEach(track => track.stop());
                this.localStream = null;
            }
            if (this.dataChannel) {
                this.dataChannel.close();
                this.dataChannel = null;
            }
            if (this.peerConnection) {
                this.peerConnection.close();
                this.peerConnection = null;
            }
            if (this.audioContext) {
                await this.audioContext.close();
                this.audioContext = null;
            }
            this.muted = false;
        },
        handleCallEvent(message) {
            const call = message.result;
            if (!call || !call.call_id) return;

            if (message.code === 'CALL_INCOMING') {
                this.incomingCall = call;
                $('#incomingCallModal').modal({ closable: false }).modal('show');
                return;
            }

            if (this.activeCall && this.activeCall.call_id === call.call_id) {
                this.activeCall = call;
                this.statusText = call.status || this.statusText;
                if (message.code === 'CALL_ENDED' || call.status === 'ended') {
                    this.cleanupMedia();
                    this.activeCall = null;
                    this.statusText = 'Idle';
                }
            }
        },
    },
    beforeUnmount() {
        this.cleanupMedia();
    },
    template: `
    <div class="teal card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui teal right ribbon label">Call</a>
            <div class="header">Voice Calls</div>
            <div class="description">
                Start and answer WhatsApp voice calls from this browser.
            </div>
        </div>
    </div>

    <div class="ui small modal" id="modalCalls">
        <i class="close icon"></i>
        <div class="header">Voice Calls</div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Phone</label>
                    <input v-model="phone" type="text" placeholder="5511999999999" :disabled="hasActiveCall">
                </div>
                <div class="field">
                    <div class="ui checkbox">
                        <input id="recordCallCheckbox" type="checkbox" v-model="recordCall">
                        <label for="recordCallCheckbox">Record this call as a WAV file</label>
                    </div>
                </div>
            </form>
            <div class="ui message" v-if="hasActiveCall">
                <div class="header">{{ statusText }}</div>
                <p>{{ activeCallLabel }}</p>
                <p v-if="activeCall && activeCall.recording">Recording: enabled</p>
            </div>
        </div>
        <div class="actions">
            <button class="ui button" :class="{disabled: !hasActiveCall}" @click.prevent="toggleMute">
                <i :class="muted ? 'microphone slash icon' : 'microphone icon'"></i>
                {{ muted ? 'Unmute' : 'Mute' }}
            </button>
            <button class="ui red button" :class="{disabled: !hasActiveCall}" @click.prevent="endCall">
                <i class="phone slash icon"></i>
                End
            </button>
            <button class="ui teal approve button" :class="{disabled: !canStart, loading: loading}" @click.prevent="startCall">
                <i class="phone icon"></i>
                Start
            </button>
        </div>
    </div>

    <div class="ui tiny modal" id="incomingCallModal">
        <div class="header">Incoming voice call</div>
        <div class="content">
            <p v-if="incomingCall">{{ incomingCall.peer_jid }}</p>
            <div class="ui checkbox">
                <input id="recordIncomingCallCheckbox" type="checkbox" v-model="recordCall">
                <label for="recordIncomingCallCheckbox">Record this incoming call</label>
            </div>
        </div>
        <div class="actions">
            <button class="ui red button" :class="{loading: loading}" @click.prevent="rejectIncomingCall">
                <i class="phone slash icon"></i>
                Reject
            </button>
            <button class="ui green button" :class="{loading: loading}" @click.prevent="acceptIncomingCall">
                <i class="phone icon"></i>
                Accept
            </button>
        </div>
    </div>
    `
}
