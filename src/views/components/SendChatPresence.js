import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendChatPresence',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            action: 'start',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
    },
    methods: {
        openModal() {
            $('#modalSendChatPresence').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            if (this.loading) {
                return;
            }

            try {
                let response = await this.submitApi()
                showSuccessInfo(response)
            } catch (err) {
                showErrorInfo(err)
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                let payload = {
                    phone: this.phone_id,
                    action: this.action
                }
                let response = await window.http.post(`/send/chat-presence`, payload)
                return response.data.message;
            } catch (error) {
                if (error.response) {
                    throw new Error(error.response.data.message);
                }
                throw new Error(error.message);
            } finally {
                this.loading = false;
            }
        }
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Chat Presence</div>
            <div class="description">
                Send <div class="ui green horizontal label">typing</div> indicators to specific chat
            </div>
        </div>
    </div>
    
    <!--  Modal SendChatPresence  -->
    <div class="ui small modal" id="modalSendChatPresence">
        <i class="close icon"></i>
        <div class="header">
            Send Chat Presence
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone" />
                <div class="field">
                    <label>Action</label>
                    <select v-model="action" class="ui dropdown">
                        <option value="start">Start Typing</option>
                        <option value="stop">Stop Typing</option>
                    </select>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'loading': loading, 'disabled': loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}