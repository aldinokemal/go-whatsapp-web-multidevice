import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendLink',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            link: '',
            caption: '',
            reply_message_id: '',
            loading: false,
            is_forwarded: false,
            duration: 0
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
    },
    methods: {
        openModal() {
            $('#modalSendLink').modal({
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
            
            // Validate link is not empty and has reasonable length
            const isLinkValid = this.link.trim().length > 0 && this.link.length <= 4096;

            // Validate caption is not empty and has reasonable length
            const isCaptionValid = this.caption.trim().length > 0 && this.caption.length <= 4096;

            return isPhoneValid && isLinkValid && isCaptionValid
        },
        async handleSubmit() {
            // Add validation check here to prevent submission when form is invalid
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalSendLink').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    link: this.link.trim(),
                    caption: this.caption.trim(),
                    is_forwarded: this.is_forwarded,
                    ...(this.duration && this.duration > 0 ? {duration: this.duration} : {})
                };
                if (this.reply_message_id !== '') {
                    payload.reply_message_id = this.reply_message_id;
                }

                const response = await window.http.post('/send/link', payload);
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
            this.link = '';
            this.caption = '';
            this.reply_message_id = '';
            this.is_forwarded = false;
            this.duration = 0;
        },
    },
    template: `
    <div class="blue card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui blue right ribbon label">Send</a>
            <div class="header">Send Link</div>
            <div class="description">
                Send link to user or group
            </div>
        </div>
    </div>
    
    <!--  Modal SendLink  -->
    <div class="ui small modal" id="modalSendLink">
        <i class="close icon"></i>
        <div class="header">
            Send Link
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
                <div class="field">
                    <label>Link</label>
                    <input v-model="link" type="text" placeholder="https://www.google.com"
                           aria-label="link">
                </div>
                <div class="field">
                    <label>Caption</label>
                    <textarea v-model="caption" placeholder="Hello this is caption"
                              aria-label="caption"></textarea>
                </div>
                <div class="field" v-if="isShowReplyId()">
                    <label>Is Forwarded</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="is forwarded" v-model="is_forwarded">
                        <label>Mark link as forwarded</label>
                    </div>
                </div>
                <div class="field">
                    <label>Disappearing Duration (seconds)</label>
                    <input v-model.number="duration" type="number" min="0" placeholder="0 (no expiry)" aria-label="duration"/>
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