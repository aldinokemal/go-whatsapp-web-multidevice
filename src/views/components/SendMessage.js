import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendMessage',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            text: '',
            reply_message_id: '',
            participant_jid: '',
            quote: '',
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
            $('#modalSendMessage').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isShowReplyId() {
            return this.type !== window.TYPESTATUS;
        },
        isValidForm() {
            // Validate phone number is not empty except for status type
            const isPhoneValid = this.type === window.TYPESTATUS || this.phone.trim().length > 0;
            
            // Validate message is not empty and has reasonable length
            const isMessageValid = this.text.trim().length > 0 && this.text.length <= 4096;
            
            // Validate reply_message_id format if provided
            const isReplyValid = this.reply_message_id === '' ||
                /^[A-F0-9]{32}\/[A-F0-9]{20}$/.test(this.reply_message_id) &&
                this.participant_jid.trim().length > 0 && this.quote.trim().length > 0;

            return isPhoneValid && isMessageValid && isReplyValid;
        },
        async handleSubmit() {
            // Add validation check here to prevent submission when form is invalid
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalSendMessage').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    message: this.text.trim(),
                    reply: {
                        reply_message_id: this.reply_message_id,
                        participant_jid: this.participant_jid,
                        quote: this.quote
                    }
                };

                const response = await window.http.post('/send/message', payload);
                this.handleReset();
                return response.data.message;
            } catch (error) {
                if (error.response?.data?.message) {
                    throw new Error(error.response.data.message);
                }
                throw error;
            } finally {
                this.loading = false;
            }
        },
        handleReset() {
            this.phone = '';
            this.text = '';
            this.reply_message_id = '';
            this.participant_jid = '';
            this.quote = '';
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
                <FormRecipient v-model:type="type" v-model:phone="phone" :show-status="true"/>
                <div class="field" v-if="isShowReplyId()">
                    <label>Reply Message ID</label>
                    <input v-model="reply_message_id" type="text"
                           placeholder="Optional: 57D29F74B7FC62F57D8AC2C840279B5B/3EB0288F008D32FCD0A424"
                           aria-label="reply_message_id">
                </div>
                <div class="field" v-if="isShowReplyId() && reply_message_id !== ''">
                    <label>Participant JID</label>
                    <input v-model="participant_jid" type="text"
                           placeholder="Quoted Participant JID"
                           aria-label="participant_jid" required>
                </div>
                <div class="field" v-if="isShowReplyId() && reply_message_id !== ''">
                    <label>Quote</label>
                    <input v-model="quote" type="text"
                           placeholder="Quote"
                           aria-label="quote" required>
                </div>
                <div class="field">
                    <label>Message</label>
                    <textarea v-model="text" placeholder="Hello this is message text"
                              aria-label="message"></textarea>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Send
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}