import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'Send',
    components: {
        FormRecipient
    },
    data() {
        return {
            phone: '',
            type: window.TYPEUSER,
            loading: false,
            selectedFileName: null,
            is_forwarded: false,
            audio_url: null,
            duration: 0,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        openModal() {
            $('#modalAudioSend').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (this.type !== window.TYPEUSER && !this.phone.trim()) {
                return false;
            }

            if (!this.selectedFileName && !this.audio_url) {
                return false;
            }

            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalAudioSend').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = new FormData();
                payload.append("phone", this.phone_id)
                payload.append("is_forwarded", this.is_forwarded)
                if (this.duration && this.duration > 0) {
                    payload.append("duration", this.duration)
                }

                const fileInput = $("#file_audio");
                if (fileInput.length > 0 && fileInput[0].files.length > 0) {
                    const file = fileInput[0].files[0];
                    payload.append('audio', file);
                }
                if (this.audio_url) {
                    payload.append('audio_url', this.audio_url)
                }

                const response = await window.http.post(`/send/audio`, payload)
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.type = window.TYPEUSER;
            this.is_forwarded = false;
            this.duration = 0;
            $("#file_audio").val('');
            this.selectedFileName = null;
            this.audio_url = null;
        },
        handleFileChange(event) {
            const file = event.target.files[0];
            if (file) {
                this.selectedFileName = file.name;
            }
        }
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Audio</div>
            <div class="description">
                Send audio to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendAudio  -->
    <div class="ui small modal" id="modalAudioSend">
        <i class="close icon"></i>
        <div class="header">
            Send Audio
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                <div class="field">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark audio as forwarded</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
                </div>
                <div class="field">
                    <label>Audio URL</label>
                    <input type="text" v-model="audio_url" placeholder="https://example.com/audio.mp3"
                           aria-label="audio_url"/>
                </div>
                <div style="text-align: left; font-weight: bold; margin: 10px 0;">or you can upload audio from your
                    device
                </div>
                <div class="field" style="padding-bottom: 30px">
                    <label>Audio</label>
                    <input type="file" style="display: none" accept="audio/*" id="file_audio"
                           @change="handleFileChange"/>
                    <label for="file_audio" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload
                    </label>
                    <div v-if="selectedFileName" style="margin-top: 60px">
                        <div class="ui message">
                            <i class="file icon"></i>
                            Selected file: {{ selectedFileName }}
                        </div>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}