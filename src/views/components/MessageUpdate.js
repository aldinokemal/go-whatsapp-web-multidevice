export default {
    name: 'UpdateMessage',
    data() {
        return {
            type: 'user',
            phone: '',
            message_id: '',
            new_message: '',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        openModal() {
            $('#modalMessageUpdate').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalMessageUpdate').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {phone: this.phone_id, message: this.new_message}

                let response = await window.http.post(`/message/${this.message_id}/update`, payload)
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
            this.type = 'user';
            this.phone = '';
            this.message_id = '';
            this.new_message = '';
            this.loading = false;
        },
    },
    template: `
    <div class="red card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui red right ribbon label">Message</a>
            <div class="header">Update Message</div>
            <div class="description">
                Update your sent message
            </div>
        </div>
    </div>
        
        <!--  Modal MessageUpdate  -->
    <div class="ui small modal" id="modalMessageUpdate">
        <i class="close icon"></i>
        <div class="header">
            Update Message
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
                    <label>Message ID</label>
                    <input v-model="message_id" type="text" placeholder="Please enter your message id"
                           aria-label="message id">
                </div>
                <div class="field">
                    <label>New Message</label>
                    <textarea v-model="new_message" type="text" placeholder="Hello this is your new message text, you can edit before 15 minutes after sent."
                              aria-label="message"></textarea>
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="handleSubmit">
                Update
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}