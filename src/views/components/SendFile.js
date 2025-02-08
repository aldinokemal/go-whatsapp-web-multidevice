import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendFile',
    components: {
        FormRecipient
    },
    props: {
        maxFileSize: {
            type: String,
            required: true,
        }
    },
    data() {
        return {
            caption: '',
            type: window.TYPEUSER,
            phone: '',
            loading: false,
            selectedFileName: null
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        }
    },
    methods: {
        openModal() {
            $('#modalSendFile').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                return false;
            }

            if (!this.selectedFileName) {
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
                $('#modalSendFile').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = new FormData();
                payload.append("caption", this.caption)
                payload.append("phone", this.phone_id)
                payload.append("file", $("#file_file")[0].files[0])
                let response = await window.http.post(`/send/file`, payload)
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
            this.phone = '';
            this.type = window.TYPEUSER;
            this.selectedFileName = null;
            $("#file_file").val('');
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
            <div class="header">Send File</div>
            <div class="description">
                Send any file up to
                <div class="ui blue horizontal label">{{ maxFileSize }}</div>
            </div>
        </div>
    </div>
    
    <!--  Modal SendFile  -->
    <div class="ui small modal" id="modalSendFile">
        <i class="close icon"></i>
        <div class="header">
            Send File
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                
                <div class="field">
                    <label>Caption</label>
                    <textarea v-model="caption" placeholder="Type some caption (optional)..."
                              aria-label="caption"></textarea>
                </div>
                <div class="field" style="padding-bottom: 30px">
                    <label>File</label>
                    <input type="file" style="display: none" id="file_file" @change="handleFileChange">
                    <label for="file_file" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload file
                    </label>
                    <div v-if="selectedFileName" style="margin-top: 60px; clear: both;">
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