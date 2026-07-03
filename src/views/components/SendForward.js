import FormRecipient from "./generic/FormRecipient.js";

export default {
    name: 'SendForward',
    components: {
        FormRecipient
    },
    data() {
        return {
            type: window.TYPEUSER,
            phone: '',
            message_id: '',
            force_reupload: false,
            duration: 0,
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
            $('#modalSendForward').modal({
                onApprove: function () {
                    return false;
                }
            }).modal('show');
        },
        isValidForm() {
            if (this.type !== window.TYPESTATUS && !this.phone.trim()) {
                return false;
            }
            if (!this.message_id.trim()) {
                return false;
            }
            return true;
        },
        async handleSubmit() {
            if (!this.isValidForm() || this.loading) {
                return;
            }
            try {
                const response = await this.submitApi();
                showSuccessInfo(response);
                $('#modalSendForward').modal('hide');
            } catch (err) {
                showErrorInfo(err);
            }
        },
        async submitApi() {
            this.loading = true;
            try {
                const payload = {
                    phone: this.phone_id,
                    force_reupload: this.force_reupload,
                };
                if (this.duration && this.duration > 0) {
                    payload.duration = this.duration;
                }
                const response = await window.http.post(`/message/${this.message_id.trim()}/forward`, payload);
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
            this.message_id = '';
            this.force_reupload = false;
            this.duration = 0;
            this.type = window.TYPEUSER;
        },
    },
    template: `
    <div class="red card" @click="openModal()" style="cursor: pointer">
        <div class="content">
            <a class="ui red right ribbon label">Message</a>
            <div class="header">Forward Message</div>
            <div class="description">
                Forward a stored message to another chat by message ID
            </div>
        </div>
    </div>

    <div class="ui small modal" id="modalSendForward">
        <i class="close icon"></i>
        <div class="header">
            Forward Message
        </div>
        <div class="content">
            <form class="ui form">
                <div class="field">
                    <label>Source Message ID</label>
                    <input v-model="message_id" type="text"
                           placeholder="57D29F74B7FC62F57D8AC2C840279B5B/3EB0288F008D32FCD0A424"
                           aria-label="message id">
                </div>
                <FormRecipient v-model:type="type" v-model:phone="phone"/>
                <div class="field">
                    <label>Force Re-upload</label>
                    <div class="ui toggle checkbox">
                        <input type="checkbox" aria-label="force reupload" v-model="force_reupload">
                        <label>Re-download and re-upload media instead of reusing CDN refs</label>
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
                 :class="{'loading': loading, 'disabled': !isValidForm() || loading}"
                 @click.prevent="handleSubmit">
                Forward
                <i class="share icon"></i>
            </button>
        </div>
    </div>
    `
}
