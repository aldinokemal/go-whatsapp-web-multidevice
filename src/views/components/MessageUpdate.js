import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'UpdateMessage',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            message_id: '',
            new_message: '',
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
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
        isValidForm() {
            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                return false;
            }

            if (!this.message_id.trim() || !this.new_message.trim()) {
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
            this.type = window.TYPEUSER;
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
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                
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
            <button class="ui approve positive right labeled icon button" :class="{'loading': this.loading, 'disabled': !this.isValidForm() || this.loading}"
                 @click="handleSubmit">
                Update
                <i class="send icon"></i>
            </button>
        </div>
    </div>
    `
}