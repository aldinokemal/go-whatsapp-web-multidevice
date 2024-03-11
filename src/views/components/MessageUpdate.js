export default {
    name: 'Update Message',
    data() {
        return {
            update_type: 'user',
            update_phone: '',
            update_message_id: '',
            update_new_message: '',
            update_loading: false,
        }
    },
    computed: {
        update_phone_id() {
            return this.update_type === 'user' ? `${this.update_phone}@${window.TYPEUSER}` : `${this.update_phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        messageEditModal() {
            $('#modalMessageEdit').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async messageEditProcess() {
            try {
                let response = await this.messageEditApi()
                showSuccessInfo(response)
                $('#modalMessageEdit').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        messageEditApi() {
            return new Promise(async (resolve, reject) => {
                try {
                    this.update_loading = true;

                    const payload = {
                        phone: this.update_phone_id,
                        message: this.update_new_message
                    }

                    let response = await http.post(`/message/${this.update_message_id}/update`, payload)
                    this.messageEditReset();
                    resolve(response.data.message)
                } catch (error) {
                    if (error.response) {
                        reject(error.response.data.message)
                    } else {
                        reject(error.message)
                    }
                } finally {
                    this.update_loading = false;
                }
            })
        },
        messageEditReset() {
            this.update_type = 'user';
            this.update_phone = '';
            this.update_message_id = '';
            this.update_new_message = '';
            this.update_loading = false;
        },
    },
    template: `
    <div class="red card" @click="messageEditModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui red right ribbon label">Message</a>
            <div class="header">Edit Message</div>
            <div class="description">
                Update your sent message
            </div>
        </div>
    </div>
        
        <!--  Modal MessageEdit  -->
    <div class="ui small modal" id="modalMessageEdit">
        <i class="close icon"></i>
        <div class="header">
            Update Message
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Type</label>
                    <select name="update_type" v-model="update_type" aria-label="type">
                        <option value="group">Group Message</option>
                        <option value="user">Private Message</option>
                    </select>
                </div>
                <div class="field">
                    <label>Phone / Group ID</label>
                    <input v-model="update_phone" type="text" placeholder="6289..."
                           aria-label="phone">
                    <input :value="update_phone_id" disabled aria-label="whatsapp_id">
                </div>
                <div class="field">
                    <label>Message ID</label>
                    <input v-model="update_message_id" type="text" placeholder="Please enter your message id"
                           aria-label="message id">
                </div>
                <div class="field">
                    <label>New Message</label>
                    <textarea v-model="update_new_message" type="text" placeholder="Hello this is your new message text, you can edit before 15 minutes after sent."
                              aria-label="message"></textarea>
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.update_loading}"
                 @click="messageEditProcess">
                Update
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}