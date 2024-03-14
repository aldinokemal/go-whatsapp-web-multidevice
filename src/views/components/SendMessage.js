export default {
    name: 'SendMessage',
    data() {
        return {
            type: 'user',
            phone: '',
            text: '',
            reply_id: '',
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
            $('#modalSendMessage').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
                $('#modalSendMessage').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    message: this.text,
                }
                if (this.reply_id !== '') {
                    payload.reply_id = this.reply_id;
                }

                let response = await window.http.post(`/send/message`, payload)
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
            this.text = '';
            this.type = 'user';
            this.reply_id = '';
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Message</div>
            <div class="description">
                Send any message to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendMessage  -->
    <div class="ui small modal" id="modalSendMessage">
        <i class="close icon"></i>
        <div class="header">
            Send Message
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
                    <label>Reply Message ID</label>
                    <input v-model="reply_id" type="text"
                           placeholder="Optional: 57D29F74B7FC62F57D8AC2C840279B5B/3EB0288F008D32FCD0A424"
                           aria-label="reply_id">
                </div>
                <div class="field">
                    <label>Message</label>
                    <textarea v-model="text" placeholder="Hello this is message text"
                              aria-label="message"></textarea>
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