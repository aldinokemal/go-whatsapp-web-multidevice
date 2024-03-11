export default {
    name: 'ReactMessage',
    data() {
        return {
            type: 'user',
            phone: '',
            message_id: '',
            emoji: '',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.type === 'user' ? `${this.phone}@${window.TYPEUSER}` : `${this.phone}@${window.TYPEGROUP}`
        }
    },
    methods: {
        messageModal() {
            $('#modalMessageReaction').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async messageProcess() {
            try {
                let response = await this.messageApi()
                showSuccessInfo(response)
                $('#modalMessageReaction').modal('hide');
            } catch (err) {
                showErrorInfo(err)
            }
        },
        messageApi() {
            return new Promise(async (resolve, reject) => {
                try {
                    this.loading = true;
                    let payload = new FormData();
                    payload.append("phone", this.phone_id)
                    payload.append("emoji", this.emoji)
                    let response = await http.post(`/message/${this.message_id}/reaction`, payload)
                    this.messageReset();
                    resolve(response.data.message)
                } catch (error) {
                    if (error.response) {
                        reject(error.response.data.message)
                    } else {
                        reject(error.message)
                    }
                } finally {
                    this.loading = false;
                }
            })
        },
        messageReset() {
            this.phone = '';
            this.message_id = '';
            this.emoji = '';
            this.type = 'user';
        },
    },
    template: `
    <div class="red card" @click="messageModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui red right ribbon label">Message</a>
            <div class="header">React Message</div>
            <div class="description">
                 any message in private or group chat
            </div>
        </div>
    </div>
    
    
    <!--  Modal MessageReaction  -->
    <div class="ui small modal" id="modalMessageReaction">
        <i class="close icon"></i>
        <div class="header">
             React Message
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
                    <label>Emoji</label>
                    <input v-model="emoji" type="text" placeholder="Please enter emoji"
                           aria-label="message id">
                </div>
            </form>
        </div>
        <div class="actions">
            <div class="ui approve positive right labeled icon button" :class="{'loading': this.loading}"
                 @click="messageProcess">
                Send
                <i class="send icon"></i>
            </div>
        </div>
    </div>
    `
}