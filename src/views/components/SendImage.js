export default {
    name: 'SendImage',
    data() {
        return {
            phone: '',
            view_once: false,
            compress: false,
            caption: '',
            type: 'user',
            loading: false,
            selected_file: null
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        openModal() {
            $('#modalSendImage').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendImage').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = new FormData();
                payload.append("phone", this.phone_id)
                payload.append("view_once", this.view_once)
                payload.append("compress", this.compress)
                payload.append("caption", this.caption)
                payload.append('image', $("#file_image")[0].files[0])

                let response = await window.http.post(`/send/image`, payload)
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
            this.view_once = false;
            this.compress = false;
            this.phone = '';
            this.caption = '';
            this.type = 'user';
            $("#file_image").val('');
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor:pointer;">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Image</div>
            <div class="description">
                Send image with
                <div class="ui blue horizontal label">jpg/jpeg/png</div>
                type
            </div>
        </div>
    </div>
    
    <!--  Modal SendImage  -->
    <div class="ui small modal" id="modalSendImage">
        <i class="close icon"></i>
        <div class="header">
            Send Image
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Type</label>
                    <select name="type" v-model="type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone / Group ID</label>
                    <input v-model="phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="phone_id" disabled aria-label="whatsapp_id">
                </div>
                <div class="field">
                    <label>Caption</label>
                    <textarea v-model="caption" type="text" placeholder="Hello this is image caption"
                              aria-label="caption"></textarea>
                </div>
                <div class="field">
                    <label>View Once</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="view once" v-model="view_once">
                        <label>Check for enable one time view</label>
                    </div>
                </div>
                <div class="field">
                    <label>Compress</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="compress" v-model="compress">
                        <label>Check for compressing image to smaller size</label>
                    </div>
                </div>
                <div class="field" style="padding-bottom: 30px">
                    <label>Image</label>
                    <input type="file" style="display: none" id="file_image" accept="image/png,image/jpg,image/jpeg"/>
                    <label for="file_image" class="ui positive medium green left floated button" style="color: white">
                        <i class="ui upload icon"></i>
                        Upload image
                    </label>
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="handleSubmit">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}