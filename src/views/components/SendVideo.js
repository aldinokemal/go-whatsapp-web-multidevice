import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendVideo',
    components: {
        FormRecipient
    },
    props: {
        maxVideoSize: {
            type: String,
            required: true,
        }
    },
    data() {
        return {
            caption: '',
            view_once: false,
            compress: false,
            type: window.TYPEUSER,
            phone: '',
            loading: false,
            video_url: null,
            selectedFileName: null,
            is_forwarded: false,
            duration: 0
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
    },
    watch: {
        view_once(newValue) {
            // If view_once is set to true, set is_forwarded to false
            if (newValue === true) {
                this.is_forwarded = false;
                this.duration = 0;
            }
        }
    },
    methods: {
        openModal() {
            $('#modalSendVideo').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isShowAttributes() {
            return this.type !== window.TYPESTATUS;
        },
        isValidForm() {
            let isValid = true;

            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                isValid = false;
            }

            const fileInput = $("#file_video")[0];
            const hasFile = fileInput && fileInput.files && fileInput.files[0];

            if (!hasFile && !this.video_url) {
                isValid = false;
            }

            if (hasFile) {
                const videoFile = fileInput.files[0];
                if (!videoFile.type.startsWith('video/')) {
                    isValid = false;
                }
            }

            return isValid;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }

            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendVideo').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = new FormData();
                payload.append("phone", this.phone_id)
                payload.append("caption", this.caption.trim())
                payload.append("view_once", this.view_once)
                payload.append("compress", this.compress)
                payload.append("is_forwarded", this.is_forwarded)
                if (this.duration && this.duration > 0) {
                    payload.append("duration", this.duration)
                }

                const fileInput = $("#file_video")[0];
                if (fileInput && fileInput.files && fileInput.files[0]) {
                    payload.append('video', fileInput.files[0])
                }
                if (this.video_url) {
                    payload.append('video_url', this.video_url)
                }

                let response = await window.http.post(`/send/video`, payload)
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
            this.caption = '';
            this.view_once = false;
            this.compress = false;
            this.phone = '';
            this.selectedFileName = null;
            this.video_url = null;
            this.is_forwarded = false;
            this.duration = 0;
            $("#file_video").val('');
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
            <div class="header">Send Video</div>
            <div class="description">
                Send video
                <div class="ui blue horizontal label">mp4</div>
                up to
                <div class="ui blue horizontal label">{{ maxVideoSize }}</div>
            </div>
        </div>
    </div>
    
    <!--  Modal SendVideo  -->
    <div class="ui small modal" id="modalSendVideo">
        <i class="close icon"></i>
        <div class="header">
            Send Video
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone" :show-status="true"/>
                
                <div class="field">
                    <label>Caption</label>
                    <textarea v-model="caption" placeholder="Type some caption (optional)..."
                              aria-label="caption"></textarea>
                </div>
                <div class="field" v-if="isShowAttributes()">
                    <label>View Once</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="view once" v-model="view_once">
                        <label>Check for enable one time view</label>
                    </div>
                </div>
                <div class="field" v-if="isShowAttributes()">
                    <label>Compress</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="compress" v-model="compress">
                        <label>Check for compressing video to smaller size</label>
                    </div>
                </div>
                <div class="field" v-if="isShowAttributes() && !view_once">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark video as forwarded</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
                </div>
                <div class="field">
                    <label>Video URL</label>
                    <input type="text" v-model="video_url" placeholder="https://example.com/sample.mp4"
                           aria-label="video_url" />
                </div>
                <div style="text-align: left; font-weight: bold; margin: 10px 0;" v-if="!video_url">or you can upload video from your device</div>
                <div class="field" style="padding-bottom: 30px" v-if="!video_url">
                    <label>Video</label>
                    <input type="file" style="display: none" accept="video/*" id="file_video" @change="handleFileChange">
                    <label for="file_video" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload video
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
            <button class="ui approve positive right labeled icon button" 
                 :class="{'loading': loading, 'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}