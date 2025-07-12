import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'ChatPinManager',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            pinned: true,
            loading: false,
        }
    },
    computed: {
        phone_id() {
            return this.phone + this.type;
        },
    },
    methods: {
        isValidForm() {
            const isPhoneValid = this.phone.trim().length > 0;
            return isPhoneValid;
        },
        openModal() {
            $('#modalChatPin').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalChatPin').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    pinned: this.pinned
                };

                const response = await window.http.post(`/chat/${this.phone_id}/pin`, payload);
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
            this.pinned = true;
        },
    },
    template: `
    <div class="purple card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui purple right ribbon label">Chat</a>
            <div class="header">Pin Chat</div>
            <div class="description">
                Pin or unpin chats to the top of the list
            </div>
        </div>
    </div>
    
    <!--  Modal ChatPin  -->
    <div class="ui small modal" id="modalChatPin">
        <i class="close icon"></i>
        <div class="header">
            Pin Chat
        </div>
        <div class="content">
            <form class="ui form">
                <FormRecipient v-model:type="type" v-model:phone="phone" :show-status="false"/>
                <div class="field">
                    <label>Action</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="pinned" v-model="pinned">
                        <label>Pin chat (uncheck to unpin)</label>
                    </div>
                </div>
            </form>
        </div>
        <div class="actions">
            <button class="ui approve positive right labeled icon button" 
                 :class="{'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                {{ pinned ? 'Pin Chat' : 'Unpin Chat' }}
                <i class="pin icon"></i>
            </button>
        </div>
    </div>
    `
} 